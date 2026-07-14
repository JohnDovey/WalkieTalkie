# MeshBridge

Design for WalkieTalkie’s companion bridge process: sync Base Station **devices + voice notes** across LANs / WAN without echoing live Talk. Started 2026-07-14.

## Role

MeshBridge runs **alongside** the Base Station (separate binary for now; library-shaped for a later embed). It does **not** bridge SFU/live Opus. Clients stay local to their LAN; remote Bases appear under the Base Station **Remote Users** tab.

## Transports (any combination)

| Type | Use when |
|------|----------|
| `manual` | Known `http://host:port` to another Base |
| `wifi` | Secondary Wi‑Fi NIC (USB); SSID+password; per-iface mDNS for Bases (`api≠0`) |
| `ethernet` | Mac already on a second LAN via Ethernet (cable into the other router); mDNS on that iface — **no SSID** |
| `punch` | QuakeMesh-style UDP hole punch; hub relay on CGNAT |

### Ethernet bridge (recommended for one Mac ↔ two routers)

1. Keep Wi‑Fi joined to LAN A (Base Station A / phones on A).
2. Plug an Ethernet cable from this Mac into a LAN port on router B (or a switch on LAN B). Use a USB‑C/Thunderbolt adapter if needed.
3. Confirm the Ethernet iface has an IP on B’s subnet (`ifconfig` / System Settings → Network). Note the device name (`en5`, `en7`, …).
4. In MeshBridge `settings.json`:

```json
"ethernet": [
  { "name": "Office LAN B", "interface": "en5" }
]
```

MeshBridge will discover Base Station(s) advertising `api=` on that iface and sync devices + voice notes into **Remote Users**. Live Talk still does not cross.

Punch design: [`QuakeMesh/plan.md`](/Volumes/JohnDovey/Projects/QuakeMesh/plan.md) § NAT/CGNAT Traversal (DCUtR `0.5×RTT`, hub TURN-style fallback).

## Base Station ingest

- `POST /api/bridge/remote-devices` — merge remote device list tagged with origin Base
- `POST /api/bridge/voice-sync` — channels + notes (+ optional audio blobs)
- `GET /api/bridge/status` / `GET /api/bridge/remote-devices`

Bridged remotes **must not** trigger mesh `ConnectAny`.

## Package layout

```
meshbridge/
  cmd/meshbridge/   main
  config/           JSON settings (manual / wifi / ethernet / punch)
  bridge/           sync pipeline → local Base ingest
  wifi/             macOS associate helpers
  discovery/        per-iface mDNS
  punch/            QuakeMesh-inspired hub + client
  VERSION           0.1.0
```

## Policy

- Live Talk stays per-LAN.
- Voicenotes and registry visibility sync across the bridge.
- Users remain attributed to their origin Base (Remote Users grouping).
