# MeshSniff

Network discovery companion for WalkieTalkie. Started 2026-07-14. Version **0.1.0**.

## Role

MeshSniff runs next to a Base Station (and optionally MeshBridge). It:

1. Seeds the topology map from **MeshBridge** `/api/inventory`, then local Base `/api/devices` + `/api/bridge/remote-devices`.
2. Discovers routers, subnets, hosts via ARP cache, TCP probes, optional ICMP (root), and WalkieTalkie mDNS.
3. Probes `GET /sniff` / `GET /api/sniff` to correlate **MAC ↔ meshId** (install UUID) and enrich nicknames, GPS, ports, services.
4. Serves a force-graph UI on **http://127.0.0.1:9096** with clickable node modals and live SSE updates.

Live Talk is never bridged. Base Station GPS map (`/map`) is unchanged.

## Identify protocol

Every WalkieTalkie participant answers MeshSniff:

| Surface | Path |
|---------|------|
| Signaling HTTP | `GET /sniff` |
| Base Station REST | `GET /api/sniff` |
| MeshBridge status | `GET /sniff` |
| mDNS TXT | `mac=` (primary, best-effort) |

Payload: `meshId`, `name`, `platform`, `appVersion`, `macs[]`, `gps`, `urls`, `services[]`.

## Privilege

Unprivileged by default (ARP read, TCP connect, mDNS, HTTP identify). ICMP ping sweeps run only when `uid=0`.

## Layout

```
meshsniff/
  cmd/meshsniff/
  config/
  seed/        MeshBridge + Base clients
  netinfo/ arp/ icmp/ tcpprobe/ identify/
  engine/      scan loop
  graph/       correlation store
  web/         vis-network UI + SSE
  VERSION
```

## Run

```sh
source tools/go-env.sh
go run ./meshsniff/cmd/meshsniff
# http://127.0.0.1:9096
```

Config: `~/Library/Application Support/WalkieTalkie/meshsniff/settings.json` (macOS).
