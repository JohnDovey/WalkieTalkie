# Phase 6 — Private-channel live Talk

Started 2026-07-14. First slice of [`TODO-p2p-voice-and-private-relay.md`](TODO-p2p-voice-and-private-relay.md): **unicast WebRTC** while a private channel is open and the peer has a direct mesh PeerConnection. Clips remain the fallback.

## Status

**In progress** — core `SendTo` / `StartTalkingTo` + Android/iOS private Hold-to-Talk branch.

## Behaviour

| Condition | Private Hold-to-Talk |
|-----------|----------------------|
| Peer has **direct** mesh PeerConnection (`IsDirectlyConnected`) | Live unicast Opus to that peer only (not group Broadcast) |
| Peer offline / SFU-only / no direct PC | Existing Base Station clip upload (`SendChannelClip`) |

Group Hold-to-Talk is unchanged (`StartTalking` → `Broadcast`).

## Non-goals this slice

- Private use of the mesh SFU / Hub rooms
- Requiring both parties' `FocusedBy` (singular field still last-writer-wins)
- Peer-to-peer voice-note transfer without Base Station
- Multi-Base Station voice blob replication

## Versions

- Android phone `1.1.0`
- iOS `0.3.0`

## Build

```bash
./tools/gomobile-bind-android.sh
./tools/gomobile-bind-ios.sh
cd android && ./android-build.sh :app:assembleDebug
cd ios && xcodegen generate
```
