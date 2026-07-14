# Phase 6 — Private-channel live Talk

Started 2026-07-14. First slices of [`TODO-p2p-voice-and-private-relay.md`](TODO-p2p-voice-and-private-relay.md).

## Status

**In progress** — live unicast + focus UX polish.

## Behaviour

| Condition | Private Hold-to-Talk |
|-----------|----------------------|
| Peer has **direct** mesh PeerConnection (`IsDirectlyConnected`) | Live unicast Opus to that peer only (not group Broadcast) |
| Peer offline / SFU-only / no direct PC | Existing Base Station clip upload (`SendChannelClip`) |

UI shows **Mode: live mesh** vs **Mode: clip via Base Station**, and notes when the peer is also focused on the channel (`focused` set).

### Focus set (server `1.2.1`)

Private channels track `focused: []string` so both participants can be focused at once. Legacy `focusedBy` stays as the most recent ID for older readers.

Group Hold-to-Talk is unchanged (`StartTalking` → `Broadcast`).

## Non-goals this slice

- Private use of the mesh SFU / Hub rooms
- Peer-to-peer voice-note transfer without Base Station
- Multi-Base Station voice blob replication

## Versions

- Android phone `1.1.1`
- iOS `0.3.1`
- Server `1.2.1`

## Build

```bash
./tools/gomobile-bind-android.sh
./tools/gomobile-bind-ios.sh
cd android && ./android-build.sh :app:assembleDebug
cd ios && xcodegen generate
```
