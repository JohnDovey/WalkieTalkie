package bridge

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/meshbridge/config"
	"github.com/JohnDovey/WalkieTalkie/meshbridge/discovery"
	"github.com/JohnDovey/WalkieTalkie/meshbridge/wifi"
)

// Pipeline runs configured transports on an interval.
type Pipeline struct {
	Settings config.Settings
	Local    *LocalClient

	mu      sync.Mutex
	targets map[string]remoteTarget // key = remote base URL or id
	status  []TransportStatus
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

// StatusSnapshot returns current transport health.
func (p *Pipeline) StatusSnapshot() []TransportStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]TransportStatus, len(p.status))
	copy(out, p.status)
	return out
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
		err := p.Local.SyncRemoteBase(m.URL, "", m.Name)
		if err != nil {
			st.LastErr = err.Error()
			log.Printf("meshbridge manual %s: %v", m.URL, err)
		} else {
			st.OK = true
			bridgeCount++
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
		found, err := p.discoverAndSync(ctx, w.Interface)
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
		st := TransportStatus{Kind: "ethernet", Name: e.Name, Detail: e.Interface}
		if e.Interface == "" {
			st.LastErr = "interface required (e.g. en5)"
			statuses = append(statuses, st)
			continue
		}
		if e.Name == "" {
			st.Name = e.Interface
		}
		found, err := p.discoverAndSync(ctx, e.Interface)
		if err != nil {
			st.LastErr = err.Error()
			log.Printf("meshbridge ethernet %s: %v", e.Interface, err)
		} else if found > 0 {
			st.OK = true
			bridgeCount += found
		} else {
			st.LastErr = "no Base Station with api= on iface (is the cable up / DHCP leased?)"
		}
		statuses = append(statuses, st)
	}

	// Punch peers: URLs may be published out-of-band via punch package into Local sync.
	for _, pb := range p.Settings.Punch {
		st := TransportStatus{Kind: "punch", Name: pb.Name, Detail: pb.HubHost}
		if pb.Name == "" {
			st.Name = pb.PeerID
		}
		// Actual punch session is owned by punch.Manager; pipeline records configured entries.
		st.Detail = fmt.Sprintf("hub=%s:%d peer=%s", pb.HubHost, pb.HubPort, pb.PeerID)
		st.OK = pb.HubHost != "" && pb.PeerID != ""
		if !st.OK {
			st.LastErr = "hubHost and peerId required"
		} else {
			bridgeCount++
		}
		statuses = append(statuses, st)
	}

	p.mu.Lock()
	p.status = statuses
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

// discoverAndSync browses mDNS on iface and syncs every Base (api≠0) found.
func (p *Pipeline) discoverAndSync(ctx context.Context, iface string) (int, error) {
	bctx, cancel := discovery.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	found := 0
	var lastErr error
	_ = discovery.BrowseBases(bctx, iface, func(bp discovery.BasePeer) {
		url := fmt.Sprintf("http://%s:%d", bp.Host, bp.APIPort)
		if err := p.Local.SyncRemoteBase(url, bp.ID, bp.Name); err != nil {
			log.Printf("meshbridge sync %s: %v", url, err)
			lastErr = err
			return
		}
		found++
	})
	if found == 0 {
		return 0, lastErr
	}
	return found, nil
}
