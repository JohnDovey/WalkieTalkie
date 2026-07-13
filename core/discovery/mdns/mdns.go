// Package mdns implements LAN discovery for WalkieTalkie nodes over
// mDNS/DNS-SD (grandcat/zeroconf), per docs/2026-07-13-implementation-plan.md
// ("Discovery layer"). This is plain UDP multicast, so the same code path
// works on desktop and, via gomobile, on Android/iOS — no platform-specific
// NSD/Bonjour APIs needed.
package mdns

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/proto"
	"github.com/grandcat/zeroconf"
)

const (
	// ServiceType is the DNS-SD service type all WalkieTalkie nodes
	// advertise under.
	ServiceType = "_walkietalkie._tcp"
	Domain      = "local."
)

// AnnounceInfo is what this node advertises about itself.
//
// GPS is optional (nil if this node has no fix yet). Including it in the
// TXT record lets GPS propagate to peers through the same discovery
// mechanism they already use to find this node — no separate gossip/push
// API needed on nodes (like a phone) that don't run the full server/api.
// Re-announcing (Register again) after a GPS update is the accepted way to
// refresh it, per the plan's GPS-update-interval requirement.
type AnnounceInfo struct {
	ID         string // instance name — stable device ID, not the display name
	Name       string
	Platform   string
	ProtoVer   int
	Port       int // service port; == SignalPort for this app
	SignalPort int
	GPS        *proto.GeoPoint

	// APIPort is the server REST API port (server/api), set only by
	// server/main.go — never by core/mobile, since mobile nodes don't run
	// that API. A peer's APIPort being non-zero is how one Base Station
	// recognizes another for registry sync (see the plan's
	// "Multi-Base-Station synchronization" section). Zero means "not a
	// Base Station" and is omitted from the TXT record entirely.
	APIPort int
}

// Peer is a sighting of another node, decoded from its TXT record.
type Peer struct {
	ID         string
	Name       string
	Platform   string
	ProtoVer   int
	SignalPort int
	APIPort    int // 0 if the peer isn't a Base Station (no server/api)
	Host       string
	IPv4       []net.IP
	IPv6       []net.IP
	GPS        *proto.GeoPoint
}

func buildTXT(info AnnounceInfo) []string {
	txt := []string{
		"id=" + info.ID,
		"name=" + url.QueryEscape(info.Name),
		"plat=" + info.Platform,
		"ver=" + strconv.Itoa(info.ProtoVer),
		"sig=" + strconv.Itoa(info.SignalPort),
	}
	if info.GPS != nil {
		txt = append(txt,
			"lat="+strconv.FormatFloat(info.GPS.Lat, 'f', -1, 64),
			"lon="+strconv.FormatFloat(info.GPS.Lon, 'f', -1, 64),
			"acc="+strconv.FormatFloat(info.GPS.Accuracy, 'f', -1, 64),
		)
	}
	if info.APIPort != 0 {
		txt = append(txt, "api="+strconv.Itoa(info.APIPort))
	}
	return txt
}

func parseTXT(text []string) (id, name, plat string, ver, sig, api int, gps *proto.GeoPoint) {
	var lat, lon, acc float64
	var hasLat, hasLon bool
	for _, kv := range text {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "id":
			id = parts[1]
		case "name":
			if v, err := url.QueryUnescape(parts[1]); err == nil {
				name = v
			}
		case "plat":
			plat = parts[1]
		case "ver":
			ver, _ = strconv.Atoi(parts[1])
		case "sig":
			sig, _ = strconv.Atoi(parts[1])
		case "api":
			api, _ = strconv.Atoi(parts[1])
		case "lat":
			lat, hasLat = parseFloat(parts[1])
		case "lon":
			lon, hasLon = parseFloat(parts[1])
		case "acc":
			acc, _ = parseFloat(parts[1])
		}
	}
	if hasLat && hasLon {
		gps = &proto.GeoPoint{Lat: lat, Lon: lon, Accuracy: acc, Timestamp: time.Now()}
	}
	return
}

func parseFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

// Server is this node's mDNS advertisement, wrapping zeroconf so callers
// never need to import it directly.
type Server struct {
	zc *zeroconf.Server
}

// UpdateInfo updates the advertised TXT record in place — confirmed via
// zeroconf.Server.SetText, resolving what was an open question in the plan
// (whether a full Shutdown()+Register() would be needed instead). Use this
// after a GPS fix or a local rename rather than re-registering.
func (s *Server) UpdateInfo(info AnnounceInfo) {
	s.zc.SetText(buildTXT(info))
}

// Shutdown stops advertising this node.
func (s *Server) Shutdown() {
	s.zc.Shutdown()
}

// Register advertises this node's presence on the LAN. Call Shutdown() on
// the returned Server when the node goes offline, or UpdateInfo to refresh
// GPS/name without a full teardown.
func Register(info AnnounceInfo) (*Server, error) {
	zc, err := zeroconf.Register(info.ID, ServiceType, Domain, info.Port, buildTXT(info), nil)
	if err != nil {
		return nil, fmt.Errorf("mdns: register: %w", err)
	}
	return &Server{zc: zc}, nil
}

// Browse watches the LAN for other WalkieTalkie nodes and calls onFound for
// every sighting. zeroconf periodically re-delivers already-known entries,
// so onFound must be an idempotent upsert (see registry.UpsertFromDirectContact),
// not treated as a one-time "device joined" event. Browse blocks until ctx
// is cancelled.
//
// Known gap (tracked in the plan's risks): this does not yet surface
// explicit "peer left" events; staleness is instead expected to be handled
// by a LastSeen-based sweep in the caller.
func Browse(ctx context.Context, selfID string, onFound func(Peer)) error {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return fmt.Errorf("mdns: new resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go func() {
		for entry := range entries {
			id, name, plat, ver, sig, api, gps := parseTXT(entry.Text)
			if id == "" || id == selfID {
				continue
			}
			onFound(Peer{
				ID:         id,
				Name:       name,
				Platform:   plat,
				ProtoVer:   ver,
				SignalPort: sig,
				APIPort:    api,
				Host:       entry.HostName,
				IPv4:       entry.AddrIPv4,
				IPv6:       entry.AddrIPv6,
				GPS:        gps,
			})
		}
	}()

	if err := resolver.Browse(ctx, ServiceType, Domain, entries); err != nil {
		return fmt.Errorf("mdns: browse: %w", err)
	}
	<-ctx.Done()
	return nil
}
