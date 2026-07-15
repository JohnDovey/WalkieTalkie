package main

import (
	"fmt"
	"strings"

	mbconfig "github.com/JohnDovey/WalkieTalkie/meshbridge/config"
)

// printStartupBanner writes a VirtBBS-style boxed header matching Base Station
// / MeshSniff, with MeshBridge endpoints and transport settings.
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
	line(fmt.Sprintf("  MeshBridge  v%s", info.Version))
	line("  Dual-LAN Base Station bridge")
	line("")
	sep()
	line("  SERVERS")
	sep()
	line(fmt.Sprintf("  Status UI      http://127.0.0.1:%d", info.StatusPort))
	bind := strings.TrimSpace(info.BindHost)
	if bind == "" {
		bind = "0.0.0.0"
	}
	if bind != "127.0.0.1" && bind != "localhost" && bind != "::1" {
		line(fmt.Sprintf("  Bind           %s:%d", bind, info.StatusPort))
		for _, ip := range info.LANIPs {
			if ip == "" || ip == "127.0.0.1" {
				continue
			}
			line(fmt.Sprintf("  LAN            http://%s:%d", ip, info.StatusPort))
		}
	}
	sep()
	line("  NODE")
	sep()
	line(fmt.Sprintf("  Node ID        %s", trimBanner(info.NodeID, w-18)))
	line(fmt.Sprintf("  Local Base     %s", trimBanner(info.LocalBaseURL, w-18)))
	line(fmt.Sprintf("  Sync interval  %ds", info.SyncIntervalSec))
	sep()
	line("  TRANSPORTS")
	sep()
	manualN := len(info.Manual)
	wifiN := len(info.WiFi)
	ethN := len(info.Ethernet)
	punchN := len(info.Punch)
	line(fmt.Sprintf("  Manual         %d", manualN))
	line(fmt.Sprintf("  Wi-Fi          %d", wifiN))
	line(fmt.Sprintf("  Ethernet       %d", ethN))
	line(fmt.Sprintf("  Punch          %d", punchN))
	for _, m := range info.Manual {
		name := m.Name
		if name == "" {
			name = m.URL
		}
		line(fmt.Sprintf("    · manual     %s", trimBanner(name, w-20)))
	}
	for _, wfi := range info.WiFi {
		name := wfi.Name
		if name == "" {
			name = wfi.SSID
		}
		line(fmt.Sprintf("    · wifi       %s", trimBanner(name, w-20)))
	}
	for _, e := range info.Ethernet {
		name := e.Name
		if name == "" {
			name = e.Interface
		}
		line(fmt.Sprintf("    · ethernet   %s", trimBanner(name, w-20)))
	}
	for _, p := range info.Punch {
		name := p.Name
		if name == "" {
			name = p.PeerID
		}
		line(fmt.Sprintf("    · punch      %s", trimBanner(name, w-20)))
	}
	hub := "off"
	if info.RunHub {
		hub = fmt.Sprintf("on (:%d)", info.HubListenPort)
	}
	line(fmt.Sprintf("  Punch hub      %s", hub))
	sep()
	line("  STORAGE")
	sep()
	line(fmt.Sprintf("  Data dir       %s", trimBanner(info.DataDir, w-18)))
	line(fmt.Sprintf("  Config         %s", trimBanner(info.ConfigPath, w-18)))
	line("")
	fmt.Printf("╚═%s═╝\n", border)
	fmt.Println()
}

func trimBanner(s string, max int) string {
	if max < 4 {
		max = 4
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

type startupInfo struct {
	Version         string
	BindHost        string
	StatusPort      int
	NodeID          string
	LocalBaseURL    string
	SyncIntervalSec int
	Manual          []mbconfig.ManualBridge
	WiFi            []mbconfig.WiFiBridge
	Ethernet        []mbconfig.EthernetBridge
	Punch           []mbconfig.PunchBridge
	RunHub          bool
	HubListenPort   int
	DataDir         string
	ConfigPath      string
	LANIPs          []string
}
