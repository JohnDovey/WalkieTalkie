package engine

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/discovery/mdns"
	"github.com/JohnDovey/WalkieTalkie/core/sniff"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/arp"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/config"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/graph"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/icmp"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/identify"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/netinfo"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/portmem"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/seed"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/tcpprobe"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/virtbbs"
)

// Engine runs seed + continuous LAN discovery into a graph.Store.
type Engine struct {
	Settings config.Settings
	Graph    *graph.Store
	Ports    *portmem.Store // remembered open ports across restarts
}

// Run seeds then scans until ctx is done.
func (e *Engine) Run(ctx context.Context) {
	e.seedOnce()
	e.scanOnce(ctx)
	ticker := time.NewTicker(e.Settings.ScanInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.seedOnce()
			e.scanOnce(ctx)
		}
	}
}

func (e *Engine) seedOnce() {
	st := graph.Status{
		ICMPEnabled: icmp.Enabled(),
		Message:     icmp.StatusMessage(),
	}

	wt := seed.ApplyWalkieTalkie(e.Graph, e.Settings.LocalBaseURL)
	st.WalkieTalkieOK = wt.OK
	st.BaseOK = wt.OK
	st.WalkieBaseName = wt.BaseName
	st.WalkieSeeded = wt.DeviceCount + wt.RemoteCount
	if !wt.OK {
		st.Message = joinMsg(st.Message, "WalkieTalkie: "+wt.Err)
	} else if wt.Err != "" {
		st.Message = joinMsg(st.Message, "WalkieTalkie: "+wt.Err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	mdnsBases, mdnsDevs := seed.SeedMDNSBases(ctx, e.Graph, e.Settings.LocalBaseURL)
	cancel()
	if mdnsBases > 0 {
		st.WalkieSeeded += mdnsDevs
		st.Message = joinMsg(st.Message, fmt.Sprintf("mDNS Bases:%d", mdnsBases))
	}

	if inv, err := seed.FetchBridgeInventory(e.Settings.MeshBridgeURL); err == nil {
		seed.ApplyBridge(e.Graph, inv)
		st.MeshBridgeOK = true
	} else {
		st.MeshBridgeOK = false
		st.Message = joinMsg(st.Message, "MeshBridge: "+err.Error())
	}

	e.addThisComputer()

	subs, _ := netinfo.LocalSubnets()
	var cidrs []string
	for _, s := range subs {
		cidrs = append(cidrs, s.CIDR)
	}
	st.CIDRs = cidrs
	e.Graph.SetStatus(st)
}

func joinMsg(a, b string) string {
	if a == "" {
		return b
	}
	return a + " · " + b
}

func (e *Engine) scanOnce(ctx context.Context) {
	start := time.Now()
	subs, err := netinfo.LocalSubnets()
	if err != nil {
		log.Printf("meshsniff netinfo: %v", err)
		return
	}
	cidrs := e.Settings.ScanCIDRs
	if len(cidrs) == 0 {
		for _, s := range subs {
			cidrs = append(cidrs, s.CIDR)
		}
	}

	gatewayBySubnet := map[string]string{}
	routerIDs := map[string]string{}

	for _, s := range subs {
		subnetID := "subnet:" + s.CIDR
		e.Graph.Upsert(graph.Node{
			ID:               subnetID,
			Kind:             graph.KindSubnet,
			Label:            s.CIDR,
			Subnet:           s.CIDR,
			IPs:              []string{s.IP},
			DiscoveryMethods: []string{"netinfo"},
			Detail:           map[string]any{"iface": s.Iface},
		})
		netID := "network:" + s.Iface
		e.Graph.Upsert(graph.Node{
			ID:               netID,
			Kind:             graph.KindNetwork,
			Label:            s.Iface,
			DiscoveryMethods: []string{"netinfo"},
		})
		e.Graph.Link(netID, subnetID, "lan", false)
		if s.Gateway != "" {
			gatewayBySubnet[s.CIDR] = s.Gateway
			gwID := e.upsertMachine(s.Gateway, "", graph.KindRouter, "Router "+s.Gateway, s.CIDR, "", []string{"gateway"})
			routerIDs[s.Gateway] = gwID
			e.Graph.Link(subnetID, gwID, "gateway", false)
		}
	}

	e.addThisComputer()

	if entries, err := arp.Table(); err == nil {
		for _, ent := range entries {
			kind := graph.KindComputer
			hn := reverseDNS(ent.IP)
			label := labelForIP(ent.IP)
			if isLikelyRouter(ent.IP, gatewayBySubnet) {
				kind = graph.KindRouter
				label = "Router " + ent.IP
			}
			subnet := matchSubnet(ent.IP, subs)
			id := e.upsertMachine(ent.IP, ent.MAC, kind, label, subnet, hn, []string{"arp"})
			if subnet != "" {
				e.Graph.Link("subnet:"+subnet, id, "lan", false)
			}
		}
	}

	mdnsCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	_ = mdns.Browse(mdnsCtx, "", func(p mdns.Peer) {
		for _, ip := range p.IPv4 {
			e.applyWalkiePeer(ip.String(), p)
		}
	})
	cancel()

	ports := config.DiscoverPorts(e.Settings.Ports)
	hostSet := map[string]bool{}
	if entries, err := arp.Table(); err == nil {
		for _, ent := range entries {
			hostSet[ent.IP] = true
		}
	}
	for _, ip := range netinfo.ThisMachine().IPs {
		hostSet[ip] = true
	}
	// Revisit hosts we previously found open ports on (even if not in ARP right now).
	if e.Ports != nil {
		for _, h := range e.Ports.Hosts() {
			hostSet[h] = true
		}
	}
	for _, cidr := range cidrs {
		hosts, err := netinfo.HostsInCIDR(cidr)
		if err != nil {
			continue
		}
		for i, h := range hosts {
			if i < 5 || i >= len(hosts)-3 || strings.HasSuffix(h, ".1") {
				hostSet[h] = true
			}
		}
		for _, h := range icmp.Sweep(hosts, 200*time.Millisecond, 64) {
			hostSet[h] = true
			subnet := matchSubnet(h, subs)
			e.upsertMachine(h, "", graph.KindComputer, labelForIP(h), subnet, reverseDNS(h), []string{"icmp"})
		}
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 40)
	for host := range hostSet {
		host := host
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			probe := append([]int(nil), ports...)
			if e.Ports != nil {
				probe = unionPorts(probe, e.Ports.PortsFor(host))
			}
			open := tcpprobe.OpenPorts(host, probe, 350*time.Millisecond)
			if e.Ports != nil {
				if len(open) > 0 {
					e.Ports.Remember(host, open)
				}
				e.Ports.PruneClosed(host, probe, open)
			}
			subnet := matchSubnet(host, subs)
			kind := graph.KindComputer
			label := labelForIP(host)
			if isLikelyRouter(host, gatewayBySubnet) {
				kind = graph.KindRouter
				label = "Router " + host
			}
			id := e.upsertMachine(host, "", kind, label, subnet, reverseDNS(host), []string{"tcp"})
			e.Graph.Upsert(graph.Node{
				ID:               id,
				Kind:             kind,
				Label:            label,
				Hostname:         reverseDNS(host),
				IPs:              []string{host},
				OpenPorts:        open,
				Services:         servicesFromPorts(open),
				Subnet:           subnet,
				DiscoveryMethods: []string{"tcp"},
			})
			for _, p := range open {
				e.tryIdentify(host, p)
			}
			e.applyVirtBBS(host, open)
			if subnet != "" {
				e.Graph.Link("subnet:"+subnet, id, "lan", false)
			}
		}()
	}
	wg.Wait()

	e.coalesceSameIP()
	e.linkViaRouters(subs, gatewayBySubnet, routerIDs)
	e.applyWifi(subs, routerIDs)

	st := e.Graph.Snapshot().Status
	st.LastScan = time.Since(start).Round(time.Millisecond).String()
	st.CIDRs = cidrs
	st.ICMPEnabled = icmp.Enabled()
	st.Message = joinMsg(icmp.StatusMessage(), "computers linked via router")
	e.Graph.SetStatus(st)
}

func (e *Engine) applyWifi(subs []netinfo.Subnet, routerIDs map[string]string) {
	w, ok := netinfo.Wifi()
	if !ok {
		return
	}
	detail := map[string]any{"wifiIface": w.Iface}
	if w.PhyMode != "" {
		detail["phyMode"] = w.PhyMode
	}
	if w.RateMbps != "" {
		detail["rateMbps"] = w.RateMbps
	}
	if w.SignalNoise != "" {
		detail["signalNoise"] = w.SignalNoise
	}
	if w.Country != "" {
		detail["country"] = w.Country
	}
	netLabel := w.SSID
	if netLabel == "" {
		netLabel = w.Iface
	} else {
		netLabel = w.SSID + " (" + w.Iface + ")"
	}
	e.Graph.Upsert(graph.Node{
		ID:               "network:" + w.Iface,
		Kind:             graph.KindNetwork,
		Label:            netLabel,
		SSID:             w.SSID,
		BSSID:            w.BSSID,
		Channel:          w.Channel,
		Security:         w.Security,
		Detail:           detail,
		DiscoveryMethods: []string{"wifi"},
	})
	for _, s := range subs {
		if s.Iface != w.Iface || s.Gateway == "" {
			continue
		}
		id := routerIDs[s.Gateway]
		if id == "" {
			id = "host:" + s.Gateway
		}
		label := w.SSID
		if label == "" {
			label = "Router " + s.Gateway
		}
		e.Graph.Upsert(graph.Node{
			ID:               id,
			Kind:             graph.KindRouter,
			Label:            label,
			IPs:              []string{s.Gateway},
			Subnet:           s.CIDR,
			SSID:             w.SSID,
			BSSID:            w.BSSID,
			Channel:          w.Channel,
			Security:         w.Security,
			Detail:           detail,
			DiscoveryMethods: []string{"wifi"},
		})
	}
}

func (e *Engine) addThisComputer() {
	me := netinfo.ThisMachine()
	if len(me.IPs) == 0 {
		return
	}
	hn := me.Hostname
	if hn == "" {
		hn, _ = os.Hostname()
	}
	label := "This computer"
	if hn != "" {
		label = hn + " (this computer)"
	}
	primary := me.IPs[0]
	subnet := ""
	gw := ""
	if len(me.Subnets) > 0 {
		subnet = me.Subnets[0].CIDR
		gw = me.Subnets[0].Gateway
	}
	var services []graph.Service
	var ports []int
	addSvc := func(name string, port int, url string) {
		if port <= 0 {
			return
		}
		ports = append(ports, port)
		services = append(services, graph.Service{Name: name, Port: port, URL: url})
	}
	addSvc("WalkieTalkie Base", 9091, e.Settings.LocalBaseURL)
	addSvc("MeshBridge", 9095, e.Settings.MeshBridgeURL)
	addSvc("MeshSniff", e.Settings.StatusPort, fmt.Sprintf("http://127.0.0.1:%d/", e.Settings.StatusPort))

	id := e.upsertMachine(primary, firstMAC(me.MACs), graph.KindComputer, label, subnet, hn, []string{"local"})
	detail := map[string]any{
		"role": "sniffer-host",
		"note": "Base Station, MeshBridge, and MeshSniff on this machine share these IPs",
	}
	if gw != "" {
		detail["gateway"] = gw
	}
	e.Graph.Upsert(graph.Node{
		ID:               id,
		Kind:             graph.KindComputer,
		Label:            label,
		Hostname:         hn,
		IPs:              me.IPs,
		MACs:             me.MACs,
		OpenPorts:        ports,
		Services:         services,
		Subnet:           subnet,
		ViaRouter:        gw,
		DiscoveryMethods: []string{"local"},
		Detail:           detail,
		Platform:         "desktop-" + runtime.GOOS,
	})
	for _, ip := range me.IPs[1:] {
		e.Graph.Upsert(graph.Node{
			ID:               "host:" + ip,
			Kind:             graph.KindComputer,
			IPs:              []string{ip},
			Hostname:         hn,
			Label:            label,
			Services:         services,
			OpenPorts:        ports,
			SameHostAs:       id,
			DiscoveryMethods: []string{"local"},
		})
	}
}

func (e *Engine) upsertMachine(ip, mac string, kind graph.Kind, label, subnet, hostname string, methods []string) string {
	if ip == "" {
		return ""
	}
	id := "host:" + ip
	n := graph.Node{
		ID:               id,
		Kind:             kind,
		Label:            label,
		Hostname:         hostname,
		IPs:              []string{ip},
		Subnet:           subnet,
		DiscoveryMethods: methods,
	}
	if mac != "" {
		n.MACs = []string{strings.ToLower(mac)}
	}
	return e.Graph.Upsert(n)
}

func (e *Engine) applyWalkiePeer(ip string, p mdns.Peer) {
	ports := []int{}
	services := []graph.Service{}
	urls := map[string]string{}
	if p.SignalPort > 0 {
		ports = append(ports, p.SignalPort)
		services = append(services, graph.Service{Name: "signaling", Port: p.SignalPort})
		urls["signaling"] = fmt.Sprintf("http://%s:%d/", ip, p.SignalPort)
	}
	if p.APIPort > 0 {
		ports = append(ports, p.APIPort)
		services = append(services, graph.Service{Name: "WalkieTalkie Base API", Port: p.APIPort})
		urls["api"] = fmt.Sprintf("http://%s:%d/", ip, p.APIPort)
	}
	if p.RelayPort > 0 {
		ports = append(ports, p.RelayPort)
		services = append(services, graph.Service{Name: "relay", Port: p.RelayPort})
	}
	kind := graph.KindWalkie
	label := p.Name
	if strings.HasPrefix(p.Platform, "desktop-") {
		kind = graph.KindComputer
		if label == "" {
			label = labelForIP(ip)
		}
	}
	id := e.upsertMachine(ip, p.PrimaryMAC, kind, label, "", reverseDNS(ip), []string{"mdns"})
	e.Graph.Upsert(graph.Node{
		ID:               id,
		Kind:             kind,
		Label:            label,
		Nickname:         p.Name,
		MeshID:           p.ID,
		Platform:         p.Platform,
		AppVersion:       p.AppVersion,
		IPs:              []string{ip},
		OpenPorts:        ports,
		Services:         services,
		URLs:             urls,
		DiscoveryMethods: []string{"mdns"},
		Detail:           map[string]any{"sameMachineNote": "All listed services share this IP"},
	})
	if p.GPS != nil {
		e.Graph.Upsert(graph.Node{
			ID:  id,
			GPS: &graph.GPS{Lat: p.GPS.Lat, Lon: p.GPS.Lon, Accuracy: p.GPS.Accuracy, At: p.GPS.Timestamp},
		})
	}
	e.tryIdentify(ip, p.SignalPort, p.APIPort)
}

func (e *Engine) tryIdentify(host string, ports ...int) {
	seen := map[int]bool{}
	for _, p := range ports {
		if p <= 0 || seen[p] || identify.SkipHTTPIdentify(p) {
			continue
		}
		seen[p] = true
		payload, srcURL, err := identify.Probe(host, p, 1200*time.Millisecond)
		if err != nil || payload == nil {
			continue
		}
		e.applyIdentify(host, payload, srcURL)
	}
}

func (e *Engine) applyVirtBBS(host string, open []int) {
	if !virtbbs.LooksLike(open) {
		return
	}
	info, ok := virtbbs.Probe(host, open, 1200*time.Millisecond)
	if !ok {
		return
	}
	id := "host:" + host
	label := info.Name
	if label == "" {
		label = "VirtBBS"
	}
	detail := info.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	detail["discoveryMethods"] = info.Methods
	n := graph.Node{
		ID:               id,
		Kind:             graph.KindComputer,
		Label:            label,
		Nickname:         info.Name,
		Platform:         info.Platform,
		AppVersion:       info.Version,
		IPs:              []string{host},
		Services:         info.Services,
		URLs:             info.URLs,
		DiscoveryMethods: append([]string{"virtbbs"}, info.Methods...),
		Detail:           detail,
	}
	e.Graph.Upsert(n)
}

func (e *Engine) applyIdentify(host string, p *sniff.IdentifyPayload, srcURL string) {
	kind := graph.KindWalkie
	label := p.Name
	if p.Platform == "meshbridge" {
		kind = graph.KindBridge
	} else if p.Platform == "meshsniff" || p.Platform == "virtbbs" || strings.HasPrefix(p.Platform, "desktop-") {
		kind = graph.KindComputer
	}
	var services []graph.Service
	var ports []int
	for _, s := range p.Services {
		name := s.Name
		switch {
		case s.Port == 9091 || name == "api":
			name = "WalkieTalkie Base"
		case s.Port == 9095 || name == "status" && p.Platform == "meshbridge":
			name = "MeshBridge"
		case s.Port == 9096 || p.Platform == "meshsniff":
			name = "MeshSniff"
		}
		services = append(services, graph.Service{Name: name, Port: s.Port, URL: s.URL})
		ports = append(ports, s.Port)
	}
	id := e.upsertMachine(host, firstMAC(p.MACs), kind, label, "", reverseDNS(host), []string{"sniff"})
	n := graph.Node{
		ID:               id,
		Kind:             kind,
		Label:            label,
		Nickname:         p.Name,
		MeshID:           p.MeshID,
		Platform:         p.Platform,
		AppVersion:       p.AppVersion,
		MACs:             p.MACs,
		IPs:              []string{host},
		URLs:             p.URLs,
		Services:         services,
		OpenPorts:        ports,
		DiscoveryMethods: []string{"sniff"},
		Detail: map[string]any{
			"identifyURL":      srcURL,
			"sameMachineNote":  "Services listed below all run on this computer",
		},
	}
	if p.GPS != nil {
		n.GPS = &graph.GPS{Lat: p.GPS.Lat, Lon: p.GPS.Lon, Accuracy: p.GPS.Accuracy, At: p.GPS.Timestamp}
	}
	e.Graph.Upsert(n)
}

// coalesceSameIP merges non-host:* nodes that share an IP into host:<ip>.
func (e *Engine) coalesceSameIP() {
	byIP := map[string][]graph.Node{}
	for _, n := range e.Graph.Nodes() {
		for _, ip := range n.IPs {
			if ip == "" || ip == "127.0.0.1" {
				continue
			}
			byIP[ip] = append(byIP[ip], n)
		}
	}
	for ip, group := range byIP {
		if len(group) < 2 {
			continue
		}
		canon := "host:" + ip
		var merged graph.Node
		merged.ID = canon
		merged.Kind = graph.KindComputer
		merged.IPs = []string{ip}
		seen := map[string]bool{}
		for _, n := range group {
			if seen[n.ID] {
				continue
			}
			seen[n.ID] = true
			e.Graph.Upsert(graph.Node{
				ID:               canon,
				Kind:             n.Kind,
				Label:            n.Label,
				Hostname:         n.Hostname,
				Nickname:         n.Nickname,
				MeshID:           n.MeshID,
				Platform:         n.Platform,
				AppVersion:       n.AppVersion,
				IPs:              n.IPs,
				MACs:             n.MACs,
				OpenPorts:        n.OpenPorts,
				Services:         n.Services,
				URLs:             n.URLs,
				GPS:              n.GPS,
				DiscoveryMethods: append([]string{}, n.DiscoveryMethods...),
				Subnet:           n.Subnet,
				ViaRouter:        n.ViaRouter,
				Detail:           n.Detail,
			})
			if n.ID != canon {
				e.Graph.Relink(n.ID, canon)
				e.Graph.Delete(n.ID)
			}
		}
		_ = merged
	}
}

func (e *Engine) linkViaRouters(subs []netinfo.Subnet, gatewayBySubnet, routerIDs map[string]string) {
	for _, n := range e.Graph.Nodes() {
		if n.Kind == graph.KindNetwork || n.Kind == graph.KindSubnet || n.Kind == graph.KindRemoteHint {
			continue
		}
		for _, ip := range n.IPs {
			subnet := matchSubnet(ip, subs)
			gw := gatewayBySubnet[subnet]
			if gw == "" || ip == gw {
				continue
			}
			routerID := routerIDs[gw]
			if routerID == "" {
				routerID = "host:" + gw
			}
			if n.ID == routerID {
				continue
			}
			e.Graph.Upsert(graph.Node{ID: n.ID, ViaRouter: gw, Subnet: subnet})
			e.Graph.Link(routerID, n.ID, "via-router", false)
			e.Graph.Link("subnet:"+subnet, n.ID, "lan", false)
		}
	}
}

func matchSubnet(ip string, subs []netinfo.Subnet) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	for _, s := range subs {
		_, n, err := net.ParseCIDR(s.CIDR)
		if err != nil {
			continue
		}
		if n.Contains(parsed) {
			return s.CIDR
		}
	}
	return ""
}

func isLikelyRouter(ip string, gatewayBySubnet map[string]string) bool {
	for _, gw := range gatewayBySubnet {
		if gw == ip {
			return true
		}
	}
	return strings.HasSuffix(ip, ".1") || strings.HasSuffix(ip, ".254")
}

func labelForIP(ip string) string {
	if hn := reverseDNS(ip); hn != "" {
		return hn
	}
	return ip
}

func reverseDNS(ip string) string {
	names, err := net.LookupAddr(ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	h := strings.TrimSuffix(names[0], ".")
	if i := strings.IndexByte(h, '.'); i > 0 {
		// Keep short local hostname when possible
		if !strings.Contains(h[i+1:], ".") {
			return h
		}
		return h[:i]
	}
	return h
}

func servicesFromPorts(ports []int) []graph.Service {
	var out []graph.Service
	for _, p := range ports {
		out = append(out, graph.Service{Name: portName(p), Port: p})
	}
	return out
}

func portName(p int) string {
	switch p {
	case 22:
		return "ssh"
	case 53:
		return "dns"
	case 80:
		return "http"
	case 139:
		return "netbios"
	case 443:
		return "https"
	case 445:
		return "smb"
	case 548:
		return "afp"
	case 631:
		return "ipp"
	case 2323:
		return "VirtBBS Telnet"
	case 3232:
		return "VirtBBS SSH"
	case 3389:
		return "rdp"
	case 5900:
		return "vnc"
	case 8081:
		return "VirtBBS Web"
	case 9091:
		return "WalkieTalkie Base"
	case 9095:
		return "MeshBridge"
	case 9096:
		return "MeshSniff"
	case 9998:
		return "VirtBBS API"
	case 24554:
		return "VirtBBS BinkP Lovly"
	case 24555:
		return "VirtBBS BinkP VirtNet"
	default:
		return fmt.Sprintf("tcp/%d", p)
	}
}

func firstMAC(macs []string) string {
	if len(macs) == 0 {
		return ""
	}
	return macs[0]
}

func unionPorts(a, b []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, p := range append(a, b...) {
		if p <= 0 || p > 65535 || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
