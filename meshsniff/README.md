# MeshSniff

LAN / dual-network discovery map for WalkieTalkie. Version **0.1.3**.

## Run

```sh
source tools/go-env.sh
go run ./meshsniff/cmd/meshsniff
# UI: http://127.0.0.1:9096
```

## Topology

- **Computers** — ARP / TCP / ICMP hosts (desktops, laptops), including **this machine** with hostname.
- **Same-machine services** — WalkieTalkie Base, MeshBridge, MeshSniff (and other open ports) coalesce onto one host node by IP; the label lists services sharing that IP.
- **Router links** — every LAN host gets a `via-router` edge to the default gateway so you can see what sits behind the router.
- **Wi‑Fi AP details** — when this machine is on Wi‑Fi, the gateway/AP node shows SSID, channel, and security (BSSID is often redacted by macOS).

1. **WalkieTalkie Base Station** (`localBaseURL`, default `http://127.0.0.1:9091`) — `/api/about`, `/api/sniff`, `/api/devices`, `/api/bridge/remote-devices`. Devices appear immediately and link under the Base hub.
2. **Other Bases on LAN** — mDNS `_walkietalkie._tcp` with `api≠0`, same seed pull per Base.
3. **MeshBridge** (`meshBridgeURL`, default `http://127.0.0.1:9095`) — dual-LAN inventory enrichment.
4. Active scan — ARP / TCP / mDNS peers / optional ICMP, correlating MAC ↔ meshId via `/sniff`.

## Build

```sh
./tools/build-macos-meshsniff.sh
./tools/build-windows-meshsniff.sh
./tools/build-linux-meshsniff.sh   # on Linux
```

See [`docs/2026-07-14-meshsniff-plan.md`](../docs/2026-07-14-meshsniff-plan.md) and Manual → MeshSniff.
