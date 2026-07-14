# TODO: P2P voice notes and remaining private-relay work

See also Phase 6 first slice: [`2026-07-14-phase6-private-live-talk.md`](2026-07-14-phase6-private-live-talk.md) (**live unicast** when the peer has a direct mesh PeerConnection — shipped in progress).

Deferred from [`2026-07-13-voice-message-and-private-channels.md`](2026-07-13-voice-message-and-private-channels.md).

## Direct P2P voice-note transfer

When both sender and recipient are online on the same LAN, transfer Opus clips peer-to-peer (data channel or short-lived HTTP between nodes) and bypass the Base Station inbox. Fall back to store-and-forward when the peer is unreachable.

## Live private-channel SFU rooms

Phase 6 covers **direct unicast** only. Still TODO: private-channel use of `core/relay` + `server/relay` Hub rooms when peers are SFU-only / force-relayed.

## Multi-Base-Station voice blob replication

Replicate `voice_notes` metadata and Opus blobs between Base Stations the same way device registry sync works today, so a recipient can pick up waiting notes from any Base Station on the LAN.
