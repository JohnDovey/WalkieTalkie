// Package proto defines the JSON wire messages exchanged between devices and
// the server: the envelope shape, message type constants, and per-type
// payloads. See docs/2026-07-13-implementation-plan.md ("Core data model &
// wire protocol") for the design this implements.
package proto

import "time"

// Version is the current wire protocol version. Bump it when a payload
// shape changes in a way older peers can't safely ignore.
const Version = 1

// Message types, used as Envelope.Type.
const (
	TypeAnnounce   = "announce"
	TypeGPSUpdate  = "gps_update"
	TypeNameUpdate = "name_update"
	TypePeerReport = "peer_report"
	TypeDisconnect = "disconnect"
)

// Envelope wraps every message exchanged over HTTP/WS between a device and
// the server, or between two devices during signaling.
type Envelope struct {
	Type    string    `json:"type"`
	Version int       `json:"version"`
	Sender  string    `json:"sender"`
	TS      time.Time `json:"ts"`
	Payload any       `json:"payload"`
}

// GeoPoint is a single GPS reading.
type GeoPoint struct {
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Accuracy  float64   `json:"accuracy"`
	Timestamp time.Time `json:"timestamp"`
}

// AnnouncePayload is sent by a device to the server on connect (and mirrored
// into the mDNS TXT record for LAN discovery).
type AnnouncePayload struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Platform     string   `json:"platform"`
	AppVersion   string   `json:"appVersion"` // the announcing app's own Major.Minor.Patch version
	Capabilities []string `json:"capabilities"`
	SignalPort   int      `json:"signalPort"`
}

// GPSUpdatePayload is sent by a device on a configurable interval.
type GPSUpdatePayload struct {
	GeoPoint
}

// NameUpdatePayload is sent by a device immediately on next connect if its
// user-set name changed while offline.
type NameUpdatePayload struct {
	Name string `json:"name"`
}

// PeerSummary describes what a reporting device knows about a peer it
// discovered directly (mDNS or BLE) but that isn't itself connected to the
// server.
type PeerSummary struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Platform           string    `json:"platform"`
	DiscoveryMethod    string    `json:"discoveryMethod"` // "mdns" or "ble"
	LastSeenByReporter time.Time `json:"lastSeenByReporter"`
	GPS                *GeoPoint `json:"gps"` // nil for BLE-only (presence-only) sightings
}

// PeerReportPayload is the concrete mechanism for "device A forwards device
// B's details to the server" when B isn't itself connected to the server.
type PeerReportPayload struct {
	Reporter string      `json:"reporter"`
	Peer     PeerSummary `json:"peer"`
}

// DisconnectPayload accompanies a graceful shutdown notice; abrupt
// disconnects are instead detected via WS close / heartbeat timeout.
type DisconnectPayload struct {
	Reason string `json:"reason,omitempty"`
}
