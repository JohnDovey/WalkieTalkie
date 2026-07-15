# VirtBBS probing from MeshSniff (2026-07-15)

MeshSniff enriches LAN hosts that look like VirtBBS (open ports among `8081`, `2323`, `3232`, `9998`, `24554`, `24555`).

## Preferred path

VirtBBS **2.1.5+** exposes unauthenticated:

- `GET /sniff`
- `GET /api/sniff` (alias)

JSON includes board `name`, `platform=virtbbs`, `appVersion`, `sysop`, `services`, and `networks` (Fido name/address/binkpPort/role).

## Fallbacks (older VirtBBS)

| Port | Probe | Metadata |
|------|--------|----------|
| 8081 | `GET /manifest.webmanifest` | Board name; fingerprint via description `VirtBBS Web` |
| 24554 / 24555 | BinkP command frames | `SYS VirtBBS`, `ZYZ`/`M_ADR` Fido addresses |
| 2323 | Telnet banner (strip IAC) | Board name + `Powered by VirtBBS` |
| 3232 / 9998 | Connect-only | Service presence; no public board metadata |

WalkieTalkie-style HTTP identify is **not** sent to telnet/SSH/BinkP/VirtAnd ports (avoids Go `Unsolicited response on idle HTTP channel` log spam).

## Code

- VirtBBS: `internal/web/sniff.go`, routes in `internal/web/server.go`
- MeshSniff: `meshsniff/virtbbs`, called from `meshsniff/engine` after TCP open-port discovery
