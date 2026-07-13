// Package mobile is the gomobile-bind facade over core: gomobile only
// supports a restricted subset of Go (no generics, limited struct/
// interface support, primitive-typed function signatures — string, bool,
// numeric types, []byte, error, and other bound types), so this package
// exists to keep that constraint from leaking into the rest of core's
// design. See docs/2026-07-13-implementation-plan.md ("Monorepo layout").
//
// Node is the mobile equivalent of server/main.go's wiring: the same
// shared core (registry, mDNS discovery/announce, WebRTC mesh), with the
// platform-native mic/speaker (media.AudioSource/AudioSink), BLE scanning,
// and GPS left to the native Android/iOS shell to provide.
package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/config"
	"github.com/JohnDovey/WalkieTalkie/core/discovery/mdns"
	"github.com/JohnDovey/WalkieTalkie/core/media"
	"github.com/JohnDovey/WalkieTalkie/core/proto"
	"github.com/JohnDovey/WalkieTalkie/core/registry"
)

// Node is the handle a native app holds for its whole lifetime.
type Node struct {
	selfID     string
	platform   string
	appVersion string
	sigPort    int

	store   *registry.Store
	session *media.PTTSession

	cancelBrowse context.CancelFunc

	mu      sync.Mutex
	name    string
	lastGPS *proto.GeoPoint
	mdnsSrv *mdns.Server
}

// StartNode boots the shared core for one local device: opens the
// registry at dataDir, starts the WebRTC mesh's signaling listener,
// advertises this node over mDNS, and begins browsing for peers to
// connect to. source/sink are the platform-native audio bridge (mic
// capture+Opus encode / Opus decode+speaker playback) — see
// core/media.AudioSource/AudioSink, implemented in Kotlin/Swift and passed
// in via the gomobile-generated callback bindings.
func StartNode(dataDir, name, platform, appVersion string, source media.AudioSource, sink media.AudioSink) (*Node, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("mobile: create data dir: %w", err)
	}

	store, err := registry.Open(filepath.Join(dataDir, "walkietalkie.db"))
	if err != nil {
		return nil, fmt.Errorf("mobile: open registry: %w", err)
	}

	selfID, err := config.LoadOrCreateDeviceID(dataDir)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("mobile: device id: %w", err)
	}

	session, err := media.NewPTTSession(selfID, source, sink)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("mobile: create PTT session: %w", err)
	}

	sigPort, err := session.Start(0)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("mobile: start signaling: %w", err)
	}

	n := &Node{
		selfID:     selfID,
		platform:   platform,
		appVersion: appVersion,
		sigPort:    sigPort,
		store:      store,
		session:    session,
		name:       name,
	}

	if err := store.UpsertFromDirectContact(selfID, name, platform, appVersion, []string{"audio"}, "direct", time.Now()); err != nil {
		n.Stop()
		return nil, fmt.Errorf("mobile: register self: %w", err)
	}

	if err := n.reannounce(nil); err != nil {
		n.Stop()
		return nil, fmt.Errorf("mobile: mdns register: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	n.cancelBrowse = cancel
	go func() {
		if err := mdns.Browse(ctx, selfID, n.onPeerFound); err != nil {
			log.Printf("mobile: mdns browse: %v", err)
		}
	}()
	go n.runStaleSweep(ctx)

	return n, nil
}

// oldNodeTimeout: a device not seen in this long is permanently removed
// from this node's own registry — see runStaleSweep. Mobile-only (unlike
// the desktop Base Station, which keeps stale devices around forever for
// the web UI's Old Nodes page), since a phone wants a bounded,
// clutter-free on-device list rather than an indefinitely-retained history.
const oldNodeTimeout = 48 * time.Hour

// runStaleSweep periodically retires devices this node hasn't heard from in
// a while — see registry.Store.SweepStale and the equivalent sweep in
// server/main.go. Without this, a device that vanished without a graceful
// disconnect (out of mDNS range, crashed) stays "connected" in this node's
// own local registry forever, and (for a node that syncs with a Base
// Station) can keep re-spreading that stale status around. Also purges
// (deletes outright) any device not seen in oldNodeTimeout.
func (n *Node) runStaleSweep(ctx context.Context) {
	timeout := time.Duration(config.Default().StaleAfterSeconds) * time.Second
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			if _, err := n.store.SweepStale(n.selfID, now, timeout); err != nil {
				log.Printf("mobile: stale sweep: %v", err)
			}
			if _, err := n.store.PurgeOlderThan(n.selfID, now, oldNodeTimeout); err != nil {
				log.Printf("mobile: purge old nodes: %v", err)
			}
		}
	}
}

func (n *Node) onPeerFound(p mdns.Peer) {
	_ = n.store.UpsertFromDirectContact(p.ID, p.Name, p.Platform, p.AppVersion, []string{"audio"}, "mdns", time.Now())
	if p.GPS != nil {
		_ = n.store.SetLocation(p.ID, *p.GPS)
	}
	if len(p.IPv4) == 0 || p.SignalPort == 0 {
		return
	}
	go func() {
		hosts := make([]string, len(p.IPv4))
		for i, ip := range p.IPv4 {
			hosts[i] = ip.String()
		}
		_ = n.session.ConnectAny(hosts, p.SignalPort, p.ID)
	}()
}

// StartTalking begins transmitting mic audio to every reachable peer
// (call on PTT-button-down).
func (n *Node) StartTalking() {
	n.session.StartTalking()
}

// StopTalking stops transmitting (call on PTT-button-up).
func (n *Node) StopTalking() {
	n.session.StopTalking()
}

// UpdateLocation records a new GPS fix for this device and re-announces it
// over mDNS so peers (and any Base Station dashboard watching this device)
// pick it up on their next sighting — see the mdns package's GPS-in-TXT-
// record design, which avoids needing a separate push API on nodes (like a
// phone) that don't run the full server/api. Call on a timer per the
// configured GPS update interval.
func (n *Node) UpdateLocation(lat, lon, accuracy float64) error {
	point := proto.GeoPoint{Lat: lat, Lon: lon, Accuracy: accuracy, Timestamp: time.Now()}
	if err := n.store.SetLocation(n.selfID, point); err != nil {
		return err
	}
	return n.reannounce(&point)
}

// UpdateName changes this device's display name and re-announces it. Per
// the plan, names are device-originated only.
func (n *Node) UpdateName(name string) error {
	if err := n.store.SetName(n.selfID, name, time.Now()); err != nil {
		return err
	}
	n.mu.Lock()
	n.name = name
	gps := n.lastGPS
	n.mu.Unlock()
	return n.reannounce(gps)
}

func (n *Node) reannounce(gps *proto.GeoPoint) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastGPS = gps

	info := mdns.AnnounceInfo{
		ID:         n.selfID,
		Name:       n.name,
		Platform:   n.platform,
		AppVersion: n.appVersion,
		ProtoVer:   proto.Version,
		Port:       n.sigPort,
		SignalPort: n.sigPort,
		GPS:        gps,
	}

	if n.mdnsSrv != nil {
		n.mdnsSrv.UpdateInfo(info)
		return nil
	}

	srv, err := mdns.Register(info)
	if err != nil {
		return fmt.Errorf("mobile: mdns register: %w", err)
	}
	n.mdnsSrv = srv
	return nil
}

// ReportBLESighting feeds a BLE-discovered peer (presence-only — no GPS,
// no audio path, since BLE advertisement payloads are too small to carry
// either) into the registry. This is the concrete mechanism for "device A
// forwards device B's details to the server" when there's no shared LAN:
// the native BLE scanner calls this for every sighting, regardless of
// whether this node currently has a server connection — the sighting is
// recorded locally immediately either way (see core/discovery/ble.Bridge).
func (n *Node) ReportBLESighting(peerID, peerName, peerPlatform string, rssi int) error {
	return n.store.UpsertFromReport(n.selfID, proto.PeerSummary{
		ID:                 peerID,
		Name:               peerName,
		Platform:           peerPlatform,
		DiscoveryMethod:    "ble",
		LastSeenByReporter: time.Now(),
	})
}

// ListDevicesJSON returns every known device as a JSON array. gomobile
// can't cleanly bind arbitrary Go struct/slice return types, so — the
// standard gomobile pattern — callers decode this JSON on the native side
// rather than receiving bound Device objects directly.
func (n *Node) ListDevicesJSON() (string, error) {
	devices, err := n.store.List()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(devices)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// SelfID returns this device's stable, persisted UUID.
func (n *Node) SelfID() string {
	return n.selfID
}

// Stop tears down this node: mDNS advertisement/browsing, the WebRTC mesh
// and its signaling listener, and the registry database. Call when the app
// is shutting down (or the foreground service is stopped).
func (n *Node) Stop() error {
	if n.cancelBrowse != nil {
		n.cancelBrowse()
	}

	n.mu.Lock()
	if n.mdnsSrv != nil {
		n.mdnsSrv.Shutdown()
		n.mdnsSrv = nil
	}
	n.mu.Unlock()

	if n.session != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		n.session.Shutdown(ctx)
	}

	if n.store != nil {
		return n.store.Close()
	}
	return nil
}
