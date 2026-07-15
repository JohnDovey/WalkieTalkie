# MeshSniff

LAN / dual-network discovery map for WalkieTalkie. Version **0.2.0**.

## Run

```sh
source tools/go-env.sh
go run ./meshsniff/cmd/meshsniff
# UI: http://127.0.0.1:9096  (also on LAN IPs; binds 0.0.0.0 by default)
```

Set `"bindHost": "127.0.0.1"` in `settings.json` to keep the UI local-only.

## Topology

- **Computers** — ARP / TCP / ICMP hosts (desktops, laptops), including **this machine** with hostname.
- **Same-machine services** — WalkieTalkie Base, MeshBridge, MeshSniff, VirtBBS, QuakeMesh Hub/Monitor, and other open ports coalesce onto one host node by IP; click the node for the full service list (map labels stay short so they do not cover the graph).
- **Router links** — every LAN host gets a `via-router` edge to the default gateway so you can see what sits behind the router.
- **Phones on Wi‑Fi** — seeded mesh phones get a `via-router` edge once MeshSniff learns a LAN IP (ARP MAC match, mDNS, Base `lastLanIp`, or full-subnet ICMP when run as root). Remote Users with a known LAN IP also link under the AP (dashed edge to Base remains).
- **Cellular uplink** — when a phone reports `networkType=cellular` (and optional carrier name), MeshSniff draws an ellipse cloud for that carrier and a dashed `cellular` edge to the phone.
- **Wi‑Fi AP details** — when this machine is on Wi‑Fi, the gateway/AP node shows SSID, channel, and security (BSSID is often redacted by macOS).
- **TCP probes** — MeshSniff does **not** sweep ports 1–65535. It connect-probes a fixed well-known list (SSH, HTTP(S), WalkieTalkie, VirtBBS, QuakeMesh `8082`/`18085`/`8083`, etc.). Extra ports can be added under `ports` in `settings.json`. WalkieTalkie identify (`GET /sniff`) runs only on HTTP-ish ports — not telnet/SSH/BinkP/VNC — so those banners do not spam the log.
- **VirtBBS** — when VirtBBS ports are open, MeshSniff probes `GET /sniff` (or `/manifest.webmanifest`), BinkP `SYS`/`ZYZ`/`ADR`, and telnet banners to label the host with board name, version, sysop, and Fido addresses.
- **QuakeMesh** — Hub heartbeat (`18085`) and Monitor (`8082`) advertise unauthenticated `GET /sniff`; MeshSniff labels them as QuakeMeshHub / QuakeMeshMonitor with mesh NodeID and services.
- **Full port scan** — in a node’s detail modal, **Full port scan** runs a background TCP sweep of ports 1–65535. Open ports stream into the graph (and identify probes) as they are found; cancel anytime. Results are saved under `known-ports.json` and re-checked on later scans / after restart.
- **Phones** — Android / iOS / Wear WalkieTalkie devices render as a phone (or watch) icon, distinct from blue computer squares and orange router diamonds.

1. **WalkieTalkie Base Station** (`localBaseURL`, default `http://127.0.0.1:9091`) — `/api/about`, `/api/sniff`, `/api/devices`, `/api/bridge/remote-devices`. Devices appear immediately and link under the Base hub.
2. **Other Bases on LAN** — mDNS `_walkietalkie._tcp` with `api≠0`, same seed pull per Base.
3. **MeshBridge** (`meshBridgeURL`, default `http://127.0.0.1:9095`) — dual-LAN inventory enrichment.
4. Active scan — ARP / TCP connect probes / mDNS peers / optional ICMP, correlating MAC ↔ meshId via `/sniff`.

## Build

```sh
./tools/build-macos-meshsniff.sh
./tools/build-windows-meshsniff.sh
./tools/build-linux-meshsniff.sh   # on Linux
```

See [`docs/2026-07-14-meshsniff-plan.md`](../docs/2026-07-14-meshsniff-plan.md) and Manual → MeshSniff.
