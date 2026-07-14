# Phase 6 — Private-channel live Talk

Started 2026-07-14. First slices of [`TODO-p2p-voice-and-private-relay.md`](TODO-p2p-voice-and-private-relay.md).

## Status

**In progress** — live unicast (direct mesh + SFU Hub), Base Station web private Talk, multi-Base channel/voice sync.

## Behaviour

| Condition | Private Hold-to-Talk |
|-----------|----------------------|
| Peer has **direct** mesh PeerConnection | Live unicast Opus to that peer only |
| Peer reachable via **Base Station SFU** (`RelayConnected`) | Live Hub unicast (`SetRoute` / `InjectTo`) — no fan-out to other mesh peers |
| Peer offline / not on Hub | Existing Base Station clip upload (`SendChannelClip`) |

UI shows **Mode: live mesh** / **Mode: live relay** / **Mode: clip via Base Station**.

### Base Station web

- `POST /api/talk/start?to=<peerId>` → `StartTalkingTo`
- `GET /api/talk/peer?id=<peerId>` → `{ "direct", "relay", "live" }`
- Private panel mirrors phone live-vs-clip behaviour

### SFU Hub unicast (`server` 1.4.0)

- `Hub.SetRoute` / `ClearRoute` / `InjectTo`
- Relay HTTP `POST /route` + `DELETE /route?sender=`
- `IsLiveTalkAvailable` = direct OR relay
- Base Station speaker ignores private frames routed to someone else

### Focus set (server)

Private channels track `focused: []string` so both participants can be focused at once. Legacy `focusedBy` stays as the most recent ID for older readers.

### Multi-Base voice sync (`1.3.1`)

When Base Stations already sync device registries, each tick also:

- `GET /api/sync/channels` — merge private channel records (status rank + focused union)
- `GET /api/sync/voice-notes` — merge metadata; fetch Opus from `GET /api/voice-notes/{id}/audio` when inserting

Group Hold-to-Talk is unchanged (`StartTalking` → `Broadcast`).

## Non-goals this slice

- Peer-to-peer voice-note transfer without Base Station
- Named multi-party Hub rooms / second WebRTC PC per channel
- Bridging mixed direct↔relay topologies (stays clip)

## Versions

- Android phone `1.2.0`
- iOS `0.4.0`
- Server `1.4.0`

## Build

```bash
./tools/gomobile-bind-android.sh
./tools/gomobile-bind-ios.sh
cd android && ./android-build.sh :app:assembleDebug
cd server && go run .
```
