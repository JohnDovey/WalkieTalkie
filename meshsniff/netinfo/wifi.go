package netinfo

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WifiInfo is the current Wi‑Fi association (best-effort; some fields are
// redacted by the OS for privacy).
type WifiInfo struct {
	Iface       string `json:"iface,omitempty"`
	SSID        string `json:"ssid,omitempty"`
	BSSID       string `json:"bssid,omitempty"`
	Security    string `json:"security,omitempty"`
	Channel     string `json:"channel,omitempty"`
	PhyMode     string `json:"phyMode,omitempty"`
	RateMbps    string `json:"rateMbps,omitempty"`
	SignalNoise string `json:"signalNoise,omitempty"`
	Country     string `json:"country,omitempty"`
}

var (
	wifiEnrichMu   sync.Mutex
	wifiEnrich     WifiInfo
	wifiEnrichAt   time.Time
	wifiEnrichTTL  = 5 * time.Minute
)

// Wifi returns the active Wi‑Fi association when the host is on Wi‑Fi.
func Wifi() (WifiInfo, bool) {
	switch runtime.GOOS {
	case "darwin":
		return wifiDarwin()
	case "linux":
		return wifiLinux()
	default:
		return WifiInfo{}, false
	}
}

func wifiDarwin() (WifiInfo, bool) {
	iface := wifiIfaceDarwin()
	if iface == "" {
		return WifiInfo{}, false
	}
	w := WifiInfo{Iface: iface}

	out, err := exec.Command("networksetup", "-getairportnetwork", iface).CombinedOutput()
	if err == nil {
		line := strings.TrimSpace(string(out))
		if strings.HasPrefix(line, "Current Wi-Fi Network:") {
			ssid := strings.TrimSpace(strings.TrimPrefix(line, "Current Wi-Fi Network:"))
			if ssid != "" && !strings.EqualFold(ssid, "You are not associated with an AirPort network.") {
				w.SSID = ssid
			}
		}
	}
	if w.SSID == "" {
		// Not associated (or SSID unavailable).
		return WifiInfo{}, false
	}

	if sum, err := exec.Command("ipconfig", "getsummary", iface).CombinedOutput(); err == nil {
		parseIPConfigSummary(string(sum), &w)
	}

	enrich := wifiEnrichDarwinCached()
	if enrich.Channel != "" {
		w.Channel = enrich.Channel
	}
	if enrich.PhyMode != "" {
		w.PhyMode = enrich.PhyMode
	}
	if enrich.RateMbps != "" {
		w.RateMbps = enrich.RateMbps
	}
	if enrich.SignalNoise != "" {
		w.SignalNoise = enrich.SignalNoise
	}
	if enrich.Country != "" {
		w.Country = enrich.Country
	}
	if enrich.Security != "" {
		w.Security = enrich.Security
	} else if w.Security != "" {
		// keep ipconfig mapping
	}
	if enrich.BSSID != "" && w.BSSID == "" {
		w.BSSID = enrich.BSSID
	}
	return w, true
}

func wifiIfaceDarwin() string {
	out, err := exec.Command("networksetup", "-listallhardwareports").CombinedOutput()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "Wi-Fi") && !strings.Contains(line, "AirPort") {
			continue
		}
		for j := i + 1; j < len(lines) && j < i+4; j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "Device:") {
				return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[j]), "Device:"))
			}
		}
	}
	return ""
}

var redactedRE = regexp.MustCompile(`(?i)^<redacted>$`)

func parseIPConfigSummary(s string, w *WifiInfo) {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, " : ")
		if !ok {
			key, val, ok = strings.Cut(line, ": ")
		}
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if redactedRE.MatchString(val) {
			continue
		}
		switch key {
		case "SSID":
			if w.SSID == "" {
				w.SSID = val
			}
		case "BSSID":
			w.BSSID = strings.ToLower(val)
		case "Security":
			w.Security = prettySecurity(val)
		}
	}
}

func prettySecurity(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case strings.EqualFold(s, "None"), s == "":
		return s
	case strings.Contains(strings.ToUpper(s), "SHA256_PSK"):
		return "WPA3-Personal"
	case strings.Contains(strings.ToUpper(s), "PSK"):
		return "WPA2-Personal"
	default:
		return s
	}
}

func wifiEnrichDarwinCached() WifiInfo {
	wifiEnrichMu.Lock()
	defer wifiEnrichMu.Unlock()
	if time.Since(wifiEnrichAt) < wifiEnrichTTL && wifiEnrichAt != (time.Time{}) {
		return wifiEnrich
	}
	wifiEnrich = wifiEnrichDarwin()
	wifiEnrichAt = time.Now()
	return wifiEnrich
}

func wifiEnrichDarwin() WifiInfo {
	out, err := exec.Command("system_profiler", "SPAirPortDataType", "-json").CombinedOutput()
	if err != nil || len(out) == 0 {
		return WifiInfo{}
	}
	var root map[string]any
	if err := json.Unmarshal(out, &root); err != nil {
		return WifiInfo{}
	}
	arr, _ := root["SPAirPortDataType"].([]any)
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ifaces, _ := m["spairport_airport_interfaces"].([]any)
		for _, raw := range ifaces {
			im, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			cur, ok := im["spairport_current_network_information"].(map[string]any)
			if !ok || len(cur) == 0 {
				continue
			}
			w := WifiInfo{}
			if name, _ := cur["_name"].(string); name != "" && !redactedRE.MatchString(name) {
				w.SSID = name
			}
			if v, _ := cur["spairport_network_channel"].(string); v != "" {
				w.Channel = v
			}
			if v, _ := cur["spairport_network_phymode"].(string); v != "" {
				w.PhyMode = v
			}
			if v, _ := cur["spairport_security_mode"].(string); v != "" {
				w.Security = prettyAirportSecurity(v)
			}
			if v, _ := cur["spairport_signal_noise"].(string); v != "" {
				w.SignalNoise = v
			}
			if v, _ := cur["spairport_network_country_code"].(string); v != "" {
				w.Country = v
			}
			switch r := cur["spairport_network_rate"].(type) {
			case float64:
				w.RateMbps = strconv.FormatFloat(r, 'f', -1, 64)
			case string:
				w.RateMbps = r
			}
			if w.Channel != "" || w.Security != "" || w.SSID != "" {
				return w
			}
		}
	}
	return WifiInfo{}
}

func prettyAirportSecurity(s string) string {
	s = strings.TrimPrefix(s, "spairport_security_mode_")
	s = strings.ReplaceAll(s, "_", " ")
	return strings.TrimSpace(s)
}

func wifiLinux() (WifiInfo, bool) {
	// Prefer nmcli when available.
	if out, err := exec.Command("nmcli", "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION", "device").CombinedOutput(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			parts := strings.Split(line, ":")
			if len(parts) < 4 {
				continue
			}
			if parts[1] != "wifi" || parts[2] != "connected" {
				continue
			}
			w := WifiInfo{Iface: parts[0], SSID: parts[3]}
			if w.SSID == "" {
				continue
			}
			if sec, err := exec.Command("nmcli", "-t", "-f", "ACTIVE,SSID,SECURITY,BSSID,CHAN", "dev", "wifi").CombinedOutput(); err == nil {
				for _, row := range strings.Split(string(sec), "\n") {
					f := strings.Split(row, ":")
					if len(f) < 5 || f[0] != "yes" {
						continue
					}
					if f[1] != "" {
						w.SSID = f[1]
					}
					w.Security = f[2]
					w.BSSID = strings.ToLower(f[3])
					w.Channel = f[4]
					break
				}
			}
			return w, true
		}
	}
	if out, err := exec.Command("iwgetid", "-r").CombinedOutput(); err == nil {
		ssid := strings.TrimSpace(string(out))
		if ssid != "" {
			return WifiInfo{SSID: ssid}, true
		}
	}
	return WifiInfo{}, false
}
