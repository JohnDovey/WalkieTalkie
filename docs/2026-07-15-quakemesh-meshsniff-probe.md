# QuakeMesh probing from MeshSniff (2026-07-15)

MeshSniff enriches LAN hosts that expose QuakeMesh Hub or Monitor HTTP surfaces.

## Preferred path

QuakeMesh **1.0.2+** exposes unauthenticated:

| Target | Ports | Paths |
|--------|-------|-------|
| QuakeMeshMonitor | `8082` (default) | `GET /sniff`, `GET /api/sniff` |
| QuakeMeshHub (LAN) | `18085` heartbeat | `GET /sniff`, `GET /api/sniff` |
| QuakeMeshHub (local) | `8083` management (loopback) | `GET /sniff`, `GET /api/sniff` |

JSON follows the MeshSniff identify contract:

- Hub: `name=QuakeMeshHub`, `platform=quakemesh-hub`, `meshId` = hub NodeID (hex), `appVersion`, hub services (heartbeat / management / OGM / discovery)
- Monitor: `name=QuakeMeshMonitor`, `platform=quakemesh-monitor`, `meshId` = local hub NodeID when known, dashboard URL

## MeshSniff changes

- Default discover ports include `8082`, `8083`, and `18085`
- `applyIdentify` treats QuakeMesh platforms as computer nodes and labels Hub / Monitor services
- Modal links open the Monitor dashboard on `:8082`

## Code

- QuakeMesh Hub: `hub/internal/nodeheartbeat/sniff.go`, `hub/internal/managementapi/sniff.go`
- QuakeMesh Monitor: `monitor/internal/server/sniff.go`
- MeshSniff: `meshsniff/config`, `meshsniff/engine`, `meshsniff/web/static/app.js`
