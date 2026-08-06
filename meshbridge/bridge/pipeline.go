package bridge

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
	"github.com/JohnDovey/WalkieTalkie/meshbridge/config"
	"github.com/JohnDovey/WalkieTalkie/meshbridge/discovery"
	"github.com/JohnDovey/WalkieTalkie/meshbridge/wifi"
)

// Pipeline runs configured transports on an interval.
type Pipeline struct {
	Settings config.Settings
	Local    *LocalClient
	NodeID   string

	mu        sync.Mutex
	targets   map[string]remoteTarget // key = remote base URL or id
	status    []TransportStatus
	inventory Inventory
}

type remoteTarget struct {
	URL  string
	ID   string
	Name string
	Kind string
}

// TransportStatus is exposed on the status page.
type TransportStatus struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Detail  string `json:"detail"`
	OK      bool   `json:"ok"`
	LastErr string `json:"lastError,omitempty"`
}

// InventoryDevice is a compact device row for MeshSniff seeding.
type InventoryDevice struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Platform    string   `json:"platform"`
	AppVersion  string   `json:"appVersion"`
	MACs        []string `json:"macs,omitempty"`
	Lat         float64  `json:"lat,omitempty"`
	Lon         float64  `json:"lon,omitempty"`
	NetworkType string   `json:"networkType,omitempty"`
	NetworkName string   `json:"networkName,omitempty"`
}

// InventoryRemote groups devices under a remote Base.
type InventoryRemote struct {
	RemoteBaseID   string            `json:"remoteBaseId"`
	RemoteBaseName string            `json:"remoteBaseName"`
	Devices        []InventoryDevice `json:"devices"`
}

// Inventory is MeshBridge's last-synced remote snapshot for MeshSniff.
type Inventory struct {
	NodeID       string            `json:"nodeId"`
	LocalBaseURL string            `json:"localBaseURL"`
	Transports   []TransportStatus `json:"transports"`
	Remotes      []InventoryRemote `json:"remotes"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

// StatusSnapshot returns current transport health.
func (p *Pipeline) StatusSnapshot() []TransportStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]TransportStatus, len(p.status))
	copy(out, p.status)
	return out
}

// InventorySnapshot returns the last-known remote inventory.
func (p *Pipeline) InventorySnapshot() Inventory {
	p.mu.Lock()
	defer p.mu.Unlock()
	inv := p.inventory
	inv.Transports = append([]TransportStatus(nil), p.status...)
	inv.NodeID = p.NodeID
	inv.LocalBaseURL = p.Settings.LocalBaseURL
	return inv
}

// Run loops until ctx is done.
func (p *Pipeline) Run(ctx context.Context) {
	p.targets = map[string]remoteTarget{}
	ticker := time.NewTicker(p.Settings.SyncInterval())
	defer ticker.Stop()
	p.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Pipeline) tick(ctx context.Context) {
	var statuses []TransportStatus
	bridgeCount := 0
	remotes := map[string]*InventoryRemote{}

	record := func(baseID, baseName string, devices []registry.Device) {
		if baseID == "" {
			return
		}
		ir := remotes[baseID]
		if ir == nil {
			ir = &InventoryRemote{RemoteBaseID: baseID, RemoteBaseName: baseName}
			remotes[baseID] = ir
		}
		if baseName != "" {
			ir.RemoteBaseName = baseName
		}
		for _, d := range devices {
			ir.Devices = append(ir.Devices, toInvDevice(d))
		}
	}

	for _, m := range p.Settings.Manual {
		st := TransportStatus{Kind: "manual", Name: m.Name, Detail: m.URL}
		if m.URL == "" {
			st.LastErr = "empty url"
			statuses = append(statuses, st)
			continue
		}
		if m.Name == "" {
			st.Name = m.URL
		}
		devices, baseID, baseName, err := p.Local.SyncRemoteBase(m.URL, "", m.Name)
		if err != nil {
			if isSkipSelf(err) {
				st.OK = true
				st.Detail = m.URL + " (self, skipped)"
			} else {
				st.LastErr = err.Error()
				log.Printf("meshbridge manual %s: %v", m.URL, err)
			}
		} else {
			st.OK = true
			bridgeCount++
			record(baseID, baseName, devices)
		}
		statuses = append(statuses, st)
	}

	for _, w := range p.Settings.WiFi {
		st := TransportStatus{Kind: "wifi", Name: w.Name, Detail: w.SSID + "@" + w.Interface}
		if w.Interface == "" || w.SSID == "" {
			st.LastErr = "interface and ssid required"
			statuses = append(statuses, st)
			continue
		}
		if err := wifi.Associate(w.Interface, w.SSID, w.Password); err != nil {
			st.LastErr = err.Error()
			log.Printf("meshbridge wifi associate: %v", err)
			statuses = append(statuses, st)
			continue
		}
		found, err := p.discoverAndSync(ctx, w.Interface, record)
		if err != nil {
			st.LastErr = err.Error()
			log.Printf("meshbridge wifi sync: %v", err)
		} else if found > 0 {
			st.OK = true
			bridgeCount += found
		} else if st.LastErr == "" {
			st.LastErr = "no Base Station with api= on iface"
		}
		statuses = append(statuses, st)
	}

	for _, e := range p.Settings.Ethernet {
		iface := e.Interface
		// Empty or "auto" = browse mDNS on the primary LAN (same-LAN multi-Base).
		if iface == "" || iface == "auto" {
			iface = ""
			st := TransportStatus{Kind: "ethernet", Name: e.Name, Detail: "auto (LAN mDNS)"}
			if e.Name == "" {
				st.Name = "LAN auto-discover"
			}
			found, err := p.discoverAndSync(ctx, iface, record)
			if err != nil {
				st.LastErr = err.Error()
				log.Printf("meshbridge ethernet auto: %v", err)
			} else if found > 0 {
				st.OK = true
				bridgeCount += found
			} else {
				st.LastErr = "no other Base Station with api= found on LAN via mDNS"
			}
			statuses = append(statuses, st)
			continue
		}
		st := TransportStatus{Kind: "ethernet", Name: e.Name, Detail: iface}
		if e.Name == "" {
			st.Name = iface
		}
		found, err := p.discoverAndSync(ctx, iface, record)
		if err != nil {
			st.LastErr = err.Error()
			log.Printf("meshbridge ethernet %s: %v", iface, err)
		} else if found > 0 {
			st.OK = true
			bridgeCount += found
		} else {
			st.LastErr = "no Base Station with api= on iface (is the cable up / DHCP leased?)"
		}
		statuses = append(statuses, st)
	}

	for _, pb := range p.Settings.Punch {
		st := TransportStatus{Kind: "punch", Name: pb.Name, Detail: pb.HubHost}
		if pb.Name == "" {
			st.Name = pb.PeerID
		}
		st.Detail = fmt.Sprintf("hub=%s:%d peer=%s", pb.HubHost, pb.HubPort, pb.PeerID)
		st.OK = pb.HubHost != "" && pb.PeerID != ""
		if !st.OK {
			st.LastErr = "hubHost and peerId required"
		} else {
			bridgeCount++
		}
		statuses = append(statuses, st)
	}

	var remList []InventoryRemote
	for _, r := range remotes {
		remList = append(remList, *r)
	}

	p.mu.Lock()
	p.status = statuses
	p.inventory = Inventory{
		NodeID:       p.NodeID,
		LocalBaseURL: p.Settings.LocalBaseURL,
		Transports:   statuses,
		Remotes:      remList,
		UpdatedAt:    time.Now(),
	}
	p.mu.Unlock()

	errMsg := ""
	for _, s := range statuses {
		if s.LastErr != "" {
			errMsg = s.LastErr
			break
		}
	}
	_ = p.Local.Heartbeat(bridgeCount, errMsg)
}

func toInvDevice(d registry.Device) InventoryDevice {
	out := InventoryDevice{
		ID:          d.ID,
		Name:        d.Name,
		Platform:    d.Platform,
		AppVersion:  d.AppVersion,
		MACs:        d.MacAddresses,
		NetworkType: d.NetworkType,
		NetworkName: d.NetworkName,
	}
	if d.CurrentLocation != nil {
		out.Lat = d.CurrentLocation.Lat
		out.Lon = d.CurrentLocation.Lon
	} else if d.LastKnownLocation != nil {
		out.Lat = d.LastKnownLocation.Lat
		out.Lon = d.LastKnownLocation.Lon
	}
	return out
}

type recordFn func(baseID, baseName string, devices []registry.Device)

// discoverAndSync browses mDNS on iface (or all interfaces when iface is empty)
// and syncs every other Base (api≠0) found. Self is skipped.
// When iface is empty ("auto"), also HTTP-scans local /24 subnets for Bases
// on port 9091 — mDNS alone is unreliable on some Windows Wi‑Fi stacks.
func (p *Pipeline) discoverAndSync(ctx context.Context, iface string, record recordFn) (int, error) {
	found := 0
	var lastErr error
	seen := map[string]bool{}

	syncPeer := func(bp discovery.BasePeer) {
		url := fmt.Sprintf("http://%s:%d", bp.Host, bp.APIPort)
		key := bp.ID
		if key == "" {
			key = url
		}
		if seen[key] {
			return
		}
		seen[key] = true
		devices, baseID, baseName, err := p.Local.SyncRemoteBase(url, bp.ID, bp.Name)
		if err != nil {
			if isSkipSelf(err) {
				log.Printf("meshbridge skip self %s", url)
				return
			}
			log.Printf("meshbridge sync %s: %v", url, err)
			lastErr = err
			return
		}
		if record != nil {
			record(baseID, baseName, devices)
		}
		found++
	}

	bctx, cancel := discovery.WithTimeout(ctx, 10*time.Second)
	_ = discovery.BrowseBases(bctx, iface, syncPeer)
	cancel()

	// Auto / empty iface: HTTP /24 scan finds Bases even when mDNS is quiet.
	if iface == "" {
		sctx, scancel := discovery.WithTimeout(ctx, 25*time.Second)
		if err := discovery.ScanLANBases(sctx, []int{9091}, syncPeer); err != nil && sctx.Err() == nil {
			log.Printf("meshbridge lan scan: %v", err)
			if lastErr == nil {
				lastErr = err
			}
		}
		scancel()
	}

	if found == 0 {
		return 0, lastErr
	}
	return found, nil
}

func isSkipSelf(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "skip self")
}
