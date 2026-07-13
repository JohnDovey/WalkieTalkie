# Voice message and private channels

## Goal

Extend **Android** and the **Go Base Station** (web UI + API + storage) with:

1. **Voice messages** — per-node async Opus clips, held on the server until the recipient connects (min **21 days**, then delete).
2. **Private channels** — invite only when the peer is **online/connected**; server-relayed 1:1 session listed in a left drawer; PTT-style UX scoped to that pair; while the recipient is not focused on the channel, clips queue as voice notes with a count badge.

**Deferred:** see [`TODO-p2p-voice-and-private-relay.md`](TODO-p2p-voice-and-private-relay.md) for peer-to-peer delivery and live WebRTC/SFU private audio.

## Architecture

Unify both features on **server store-and-forward of Opus clips**, not live mesh audio:

- **Voice message:** record → upload targeting `toDeviceId` → stay in inbox until fetch/ack or TTL purge.
- **Private channel:** invite (both must be `connected`) → channel listed in drawer → hold-to-talk records a clip tagged with `channelId` → if peer is focused on that channel, deliver/auto-play promptly; if not, queue and show count.
- Main-screen group PTT stays the existing WebRTC `Broadcast` mesh.

**Base Station requirement:** a LAN Base Station (mDNS `api` port) is the inbox. If none is reachable, voice-note send fails with a clear error. Multi-Base sync of voice blobs is out of scope for v1.

## Data model

bbolt buckets alongside `devices` / `config`:

- **`voice_notes`** — id, fromId, toId, optional channelId, createdAt, expiresAt (= createdAt + 21d), size, path, status (`queued` / `delivered` / `deleted`)
- **`private_channels`** — id, participantA, participantB, createdAt, status (`pending` / `active` / `closed`)
- Blob files: `{dataDir}/voice-notes/{id}.opus`

Device list API includes `pendingVoiceNotes` (count waiting for that device).

## API

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/voice-notes` | Opus upload (`from`, `to`, optional `channelId`) |
| GET | `/api/voice-notes?for={deviceId}` | Inbox metadata |
| GET | `/api/voice-notes/{id}/audio` | Download Opus body |
| POST | `/api/voice-notes/{id}/ack` | Mark played / delivered |
| DELETE | `/api/voice-notes/{id}` | Soft-delete |
| GET | `/api/devices` | Extended with `pendingVoiceNotes` |
| POST | `/api/channels/invite` | Invite (peer must be connected) |
| POST | `/api/channels/{id}/accept` | Accept invite |
| GET | `/api/channels?for={deviceId}` | Channels + unread counts |
| POST | `/api/channels/{id}/close` | Leave/close |
| POST | `/api/channels/{id}/focus` | Mark focused (for auto-play vs queue) |
| POST | `/api/channels/{id}/blur` | Clear focus |

## UI

- Per-row **Voice Message** icon (all known nodes) and **Invite to Chat** (connected only).
- Left drawer: private channels + voice-message badges with counts.
- Voice-note thread: list / play / reply / delete.
- Record flow: Stop+Send / Cancel → upload.
- Private channel screen: same Talk affordance, scoped clips.

## Versions

Minor bumps: server **0.6.0**, android **0.6.0**.
