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
)

// Settings are the user-editable server settings (see the web UI's
// Settings page — server config only, not per-device renaming).
type Settings struct {
	Port               int  `json:"port"`
	GPSIntervalSeconds int  `json:"gpsIntervalSeconds"`
	RelayEnabled       bool `json:"relayEnabled"`
}

// DefaultPort is the default port the server's web UI/API listens on.
const DefaultPort = 9091

// Default returns the out-of-the-box settings.
func Default() Settings {
	return Settings{
		Port:               DefaultPort,
		GPSIntervalSeconds: 30,
		RelayEnabled:       true,
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
