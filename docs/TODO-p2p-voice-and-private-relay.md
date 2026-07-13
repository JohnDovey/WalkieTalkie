# TODO: P2P voice notes and live private-channel relay

Deferred from [`2026-07-13-voice-message-and-private-channels.md`](2026-07-13-voice-message-and-private-channels.md).

## Direct P2P voice-note transfer

When both sender and recipient are online on the same LAN, transfer Opus clips peer-to-peer (data channel or short-lived HTTP between nodes) and bypass the Base Station inbox. Fall back to store-and-forward when the peer is unreachable.

## Live private-channel WebRTC / SFU

While both participants are focused on a private channel, stream live Opus via:

- Unicast WebRTC (reuse mesh `PeerConnection`, route frames only to that peer), or
- Server SFU once `core/relay` + `server/relay` exist.

Clips / store-and-forward remain the fallback when a participant is not focused.

## Multi-Base-Station voice blob replication

Replicate `voice_notes` metadata and Opus blobs between Base Stations the same way device registry sync works today, so a recipient can pick up waiting notes from any Base Station on the LAN.
