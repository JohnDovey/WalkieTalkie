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
	"github.com/wlynxg/anet"
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
	AppVersion string // the announcing app's own Major.Minor.Patch version
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

	// RelayPort is the Base Station SFU signaling port (server/relay). Zero
	// means "no SFU" and is omitted from TXT. Mobile/desktop clients use
	// this for DialViaRelay force-threshold / ICE-fail fallback.
	RelayPort int
}

// Peer is a sighting of another node, decoded from its TXT record.
type Peer struct {
	ID         string
	Name       string
	Platform   string
	AppVersion string
	ProtoVer   int
	SignalPort int
	APIPort    int // 0 if the peer isn't a Base Station (no server/api)
	RelayPort  int // 0 if the peer isn't hosting an SFU
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
		"appver=" + info.AppVersion,
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
	if info.RelayPort != 0 {
		txt = append(txt, "relay="+strconv.Itoa(info.RelayPort))
	}
	return txt
}

func parseTXT(text []string) (id, name, plat, appVersion string, ver, sig, api, relay int, gps *proto.GeoPoint) {
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
		case "appver":
			appVersion = parts[1]
		case "ver":
			ver, _ = strconv.Atoi(parts[1])
		case "sig":
			sig, _ = strconv.Atoi(parts[1])
		case "api":
			api, _ = strconv.Atoi(parts[1])
		case "relay":
			relay, _ = strconv.Atoi(parts[1])
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
//
// Uses zeroconf.RegisterProxy (supplying IPs/interfaces ourselves via
// wlynxg/anet) rather than zeroconf.Register — found the hard way, on real
// Android hardware: zeroconf.Register's own interface/address auto-
// detection calls the stdlib net.Interface.Addrs() per interface, which
// returns nothing usable on Android (the same gap wlynxg/anet exists to
// paper over for pion's ICE code — see the gomobile-bind ldflags note in
// tools/gomobile-bind-android.sh). Without this, Register failed on-device
// with "Could not determine host IP addresses" even though the phone had
// a perfectly normal Wi-Fi IP.
func Register(info AnnounceInfo) (*Server, error) {
	ips, err := hostIPs()
	if err != nil {
		return nil, fmt.Errorf("mdns: register: %w", err)
	}
	zc, err := zeroconf.RegisterProxy(info.ID, ServiceType, Domain, info.Port, info.ID, ips, buildTXT(info), multicastInterfaces())
	if err != nil {
		return nil, fmt.Errorf("mdns: register: %w", err)
	}
	return &Server{zc: zc}, nil
}

// hostIPs returns every LAN-reachable IPv4 address this node has, via
// wlynxg/anet (works on Android, unlike net.InterfaceAddrs/Interface.Addrs).
//
// Filters by *interface flags*, not just IP address range — found the hard
// way, on real Android hardware with mobile data also enabled. The first
// attempt filtered to RFC 1918 private ranges, which wasn't enough: on this
// particular (MediaTek-chipset) phone, the cellular modem exposes two
// interfaces — "ccmni0" with a public-looking carrier IP, but *also*
// "ccmni1" with a 10.x CGNAT address indistinguishable from a real private
// LAN address by range alone. Both are point-to-point cellular links, not
// LAN segments — confirmed via `ip addr`: wlan0 has BROADCAST set, both
// ccmni interfaces don't. Requiring FlagBroadcast (LAN/Wi-Fi interfaces
// have it; point-to-point cellular links don't) reliably excludes them
// regardless of what IP range the carrier happens to hand out. Kept the
// private-range check too as a second, complementary filter.
func hostIPs() ([]string, error) {
	ifaces, err := anet.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("enumerate interfaces: %w", err)
	}

	var ips []string
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, err := anet.InterfaceAddrsByInterface(&ifi)
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipNet.IP.To4()
			if v4 == nil || ipNet.IP.IsLoopback() || !isPrivateIPv4(v4) {
				continue
			}
			ips = append(ips, v4.String())
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no usable LAN IPv4 addresses found")
	}
	return ips, nil
}

// multicastInterfaces returns the up + multicast-capable interfaces, via
// wlynxg/anet, for zeroconf to bind its multicast sockets to. Falls back to
// zeroconf's own detection (nil) if anet can't enumerate interfaces either.
func multicastInterfaces() []net.Interface {
	ifaces, err := anet.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.Interface
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		out = append(out, ifi)
	}
	return out
}

// browseRestartInterval bounds how long a single underlying
// zeroconf.Resolver/Browse call runs before Browse tears it down and starts
// a fresh one. This works around a real bug (not just a quirk) in vendored
// github.com/grandcat/zeroconf@v1.0.0's client.go: once its mainloop has
// delivered a ServiceEntry for a given instance name once, it's added to an
// internal sentEntries map and never delivered again for the lifetime of
// that resolver ("if _, ok := sentEntries[k]; ok { continue }") — found the
// hard way, on real hardware: a phone's GPS position, name, and app version
// all silently froze at whatever they were on first sighting, no matter how
// many times the phone re-announced fresh TXT data, because the desktop
// Base Station's Browse() call had already "seen" that phone once and
// zeroconf refused to redeliver it. Periodically restarting with a brand
// new resolver (which has an empty sentEntries map) is the pragmatic fix
// without forking a third-party dependency — it forces every still-present
// peer's current TXT content back through onFound at least this often.
const browseRestartInterval = 60 * time.Second

// Browse watches the LAN for other WalkieTalkie nodes and calls onFound for
// every sighting. onFound must be an idempotent upsert (see
// registry.UpsertFromDirectContact), not treated as a one-time "device
// joined" event — both because zeroconf itself may re-deliver an entry, and
// because Browse periodically restarts its underlying resolver (see
// browseRestartInterval) to work around zeroconf's own never-redeliver bug.
// Browse blocks until ctx is cancelled.
//
// Known gap (tracked in the plan's risks): this does not yet surface
// explicit "peer left" events; staleness is instead handled by a
// LastSeen-based sweep in the caller (registry.Store.SweepStale).
func Browse(ctx context.Context, selfID string, onFound func(Peer)) error {
	for {
		iterCtx, cancel := context.WithTimeout(ctx, browseRestartInterval)
		err := browseOnce(iterCtx, selfID, onFound)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		// iterCtx's own timeout elapsed (not the outer ctx) — loop and start
		// a fresh resolver.
	}
}

// browseOnce runs a single zeroconf resolver/Browse session until ctx is
// cancelled (by the caller, or by browseRestartInterval elapsing).
func browseOnce(ctx context.Context, selfID string, onFound func(Peer)) error {
	// Same Android fix as Register: zeroconf.NewResolver(nil) defaults to
	// its own interface enumeration (net.Interfaces()), which returns
	// nothing usable on Android — confirmed on real hardware ("failed to
	// join any of these interfaces: []"). Supply the interface list
	// ourselves via wlynxg/anet, same as Register does.
	resolver, err := zeroconf.NewResolver(zeroconf.SelectIfaces(multicastInterfaces()))
	if err != nil {
		return fmt.Errorf("mdns: new resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go func() {
		for entry := range entries {
			id, name, plat, appVersion, ver, sig, api, relay, gps := parseTXT(entry.Text)
			if id == "" || id == selfID {
				continue
			}
			onFound(Peer{
				ID:         id,
				Name:       name,
				Platform:   plat,
				AppVersion: appVersion,
				ProtoVer:   ver,
				SignalPort: sig,
				APIPort:    api,
				RelayPort:  relay,
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

// isPrivateIPv4 reports whether ip is in one of the RFC 1918 private
// ranges (10/8, 172.16/12, 192.168/16) — see hostIPs for why this matters.
func isPrivateIPv4(ip net.IP) bool {
	return ip[0] == 10 ||
		(ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31) ||
		(ip[0] == 192 && ip[1] == 168)
}
