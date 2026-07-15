# Cellular uplink on MeshSniff (2026-07-15)

Phones can report their active uplink so MeshSniff shows a carrier cloud when they are on mobile data (not only Wi‑Fi AP placement).

## Wire format

`AnnouncePayload` / `registry.Device` / mDNS TXT:

| Field | Values | Source |
|-------|--------|--------|
| `networkType` | `wifi` \| `cellular` | Native path monitor |
| `networkName` | SSID or carrier | Wi‑Fi / TelephonyManager |

mDNS TXT: `net=` / `netname=` (URL-escaped).

## Flow

1. Android `NetworkLinkReporter` (iOS `NWPathMonitor`) calls `Node.SetNetworkLink`
2. Mobile re-announces mDNS and `POST /api/devices/announce` to the Base
3. Base stores `networkType` / `networkName`
4. MeshSniff seed creates `network:cellular:<slug>` (`KindNetwork`) and a dashed `cellular` edge to the phone

Wi‑Fi phones still use AP / `via-router` when a LAN IP is known; cellular cloud is drawn when type is `cellular`.

## Versions

Server **1.10.0**, MeshSniff **0.2.0**, Android **1.8.0**, iOS **0.10.0**.
