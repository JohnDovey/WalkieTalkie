// Package ble defines the contract between the shared Go core and native
// per-platform Bluetooth LE scanning/advertising code.
//
// There is deliberately no Go implementation here for mobile: research
// confirmed tinygo-org/bluetooth (the cross-platform Go BLE library) does
// not support Android or iOS. Real BLE scanning/advertising is native code
// (Android BluetoothLeScanner/AdvertiseCallback, iOS CoreBluetooth) that
// calls into this package's Bridge via the gomobile-generated bindings in
// core/mobile. See docs/2026-07-13-implementation-plan.md ("Discovery
// layer") for the design.
//
// BLE sightings are presence-only: advertisement payloads are too small to
// carry GPS, so a Peer reported here never has audio capability until it's
// also reachable via LAN/mDNS.
package ble

import "time"

// Peer is a BLE sighting reported by native platform code.
type Peer struct {
	ID        string
	Name      string
	Platform  string
	RSSI      int
	Timestamp time.Time
}

// Bridge receives BLE sightings from native platform scanning code and
// forwards them into the local registry as presence-only PeerReports.
// Implemented by core/media or a small adapter that wraps
// registry.Store.UpsertFromReport.
type Bridge interface {
	ReportPeerSeen(peer Peer)
}
