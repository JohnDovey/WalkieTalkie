package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TransportType selects how MeshBridge reaches a remote Base.
type TransportType string

const (
	TransportManual   TransportType = "manual"
	TransportWiFi     TransportType = "wifi"
	TransportEthernet TransportType = "ethernet"
	TransportPunch    TransportType = "punch"
)

// ManualBridge reaches a Base at a fixed HTTP URL.
type ManualBridge struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url"`
}

// WiFiBridge associates a secondary NIC and discovers Bases via mDNS.
type WiFiBridge struct {
	Name      string `json:"name,omitempty"`
	SSID      string `json:"ssid"`
	Password  string `json:"password"`
	Interface string `json:"interface"` // e.g. en1
}

// EthernetBridge uses a wired iface already on the other router’s LAN
// (USB-C/Thunderbolt adapter, docking station, etc.). No SSID associate —
// macOS (or the user) must already have that interface up with an IP.
// MeshBridge only mDNS-discovers Bases (`api≠0`) on that interface.
type EthernetBridge struct {
	Name      string `json:"name,omitempty"`
	Interface string `json:"interface"` // e.g. en5 — check Network settings / ifconfig
}

// PunchBridge uses QuakeMesh-style UDP punch + optional hub relay.
type PunchBridge struct {
	Name       string `json:"name,omitempty"`
	PeerID     string `json:"peerId"`               // remote MeshBridge node id
	HubHost    string `json:"hubHost"`              // relay/rendezvous host
	HubPort    int    `json:"hubPort"`              // UDP control port
	RoleHub    bool   `json:"roleHub"`              // also run hub locally
	ListenPort int    `json:"listenPort,omitempty"` // local hub listen (default 29191)
}

// Settings is MeshBridge's persisted configuration.
type Settings struct {
	LocalBaseURL    string           `json:"localBaseURL"`
	SyncIntervalSec int              `json:"syncIntervalSeconds"`
	StatusPort      int              `json:"statusPort"`
	NodeID          string           `json:"nodeId,omitempty"`
	Manual          []ManualBridge   `json:"manual"`
	WiFi            []WiFiBridge     `json:"wifi"`
	Ethernet        []EthernetBridge `json:"ethernet"`
	Punch           []PunchBridge    `json:"punch"`
	HubListenPort   int              `json:"hubListenPort,omitempty"`
	RunHub          bool             `json:"runHub"`
}

// Default returns out-of-the-box settings.
func Default() Settings {
	return Settings{
		LocalBaseURL:    "http://127.0.0.1:9091",
		SyncIntervalSec: 30,
		StatusPort:      9095,
		HubListenPort:   29191,
		Manual:          []ManualBridge{},
		WiFi:            []WiFiBridge{},
		Ethernet:        []EthernetBridge{},
		Punch:           []PunchBridge{},
	}
}

// SyncInterval returns the sync period.
func (s Settings) SyncInterval() time.Duration {
	sec := s.SyncIntervalSec
	if sec < 5 {
		sec = 30
	}
	return time.Duration(sec) * time.Second
}

// Load reads settings from path, or writes defaults if missing.
func Load(path string) (Settings, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return Settings{}, err
		}
		s := Default()
		if err := Save(path, s); err != nil {
			return s, err
		}
		return s, nil
	}
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return Settings{}, fmt.Errorf("meshbridge config: %w", err)
	}
	d := Default()
	if s.LocalBaseURL == "" {
		s.LocalBaseURL = d.LocalBaseURL
	}
	if s.SyncIntervalSec <= 0 {
		s.SyncIntervalSec = d.SyncIntervalSec
	}
	if s.StatusPort <= 0 {
		s.StatusPort = d.StatusPort
	}
	if s.HubListenPort <= 0 {
		s.HubListenPort = d.HubListenPort
	}
	return s, nil
}

// Save writes settings atomically.
func Save(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DataDir returns ~/.config/WalkieTalkie/meshbridge (or OS equivalent).
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "WalkieTalkie", "meshbridge")
	return dir, os.MkdirAll(dir, 0o755)
}
