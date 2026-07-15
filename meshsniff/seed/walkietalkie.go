package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/discovery/mdns"
	"github.com/JohnDovey/WalkieTalkie/core/registry"
	"github.com/JohnDovey/WalkieTalkie/core/sniff"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/graph"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/netinfo"
)

// AboutInfo is Base Station GET /api/about.
type AboutInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Version  string `json:"version"`
}

// WalkieSeedResult summarizes WalkieTalkie seeding for the status strip.
type WalkieSeedResult struct {
	OK          bool
	BaseID      string
	BaseName    string
	DeviceCount int
	RemoteCount int
	MDNSBases   int
	Err         string
}

// FetchAbout loads GET /api/about from a Base Station.
func FetchAbout(baseURL string) (*AboutInfo, error) {
	raw, err := getJSON(baseURL + "/api/about")
	if err != nil {
		return nil, err
	}
	var a AboutInfo
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// FetchIdentify loads GET /api/sniff (fallback /sniff) from a WalkieTalkie HTTP port.
func FetchIdentify(baseURL string) (*sniff.IdentifyPayload, error) {
	raw, err := getJSON(baseURL + "/api/sniff")
	if err != nil {
		raw, err = getJSON(baseURL + "/sniff")
		if err != nil {
			return nil, err
		}
	}
	var p sniff.IdentifyPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ApplyWalkieTalkie seeds the map from one WalkieTalkie Base Station:
// About + identify hub, local devices, Remote Users — linked under the Base hub.
func ApplyWalkieTalkie(g *graph.Store, baseURL string) WalkieSeedResult {
	res := WalkieSeedResult{}
	about, err := FetchAbout(baseURL)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	res.OK = true
	res.BaseID = about.ID
	res.BaseName = about.Name

	hubID := "walkiebase:" + about.ID
	if about.ID == "" {
		hubID = "walkiebase:local"
	}
	n := graph.Node{
		ID:               hubID,
		Kind:             graph.KindWalkie,
		Label:            about.Name,
		Nickname:         about.Name,
		MeshID:           about.ID,
		Platform:         about.Platform,
		AppVersion:       about.Version,
		DiscoveryMethods: []string{"walkietalkie"},
		URLs:             map[string]string{"api": trailingSlash(baseURL)},
		Services:         []graph.Service{{Name: "WalkieTalkie Base", URL: trailingSlash(baseURL)}},
		Detail: map[string]any{
			"role":       "base-station",
			"seededFrom": "walkietalkie",
			"baseURL":    baseURL,
		},
	}
	// When Base is local (or we have LAN context), attach this machine's IPs so
	// Base / MeshBridge / MeshSniff coalesce onto one computer on the map.
	if host, _, ok := hostPortFromURL(baseURL); ok && (host == "127.0.0.1" || host == "localhost" || host == "::1") {
		me := netinfo.ThisMachine()
		n.IPs = me.IPs
		n.MACs = me.MACs
		n.Hostname = me.Hostname
		n.Kind = graph.KindComputer
		if me.Hostname != "" {
			n.Label = me.Hostname + " · " + about.Name
		}
		n.Detail["sameMachineNote"] = "WalkieTalkie services on this computer share these IPs"
		if len(me.IPs) > 0 {
			n.ID = "host:" + me.IPs[0]
			hubID = n.ID
		}
	} else if host, _, ok := hostPortFromURL(baseURL); ok {
		n.IPs = []string{host}
		n.ID = "host:" + host
		hubID = n.ID
	}
	if id, err := FetchIdentify(baseURL); err == nil && id != nil {
		if id.MeshID != "" {
			n.MeshID = id.MeshID
		}
		if id.Name != "" {
			n.Nickname = id.Name
			if n.Hostname == "" {
				n.Label = id.Name
			}
		}
		n.MACs = append(n.MACs, id.MACs...)
		n.URLs = mergeURLMaps(n.URLs, id.URLs)
		for _, s := range id.Services {
			name := s.Name
			if s.Port == 9091 || name == "api" {
				name = "WalkieTalkie Base"
			}
			n.Services = append(n.Services, graph.Service{Name: name, Port: s.Port, URL: s.URL})
			if s.Port > 0 {
				n.OpenPorts = append(n.OpenPorts, s.Port)
			}
		}
		if id.GPS != nil {
			n.GPS = &graph.GPS{Lat: id.GPS.Lat, Lon: id.GPS.Lon, Accuracy: id.GPS.Accuracy, At: id.GPS.Timestamp}
		}
		if id.AppVersion != "" {
			n.AppVersion = id.AppVersion
		}
		if id.Platform != "" {
			n.Platform = id.Platform
			if strings.HasPrefix(id.Platform, "desktop-") {
				n.Kind = graph.KindComputer
			}
		}
	}
	hubID = g.Upsert(n)

	if devices, err := FetchDevices(baseURL); err == nil {
		for _, d := range devices {
			if d == nil || d.ID == "" {
				continue
			}
			devID := g.Upsert(deviceToWalkieNode(d, "walkietalkie"))
			g.Link(hubID, devID, "walkietalkie", false)
			res.DeviceCount++
		}
	} else {
		res.Err = "devices: " + err.Error()
	}

	if remotes, err := FetchRemoteDevices(baseURL); err == nil {
		for _, rd := range remotes {
			d := rd.Device
			node := deviceToWalkieNode(&d, "walkietalkie-remote")
			node.Kind = graph.KindRemoteHint
			if d.LastLANIP == "" {
				node.ID = "remote:" + d.ID
			}
			node.RemoteBaseID = rd.RemoteBaseID
			node.RemoteBaseName = rd.RemoteBaseName
			node.DiscoveryMethods = []string{"walkietalkie", "remote-users"}
			id := g.Upsert(node)
			g.Link(hubID, id, "remote", true)
			res.RemoteCount++
		}
	}
	return res
}

func deviceToWalkieNode(d *registry.Device, method string) graph.Node {
	n := graph.Node{
		ID:               "dev:" + d.ID,
		Kind:             graph.KindWalkie,
		Label:            d.Name,
		Nickname:         d.Name,
		MeshID:           d.ID,
		Platform:         d.Platform,
		AppVersion:       d.AppVersion,
		MACs:             d.MacAddresses,
		DiscoveryMethods: append([]string{method}, d.DiscoveryMethods...),
		Detail: map[string]any{
			"seededFrom": "walkietalkie",
			"status":     string(d.Status),
		},
	}
	if d.LastLANIP != "" {
		n.IPs = []string{d.LastLANIP}
		n.ID = "host:" + d.LastLANIP
		n.DiscoveryMethods = append(n.DiscoveryMethods, "last-lan-ip")
	}
	loc := d.CurrentLocation
	if loc == nil {
		loc = d.LastKnownLocation
	}
	if loc != nil {
		n.GPS = &graph.GPS{Lat: loc.Lat, Lon: loc.Lon, Accuracy: loc.Accuracy, At: loc.Timestamp}
	}
	return n
}

// SeedMDNSBases browses for other WalkieTalkie Base Stations (api≠0) and seeds each.
func SeedMDNSBases(ctx context.Context, g *graph.Store, skipBaseURL string) (bases, devices int) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	seen := map[string]bool{}
	if host, port, ok := hostPortFromURL(skipBaseURL); ok {
		seen[net.JoinHostPort(host, strconv.Itoa(port))] = true
	}

	var mu sync.Mutex
	_ = mdns.Browse(ctx, "", func(p mdns.Peer) {
		if p.APIPort == 0 || len(p.IPv4) == 0 {
			return
		}
		host := p.IPv4[0].String()
		key := net.JoinHostPort(host, strconv.Itoa(p.APIPort))
		mu.Lock()
		dup := seen[key]
		if !dup {
			seen[key] = true
		}
		mu.Unlock()
		if dup {
			return
		}

		baseURL := fmt.Sprintf("http://%s:%d", host, p.APIPort)
		res := ApplyWalkieTalkie(g, baseURL)
		mu.Lock()
		defer mu.Unlock()
		if !res.OK {
			return
		}
		bases++
		devices += res.DeviceCount + res.RemoteCount
		hubID := "walkiebase:" + res.BaseID
		g.Upsert(graph.Node{
			ID:               hubID,
			Kind:             graph.KindWalkie,
			IPs:              []string{host},
			OpenPorts:        []int{p.APIPort, p.SignalPort},
			DiscoveryMethods: []string{"walkietalkie", "mdns"},
			Detail:           map[string]any{"seededFrom": "walkietalkie-mdns", "baseURL": baseURL},
		})
	})
	return bases, devices
}

func trailingSlash(u string) string {
	if u == "" {
		return u
	}
	if u[len(u)-1] == '/' {
		return u
	}
	return u + "/"
}

func mergeURLMaps(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func hostPortFromURL(raw string) (host string, port int, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", 0, false
	}
	h := u.Hostname()
	p := u.Port()
	if p == "" {
		if u.Scheme == "https" {
			return h, 443, true
		}
		return h, 80, true
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, false
	}
	return h, n, true
}
