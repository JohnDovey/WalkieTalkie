# Phase 6 — Private-channel live Talk

Started 2026-07-14. Slices of [`TODO-p2p-voice-and-private-relay.md`](TODO-p2p-voice-and-private-relay.md).

## Status

**Software complete** — live unicast (direct + SFU Hub), multi-Base voice sync, P2P voice-note DataChannel, P2P→Base mirror-upload for multi-Base visibility.

## Behaviour

### Private Hold-to-Talk

| Condition | Behaviour |
|-----------|-----------|
| Peer has **direct** mesh PeerConnection | Live unicast Opus |
| Peer reachable via **Base Station SFU** | Live Hub unicast (`SetRoute` / `InjectTo`) |
| Peer offline / not on Hub | Clip upload via Base Station |

UI: **Mode: live mesh** / **Mode: live relay** / **Mode: clip via Base Station**.

### Voice notes / channel clips

| Condition | Behaviour |
|-----------|-----------|
| Peer **DirectConnected** | Opus over `"voicenote"` DataChannel → recipient local inbox (phones) or Base Station store; then best-effort mirror-upload to Base with the same note ID |
| Otherwise | Existing `POST /api/voice-notes` store-and-forward |

List/download/ack merge local inbox + Base Station. Upload accepts optional `id`/`createdAt` for stable P2P mirror IDs (`ImportNote`).

### Base Station web

- `GET /api/talk/peer` → `{direct, relay, live}`
- Private panel live mesh / live relay / clip
- Receives P2P notes into the same voice-note store as HTTP uploads

### Multi-Base voice sync (`1.3.1+`)

Registry sync tick also pulls `/api/sync/channels` and `/api/sync/voice-notes` (+ audio blobs). Mirrored P2P notes participate in that sync.

## Versions

- Android phone `1.3.1`
- iOS `0.5.1`
- Server `1.5.1`

## Build

```bash
./tools/gomobile-bind-android.sh
./tools/gomobile-bind-ios.sh
cd android && ./android-build.sh :app:assembleDebug
cd server && go run .
```

## Still deferred

- Named multi-party Hub rooms / second WebRTC PC per channel
- Bridging mixed direct↔relay for notes over SFU
