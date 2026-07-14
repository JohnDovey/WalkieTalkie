package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Settings is MeshSniff persisted configuration.
type Settings struct {
	LocalBaseURL   string   `json:"localBaseURL"`
	MeshBridgeURL  string   `json:"meshBridgeURL"`
	StatusPort     int      `json:"statusPort"`
	ScanIntervalSec int     `json:"scanIntervalSeconds"`
	ScanCIDRs      []string `json:"scanCIDRs"` // empty = auto from ifaces
	Ports          []int    `json:"ports"`
}

// Default returns out-of-the-box settings.
func Default() Settings {
	return Settings{
		LocalBaseURL:    "http://127.0.0.1:9091",
		MeshBridgeURL:   "http://127.0.0.1:9095",
		StatusPort:      9096,
		ScanIntervalSec: 20,
		ScanCIDRs:       nil,
		Ports:           []int{22, 53, 80, 88, 139, 443, 445, 548, 631, 3389, 5000, 5900, 7000, 8080, 8443, 9091, 9095, 9096},
	}
}

// ScanInterval returns the scan period.
func (s Settings) ScanInterval() time.Duration {
	sec := s.ScanIntervalSec
	if sec < 5 {
		sec = 20
	}
	return time.Duration(sec) * time.Second
}

// Load reads settings or writes defaults.
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
		return Settings{}, fmt.Errorf("meshsniff config: %w", err)
	}
	d := Default()
	if s.LocalBaseURL == "" {
		s.LocalBaseURL = d.LocalBaseURL
	}
	if s.MeshBridgeURL == "" {
		s.MeshBridgeURL = d.MeshBridgeURL
	}
	if s.StatusPort <= 0 {
		s.StatusPort = d.StatusPort
	}
	if s.ScanIntervalSec <= 0 {
		s.ScanIntervalSec = d.ScanIntervalSec
	}
	if len(s.Ports) == 0 {
		s.Ports = d.Ports
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

// DataDir returns WalkieTalkie/meshsniff under the user config dir.
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "WalkieTalkie", "meshsniff")
	return dir, os.MkdirAll(dir, 0o755)
}
