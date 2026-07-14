# TODO: P2P voice notes and remaining private-relay work

See also Phase 6 first slice: [`2026-07-14-phase6-private-live-talk.md`](2026-07-14-phase6-private-live-talk.md) (**live unicast** when the peer has a direct mesh PeerConnection — shipped in progress).

Deferred from [`2026-07-13-voice-message-and-private-channels.md`](2026-07-13-voice-message-and-private-channels.md).

## Direct P2P voice-note transfer

When both sender and recipient are online on the same LAN, transfer Opus clips peer-to-peer (data channel or short-lived HTTP between nodes) and bypass the Base Station inbox. Fall back to store-and-forward when the peer is unreachable.

## Live private-channel SFU rooms

✅ Shipped as Hub **unicast routes** in `server` 1.4.0 / android `1.2.0` / ios `0.4.0` (not full named rooms). Private Hold-to-Talk over the SFU uses `SetRoute` / `InjectTo` so Opus only reaches the target peer. See Phase 6 doc.

## Multi-Base-Station voice blob replication

✅ Shipped in `server` 1.3.1 — see Phase 6 doc. After each device-registry pull, peer Base Stations also pull `GET /api/sync/channels` and `GET /api/sync/voice-notes` (plus audio blobs for new notes).
