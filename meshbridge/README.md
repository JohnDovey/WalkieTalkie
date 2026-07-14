# MeshBridge

Companion process for WalkieTalkie. Runs **next to** a Base Station and syncs remote Base **devices + voice notes / channels** into that Base’s **Remote Users** tab.

**Does not** bridge live Talk (group PTT or private live Opus). Clients stay on their local LAN.

Version: see [`VERSION`](VERSION) (currently **0.1.1**).

## Run

```sh
# from repo root, with Base Station already on :9091
source tools/go-env.sh
go run ./meshbridge/cmd/meshbridge
```

- Status UI / API: `http://127.0.0.1:9095` (configurable)
- Config: OS user config dir → `WalkieTalkie/meshbridge/settings.json`
  - macOS: `~/Library/Application Support/WalkieTalkie/meshbridge/settings.json`
  - Linux: `~/.config/WalkieTalkie/meshbridge/settings.json`
- Override data dir: `--data-dir /path`

## Build

```sh
./tools/build-macos-meshbridge.sh
./tools/build-windows-meshbridge.sh
./tools/build-linux-meshbridge.sh   # run on Linux
```

Binaries land under `/Volumes/JohnDovey/tmp/` on this project’s Mac setup (see `tools/`).

## Transports (`settings.json`)

Any combination:

| Key | Purpose |
|-----|---------|
| `manual` | Fixed remote Base URL (`http://host:port`) |
| `ethernet` | mDNS on a wired iface already on the other router’s LAN (no SSID) |
| `wifi` | Secondary Wi‑Fi NIC: associate SSID then mDNS on that iface |
| `punch` | QuakeMesh-style UDP punch + optional hub; then HTTP sync |

Example Ethernet (Wi‑Fi on LAN A, cable into router B):

```json
{
  "localBaseURL": "http://127.0.0.1:9091",
  "ethernet": [
    { "name": "Office LAN B", "interface": "en5" }
  ]
}
```

## Design / Manual

- Plan: [`docs/2026-07-14-meshbridge-plan.md`](../docs/2026-07-14-meshbridge-plan.md)
- End-user Manual chapter: `Manual/chapters/009-MeshBridge.ebhtml`
- Base Station UI: `/remote-users`
