// Package sniff defines the MeshSniff identify payload answered by every
// WalkieTalkie participant (signaling GET /sniff, Base Station GET /api/sniff).
package sniff

import (
	"net"
	"strings"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/proto"
	"github.com/wlynxg/anet"
)

// Service describes one reachable HTTP/TCP service on this node.
type Service struct {
	Name string `json:"name"`
	Port int    `json:"port"`
	URL  string `json:"url,omitempty"`
}

// IdentifyPayload is what MeshSniff (and peers) learn about this node.
type IdentifyPayload struct {
	MeshID      string            `json:"meshId"`
	Name        string            `json:"name"`
	Platform    string            `json:"platform"`
	AppVersion  string            `json:"appVersion"`
	MACs        []string          `json:"macs,omitempty"`
	GPS         *proto.GeoPoint   `json:"gps,omitempty"`
	URLs        map[string]string `json:"urls,omitempty"`
	Services    []Service         `json:"services,omitempty"`
	NetworkType string            `json:"networkType,omitempty"`
	NetworkName string            `json:"networkName,omitempty"`
}

// LocalMACs returns best-effort non-loopback hardware addresses (may be empty
// on iOS / randomized Android Wi‑Fi).
func LocalMACs() []string {
	ifaces, err := anet.Interfaces()
	if err != nil {
		// Fallback to stdlib on desktop if anet fails.
		std, err2 := net.Interfaces()
		if err2 != nil {
			return nil
		}
		return macsFromStd(std)
	}
	var out []string
	seen := map[string]bool{}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagLoopback != 0 || ifi.Flags&net.FlagUp == 0 {
			continue
		}
		ha := strings.ToLower(ifi.HardwareAddr.String())
		if ha == "" || ha == "00:00:00:00:00:00" || seen[ha] {
			continue
		}
		seen[ha] = true
		out = append(out, ha)
	}
	return out
}

func macsFromStd(ifaces []net.Interface) []string {
	var out []string
	seen := map[string]bool{}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagLoopback != 0 || ifi.Flags&net.FlagUp == 0 {
			continue
		}
		ha := strings.ToLower(ifi.HardwareAddr.String())
		if ha == "" || ha == "00:00:00:00:00:00" || seen[ha] {
			continue
		}
		seen[ha] = true
		out = append(out, ha)
	}
	return out
}

// PrimaryMAC returns the first local MAC or "".
func PrimaryMAC() string {
	m := LocalMACs()
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

// Stamp sets Timestamp on a copy of gps if non-nil and zero.
func Stamp(gps *proto.GeoPoint) *proto.GeoPoint {
	if gps == nil {
		return nil
	}
	cp := *gps
	if cp.Timestamp.IsZero() {
		cp.Timestamp = time.Now()
	}
	return &cp
}
