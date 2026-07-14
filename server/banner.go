package main

import (
	"fmt"
	"strings"
)

// printStartupBanner writes a VirtBBS-style boxed header to stdout with the
// program name, version, listening endpoints, and storage paths.
func printStartupBanner(info startupInfo) {
	const w = 60 // inner width (between the ║ borders)
	border := strings.Repeat("═", w)
	pad := func(s string) string {
		n := len([]rune(s))
		if n >= w {
			return string([]rune(s)[:w])
		}
		return s + strings.Repeat(" ", w-n)
	}
	line := func(s string) { fmt.Printf("║ %s ║\n", pad(s)) }
	sep := func() { fmt.Printf("╠═%s═╣\n", border) }

	fmt.Printf("╔═%s═╗\n", border)
	line("")
	line(fmt.Sprintf("  WalkieTalkie Base Station  v%s", info.Version))
	line("  Cross-platform LAN push-to-talk")
	if info.Name != "" {
		line(fmt.Sprintf("  %s", info.Name))
	}
	line("")
	sep()
	line("  SERVERS")
	sep()
	line(fmt.Sprintf("  Web UI / API   http://0.0.0.0:%d", info.WebPort))
	line(fmt.Sprintf("  Signaling      :%d", info.SignalPort))
	line(fmt.Sprintf("  SFU relay      :%d", info.RelayPort))
	sep()
	line("  NODE")
	sep()
	line(fmt.Sprintf("  Device ID      %s", info.DeviceID))
	line(fmt.Sprintf("  Platform       %s", info.Platform))
	audio := "enabled"
	if info.AudioDisabled {
		audio = "disabled"
	}
	line(fmt.Sprintf("  Audio          %s", audio))
	sep()
	line("  SETTINGS")
	sep()
	relay := "off"
	if info.RelayEnabled {
		relay = fmt.Sprintf("on (threshold %d)", info.RelayThreshold)
	}
	line(fmt.Sprintf("  Mesh relay     %s", relay))
	line(fmt.Sprintf("  Sync interval  %ds", info.SyncIntervalSeconds))
	sep()
	line("  STORAGE")
	sep()
	line(fmt.Sprintf("  Data dir       %s", info.DataDir))
	line("")
	fmt.Printf("╚═%s═╝\n", border)
	fmt.Println()
}

type startupInfo struct {
	Version             string
	Name                string
	DeviceID            string
	Platform            string
	WebPort             int
	SignalPort          int
	RelayPort           int
	DataDir             string
	AudioDisabled       bool
	RelayEnabled        bool
	RelayThreshold      int
	SyncIntervalSeconds int
}
