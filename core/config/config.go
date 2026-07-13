// Package config defines the app's runtime settings and where they (and the
// registry database) live on disk.
//
// This is the shipped app's own runtime data, on whatever machine it runs
// on — NOT the dev-machine build-tooling convention in
// .cursor/rules/volume-storage.mdc (that's for GOPATH/GOCACHE/TMPDIR on
// this JohnDovey-drive dev machine only). AppDataDir must never hardcode a
// /Volumes/JohnDovey path.
package config

import (
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// Settings are the user-editable server settings (see the web UI's
// Settings page — server config only, not per-device renaming).
type Settings struct {
	Port               int  `json:"port"`
	GPSIntervalSeconds int  `json:"gpsIntervalSeconds"`
	RelayEnabled       bool `json:"relayEnabled"`

	// SyncIntervalSeconds controls how often this Base Station re-pulls
	// another Base Station's full registry once discovered (see
	// docs/2026-07-13-implementation-plan.md, "Multi-Base-Station
	// synchronization"). Not used by mobile nodes, which never run the
	// server's REST API and so are never sync targets.
	SyncIntervalSeconds int `json:"syncIntervalSeconds"`

	// RelayThreshold: once the number of simultaneously connected mesh
	// peers exceeds this count, all new connections route through the
	// server relay instead of attempting direct P2P (see the plan's
	// "Audio layer" — formalizes the mesh-scaling risk).
	RelayThreshold int `json:"relayThreshold"`

	// StaleAfterSeconds: a device marked Connected that hasn't been seen
	// (direct contact, mDNS re-sighting, or peer report) in this long is
	// swept to Disconnected — see registry.Store.SweepStale. Without this,
	// a device that vanishes without a graceful disconnect (killed
	// process, phone walking out of range, stale test data) stays
	// "connected" forever, and multi-Base-Station sync's last-seen-wins
	// rule then keeps re-spreading that stale "connected" status to every
	// other Base Station's registry too. grandcat/zeroconf's Browse
	// re-queries at most every 60s once steady-state, so this must stay
	// safely above that to avoid flapping a present device to
	// disconnected between re-sightings.
	StaleAfterSeconds int `json:"staleAfterSeconds"`
}

// DefaultPort is the default port the server's web UI/API listens on.
const DefaultPort = 9091

// Default returns the out-of-the-box settings.
func Default() Settings {
	return Settings{
		Port:                DefaultPort,
		GPSIntervalSeconds:  30,
		RelayEnabled:        true,
		SyncIntervalSeconds: 30,
		RelayThreshold:      10,
		StaleAfterSeconds:   180,
	}
}

// AppDataDir returns (creating if needed) the OS-appropriate per-user
// app-data directory for this app: e.g. `~/Library/Application
// Support/WalkieTalkie` on macOS, `~/.config/walkietalkie` on Linux,
// `%APPDATA%\WalkieTalkie` on Windows.
func AppDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "WalkieTalkie")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// LoadOrCreateDeviceID persists a UUID for this install in dataDir so the
// device's identity survives restarts (the ID is generated once per
// install, not derived from hardware like a MAC address, for privacy and
// because hardware identifiers aren't available equally on every
// platform). Shared by every app (desktop, and via gomobile, mobile).
func LoadOrCreateDeviceID(dataDir string) (string, error) {
	path := filepath.Join(dataDir, "device-id")
	if raw, err := os.ReadFile(path); err == nil {
		return string(raw), nil
	}
	id := uuid.NewString()
	if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
		return "", err
	}
	return id, nil
}
