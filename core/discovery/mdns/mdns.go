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

	"github.com/grandcat/zeroconf"
)

const (
	// ServiceType is the DNS-SD service type all WalkieTalkie nodes
	// advertise under.
	ServiceType = "_walkietalkie._tcp"
	Domain      = "local."
)

// AnnounceInfo is what this node advertises about itself.
type AnnounceInfo struct {
	ID         string // instance name — stable device ID, not the display name
	Name       string
	Platform   string
	ProtoVer   int
	Port       int // service port; == SignalPort for this app
	SignalPort int
}

// Peer is a sighting of another node, decoded from its TXT record.
type Peer struct {
	ID         string
	Name       string
	Platform   string
	ProtoVer   int
	SignalPort int
	Host       string
	IPv4       []net.IP
	IPv6       []net.IP
}

func buildTXT(info AnnounceInfo) []string {
	return []string{
		"id=" + info.ID,
		"name=" + url.QueryEscape(info.Name),
		"plat=" + info.Platform,
		"ver=" + strconv.Itoa(info.ProtoVer),
		"sig=" + strconv.Itoa(info.SignalPort),
	}
}

func parseTXT(text []string) (id, name, plat string, ver, sig int) {
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
		}
	}
	return
}

// Register advertises this node's presence on the LAN. Call Shutdown() on
// the returned server when the node goes offline.
//
// Open question flagged in the plan: whether zeroconf supports updating an
// already-registered TXT record in place (e.g. on a local rename) or needs
// Shutdown()+Register() again — assume the latter until verified.
func Register(info AnnounceInfo) (*zeroconf.Server, error) {
	srv, err := zeroconf.Register(info.ID, ServiceType, Domain, info.Port, buildTXT(info), nil)
	if err != nil {
		return nil, fmt.Errorf("mdns: register: %w", err)
	}
	return srv, nil
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
			id, name, plat, ver, sig := parseTXT(entry.Text)
			if id == "" || id == selfID {
				continue
			}
			onFound(Peer{
				ID:         id,
				Name:       name,
				Platform:   plat,
				ProtoVer:   ver,
				SignalPort: sig,
				Host:       entry.HostName,
				IPv4:       entry.AddrIPv4,
				IPv6:       entry.AddrIPv6,
			})
		}
	}()

	if err := resolver.Browse(ctx, ServiceType, Domain, entries); err != nil {
		return fmt.Errorf("mdns: browse: %w", err)
	}
	<-ctx.Done()
	return nil
}
