# MeshSniff phone placement under Wi‑Fi AP (2026-07-15)

Phones seeded from WalkieTalkie Base (`GET /api/devices`) used to appear only under the Base hub: registry records had MACs/GPS but **no LAN IP**, and `via-router` edges require an IP.

## Fixes (MeshSniff 0.1.15 + Base 1.9.1)

1. **Graph merge** — upserting `host:<ip>` (ARP/mDNS) relocates matching `dev:<meshId>` walkies by MAC or MeshID onto the host node (edges relinked).
2. **ARP MAC correlate** — after reading the ARP cache, walkies still missing IPs are attached when a MAC matches.
3. **Base `lastLanIp`** — announce and GPS HTTP handlers store the client’s private IPv4; MeshSniff seeds that as `IPs` so a known phone can sit under the AP even without ARP/mDNS.

Quiet phones (sleep, randomized MAC, no HTTP to Base) may still stay mesh-only until something yields an IP.

## Full-subnet ICMP (root only)

When MeshSniff runs as **root** (`sudo`), each scan pings every host in local CIDRs (up to `/22` size cap) to populate the ARP cache and discover ICMP responders. Without root, ICMP is skipped entirely (no error).

## Remote Users on the LAN

`remoteHint` nodes with a LAN IP (e.g. Base `lastLanIp`) now get solid `via-router` edges to the gateway as well as the dashed remote link to the Base hub.

## MeshBridge LAN UI (0.1.4)

Status HTTP binds `0.0.0.0` by default (`bindHost`); set `"bindHost": "127.0.0.1"` for localhost-only.
