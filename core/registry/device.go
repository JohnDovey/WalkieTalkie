// Package registry holds the canonical Device record for every device the
// local node has seen (directly or via a peer report), plus the upsert
// precedence rules described in docs/2026-07-13-implementation-plan.md.
package registry

import (
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/proto"
)

// Status is a device's current connection status.
type Status string

const (
	StatusConnected    Status = "connected"
	StatusDisconnected Status = "disconnected"
)

// Device is the canonical record for one device on the network, whether the
// server heard from it directly or only via another device's PeerReport.
type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Platform   string    `json:"platform"`
	AppVersion string    `json:"appVersion"`
	Status     Status    `json:"status"`
	LastSeen   time.Time `json:"lastSeen"`

	CurrentLocation   *proto.GeoPoint `json:"currentLocation,omitempty"`
	LastKnownLocation *proto.GeoPoint `json:"lastKnownLocation,omitempty"`

	// DiscoveryMethods can combine, e.g. a device seen both via mDNS and BLE.
	DiscoveryMethods []string `json:"discoveryMethods"`

	// ReportedBy holds the IDs of devices that forwarded a PeerReport about
	// this device. Empty means the server heard from this device directly.
	ReportedBy []string `json:"reportedBy,omitempty"`

	// Capabilities e.g. ["audio"] vs ["presence-only"] for BLE-only entries
	// that were never reachable for a WebRTC session.
	Capabilities []string `json:"capabilities"`

	// Best-effort hardware addresses for MeshSniff correlation (may be empty).
	MacAddresses []string `json:"macAddresses,omitempty"`

	ProtocolVersion int `json:"protocolVersion"`

	// directSeen is true once the server has heard from this device itself
	// (not just via a peer report) at least once. It governs the upsert
	// precedence rule: a device's own direct data always outranks anything
	// reported about it secondhand.
	directSeen bool
}

// IsPresenceOnly reports whether this device has ever been anything more
// than a BLE-detected stub (never discovered on the LAN, never connected
// directly).
func (d *Device) IsPresenceOnly() bool {
	for _, c := range d.Capabilities {
		if c == "audio" {
			return false
		}
	}
	return true
}

func hasMethod(methods []string, m string) bool {
	for _, existing := range methods {
		if existing == m {
			return true
		}
	}
	return false
}

func addMethod(methods []string, m string) []string {
	if hasMethod(methods, m) {
		return methods
	}
	return append(methods, m)
}
