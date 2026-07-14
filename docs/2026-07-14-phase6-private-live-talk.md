# Phase 6 — Private-channel live Talk

Started 2026-07-14. First slices of [`TODO-p2p-voice-and-private-relay.md`](TODO-p2p-voice-and-private-relay.md).

## Status

**In progress** — live unicast on Android/iOS + Base Station web private Talk; multi-Base channel + voice-note sync (`server` 1.3.1).

## Behaviour

| Condition | Private Hold-to-Talk |
|-----------|----------------------|
| Peer has **direct** mesh PeerConnection (`IsDirectlyConnected`) | Live unicast Opus to that peer only (not group Broadcast) |
| Peer offline / SFU-only / no direct PC | Existing Base Station clip upload (`SendChannelClip`) |

UI shows **Mode: live mesh** vs **Mode: clip via Base Station** (phones + Base Station private panel).

### Base Station web (`1.3.0`)

- `POST /api/talk/start?to=<peerId>` → `StartTalkingTo`
- `GET /api/talk/peer?id=<peerId>` → `{ "direct": true|false }`
- Private panel mirrors phone live-vs-clip behaviour

### Focus set (server)

Private channels track `focused: []string` so both participants can be focused at once. Legacy `focusedBy` stays as the most recent ID for older readers.

### Multi-Base voice sync (`1.3.1`)

When Base Stations already sync device registries, each tick also:

- `GET /api/sync/channels` — merge private channel records (status rank + focused union)
- `GET /api/sync/voice-notes` — merge metadata; fetch Opus from `GET /api/voice-notes/{id}/audio` when inserting
- Soft-deletes and delivered status replicate; local tombstones win over reappearing blobs

Group Hold-to-Talk is unchanged (`StartTalking` → `Broadcast`).

## Non-goals this slice

- Private use of the mesh SFU / Hub rooms
- Peer-to-peer voice-note transfer without Base Station

## Versions

- Android phone `1.1.1`
- iOS `0.3.1`
- Server `1.3.1`

## Build

```bash
./tools/gomobile-bind-android.sh
./tools/gomobile-bind-ios.sh
cd android && ./android-build.sh :app:assembleDebug
cd server && go run .
```
