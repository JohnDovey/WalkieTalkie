package engine

import (
	"context"
	"fmt"
	"log"
	"net"
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
	"github.com/JohnDovey/WalkieTalkie/meshsniff/seed"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/tcpprobe"
)

// Engine runs seed + continuous LAN discovery into a graph.Store.
type Engine struct {
	Settings config.Settings
	Graph    *graph.Store

	mu sync.Mutex
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
	if inv, err := seed.FetchBridgeInventory(e.Settings.MeshBridgeURL); err == nil {
		seed.ApplyBridge(e.Graph, inv)
		st.MeshBridgeOK = true
	} else {
		st.MeshBridgeOK = false
		st.Message = joinMsg(st.Message, "MeshBridge: "+err.Error())
	}
	if devices, err := seed.FetchDevices(e.Settings.LocalBaseURL); err == nil {
		seed.ApplyBaseDevices(e.Graph, devices)
		st.BaseOK = true
	} else {
		st.BaseOK = false
		st.Message = joinMsg(st.Message, "Base: "+err.Error())
	}
	if remotes, err := seed.FetchRemoteDevices(e.Settings.LocalBaseURL); err == nil {
		seed.ApplyRemoteDevices(e.Graph, remotes)
	}
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
			gwID := e.upsertHost(s.Gateway, "", graph.KindRouter, "Gateway "+s.Gateway, s.CIDR, []string{"gateway"})
			e.Graph.Link(subnetID, gwID, "gateway", false)
		}
	}

	if entries, err := arp.Table(); err == nil {
		for _, ent := range entries {
			kind := graph.KindHost
			label := ent.IP
			if strings.HasSuffix(ent.IP, ".1") || strings.HasSuffix(ent.IP, ".254") {
				kind = graph.KindRouter
				label = "Router " + ent.IP
			}
			subnet := matchSubnet(ent.IP, subs)
			id := e.upsertHost(ent.IP, ent.MAC, kind, label, subnet, []string{"arp"})
			if subnet != "" {
				e.Graph.Link("subnet:"+subnet, id, "lan", false)
			}
		}
	}

	// mDNS walkietalkie peers
	mdnsCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	_ = mdns.Browse(mdnsCtx, "", func(p mdns.Peer) {
		ips := []string{}
		for _, ip := range p.IPv4 {
			ips = append(ips, ip.String())
		}
		ports := []int{}
		services := []graph.Service{}
		urls := map[string]string{}
		if p.SignalPort > 0 {
			ports = append(ports, p.SignalPort)
			services = append(services, graph.Service{Name: "signaling", Port: p.SignalPort})
			if len(ips) > 0 {
				urls["signaling"] = fmt.Sprintf("http://%s:%d/", ips[0], p.SignalPort)
			}
		}
		if p.APIPort > 0 {
			ports = append(ports, p.APIPort)
			services = append(services, graph.Service{Name: "api", Port: p.APIPort})
			if len(ips) > 0 {
				urls["api"] = fmt.Sprintf("http://%s:%d/", ips[0], p.APIPort)
			}
		}
		if p.RelayPort > 0 {
			ports = append(ports, p.RelayPort)
			services = append(services, graph.Service{Name: "relay", Port: p.RelayPort})
		}
		n := graph.Node{
			ID:               "walkie:" + p.ID,
			Kind:             graph.KindWalkie,
			Label:            p.Name,
			Nickname:         p.Name,
			MeshID:           p.ID,
			Platform:         p.Platform,
			AppVersion:       p.AppVersion,
			IPs:              ips,
			OpenPorts:        ports,
			Services:         services,
			URLs:             urls,
			DiscoveryMethods: []string{"mdns"},
		}
		if p.PrimaryMAC != "" {
			n.MACs = []string{p.PrimaryMAC}
		}
		if p.GPS != nil {
			n.GPS = &graph.GPS{Lat: p.GPS.Lat, Lon: p.GPS.Lon, Accuracy: p.GPS.Accuracy, At: p.GPS.Timestamp}
		}
		id := e.Graph.Upsert(n)
		for _, ip := range ips {
			e.tryIdentify(ip, p.SignalPort, p.APIPort)
			_ = ip
		}
		_ = id
	})
	cancel()

	// TCP probe ARP hosts + limited CIDR sweep
	ports := append([]int(nil), e.Settings.Ports...)
	hostSet := map[string]bool{}
	if entries, err := arp.Table(); err == nil {
		for _, ent := range entries {
			hostSet[ent.IP] = true
		}
	}
	for _, cidr := range cidrs {
		hosts, err := netinfo.HostsInCIDR(cidr)
		if err != nil {
			continue
		}
		// Prefer ARP + gateways; sample first/last of CIDR for light probe
		for i, h := range hosts {
			if i < 3 || i >= len(hosts)-2 || strings.HasSuffix(h, ".1") {
				hostSet[h] = true
			}
		}
		if alive := icmp.Sweep(hosts, 200*time.Millisecond, 48); len(alive) > 0 {
			for _, h := range alive {
				hostSet[h] = true
				e.upsertHost(h, "", graph.KindHost, h, cidr, []string{"icmp"})
			}
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
			open := tcpprobe.OpenPorts(host, ports, 350*time.Millisecond)
			if len(open) == 0 {
				return
			}
			kind := graph.KindHost
			label := host
			if containsPort(open, 80) || containsPort(open, 443) || containsPort(open, 53) {
				if strings.HasSuffix(host, ".1") {
					kind = graph.KindRouter
					label = "Router " + host
				}
			}
			subnet := matchSubnet(host, subs)
			id := e.upsertHost(host, "", kind, label, subnet, []string{"tcp"})
			e.Graph.Upsert(graph.Node{ID: id, Kind: kind, Label: label, IPs: []string{host}, OpenPorts: open, Subnet: subnet, DiscoveryMethods: []string{"tcp"}})
			for _, p := range open {
				e.tryIdentify(host, p, 0)
			}
			if subnet != "" {
				e.Graph.Link("subnet:"+subnet, id, "lan", false)
			}
		}()
	}
	wg.Wait()

	st := e.Graph.Snapshot().Status
	st.LastScan = time.Since(start).Round(time.Millisecond).String()
	st.CIDRs = cidrs
	st.ICMPEnabled = icmp.Enabled()
	st.Message = icmp.StatusMessage()
	e.Graph.SetStatus(st)
}

func (e *Engine) upsertHost(ip, mac string, kind graph.Kind, label, subnet string, methods []string) string {
	id := "host:" + ip
	n := graph.Node{
		ID:               id,
		Kind:             kind,
		Label:            label,
		IPs:              []string{ip},
		Subnet:           subnet,
		DiscoveryMethods: methods,
	}
	if mac != "" {
		n.MACs = []string{strings.ToLower(mac)}
	}
	return e.Graph.Upsert(n)
}

func (e *Engine) tryIdentify(host string, ports ...int) {
	seen := map[int]bool{}
	for _, p := range ports {
		if p <= 0 || seen[p] {
			continue
		}
		seen[p] = true
		payload, url, err := identify.Probe(host, p, 1200*time.Millisecond)
		if err != nil || payload == nil {
			continue
		}
		e.applyIdentify(host, payload, url)
	}
}

func (e *Engine) applyIdentify(host string, p *sniff.IdentifyPayload, srcURL string) {
	n := graph.Node{
		ID:               "walkie:" + p.MeshID,
		Kind:             graph.KindWalkie,
		Label:            p.Name,
		Nickname:         p.Name,
		MeshID:           p.MeshID,
		Platform:         p.Platform,
		AppVersion:       p.AppVersion,
		MACs:             p.MACs,
		IPs:              []string{host},
		URLs:             p.URLs,
		DiscoveryMethods: []string{"sniff"},
		Detail:           map[string]any{"identifyURL": srcURL},
	}
	if p.Platform == "meshbridge" {
		n.Kind = graph.KindBridge
		n.ID = "bridge:" + p.MeshID
	}
	for _, s := range p.Services {
		n.Services = append(n.Services, graph.Service{Name: s.Name, Port: s.Port, URL: s.URL})
		n.OpenPorts = append(n.OpenPorts, s.Port)
	}
	if p.GPS != nil {
		n.GPS = &graph.GPS{Lat: p.GPS.Lat, Lon: p.GPS.Lon, Accuracy: p.GPS.Accuracy, At: p.GPS.Timestamp}
	}
	e.Graph.Upsert(n)
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

func containsPort(ports []int, p int) bool {
	for _, x := range ports {
		if x == p {
			return true
		}
	}
	return false
}
