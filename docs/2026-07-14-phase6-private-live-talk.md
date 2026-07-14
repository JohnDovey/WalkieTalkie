# Phase 6 — Private-channel live Talk

Started 2026-07-14. Slices of [`TODO-p2p-voice-and-private-relay.md`](TODO-p2p-voice-and-private-relay.md).

## Status

**Software complete** — live unicast (direct + SFU Hub), named Hub rooms on channel focus, Hub→direct Talk bridge, multi-Base voice sync, P2P voice-note DataChannel + Base mirror.

## Behaviour

### Private Hold-to-Talk

| Condition | Behaviour |
|-----------|-----------|
| Peer has **direct** mesh PeerConnection | Live unicast Opus |
| Peer reachable via **Base Station SFU** | Live Hub unicast (`SetRoute` / `InjectTo`) |
| Peer on Hub talks to a **DirectConnected-only** peer | Base Station bridges Hub→direct (`SendTo`) |
| Peer offline / not reachable live | Clip upload via Base Station |

UI: **Mode: live mesh** / **Mode: live relay** / **Mode: clip via Base Station**.

### Named Hub rooms (`1.6.0+`)

Focusing a private channel joins Hub room `channelID` (empty room = group mesh). SFU fan-out stays within that room unless a temporary `SetRoute` is active for 1:1 Talk. Blur returns to the group room. Same single SFU PeerConnection (no second PC).

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
- Focus/blur joins/leaves the Hub room for the Base Station publisher

### Multi-Base voice sync (`1.3.1+`)

Registry sync tick also pulls `/api/sync/channels` and `/api/sync/voice-notes` (+ audio blobs). Mirrored P2P notes participate in that sync.

## Versions

- Android phone `1.4.0`
- iOS `0.6.0`
- Server `1.6.0`

## Build

```bash
./tools/gomobile-bind-android.sh
./tools/gomobile-bind-ios.sh
cd android && ./android-build.sh :app:assembleDebug
cd server && go run .
```

## Manual export

```bash
./tools/export-manual.sh
# → Manual/output/walkie-talkie-mesh-chat.{epub,pdf,docx}
```
