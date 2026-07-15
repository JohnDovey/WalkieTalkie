package main

import (
	"fmt"
	"strings"
)

// printStartupBanner writes a VirtBBS-style boxed header matching the Base
// Station server, with MeshSniff endpoints and seed/scan settings.
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
	line(fmt.Sprintf("  MeshSniff  v%s", info.Version))
	line("  LAN / dual-network discovery map")
	line("")
	sep()
	line("  SERVERS")
	sep()
	line(fmt.Sprintf("  Web UI         http://127.0.0.1:%d", info.StatusPort))
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
	line("  SEED")
	sep()
	line(fmt.Sprintf("  Base Station   %s", trimBanner(info.LocalBaseURL, w-18)))
	line(fmt.Sprintf("  MeshBridge     %s", trimBanner(info.MeshBridgeURL, w-18)))
	sep()
	line("  SCAN")
	sep()
	line(fmt.Sprintf("  Interval       %ds", info.ScanIntervalSec))
	icmp := "off"
	if info.ICMPEnabled {
		icmp = "on"
	}
	line(fmt.Sprintf("  ICMP           %s", icmp))
	cidrs := "auto (from interfaces)"
	if len(info.ScanCIDRs) > 0 {
		cidrs = strings.Join(info.ScanCIDRs, ", ")
	}
	line(fmt.Sprintf("  CIDRs          %s", trimBanner(cidrs, w-18)))
	sep()
	line("  STORAGE")
	sep()
	line(fmt.Sprintf("  Data dir       %s", trimBanner(info.DataDir, w-18)))
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
	LocalBaseURL    string
	MeshBridgeURL   string
	ScanIntervalSec int
	ScanCIDRs       []string
	ICMPEnabled     bool
	DataDir         string
	LANIPs          []string
}
