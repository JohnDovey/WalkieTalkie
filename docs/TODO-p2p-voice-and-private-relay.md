# TODO: P2P voice notes and remaining private-relay work

See also Phase 6: [`2026-07-14-phase6-private-live-talk.md`](2026-07-14-phase6-private-live-talk.md).

Deferred from [`2026-07-13-voice-message-and-private-channels.md`](2026-07-13-voice-message-and-private-channels.md).

## Direct P2P voice-note transfer

✅ Shipped in android `1.3.0` / ios `0.5.0` / server `1.5.0` — `"voicenote"` DataChannel on direct mesh PeerConnections; local inbox on phones; Base Station `ImportNote` on receive; fall back to store-and-forward when not DirectConnected.

✅ Mirror-upload (`1.5.1` / android `1.3.1` / ios `0.5.1`): after a successful P2P send or receive, best-effort `UploadNote` with the same note ID so multi-Base voice sync can replicate the clip.

## Live private-channel SFU rooms

✅ Shipped as Hub **unicast routes** in `server` 1.4.0 / android `1.2.0` / ios `0.4.0`. Private Hold-to-Talk over the SFU uses `SetRoute` / `InjectTo`.

✅ Named multi-party Hub rooms + Hub→direct Talk bridge in `server` 1.6.0 / android `1.4.0` / ios `0.6.0`.

✅ Room-scoped channel Talk + Hub→direct room bridge + Base→DirectConnected voice-note push in `server` 1.7.0 / android `1.5.0` / ios `0.7.0`. See Phase 6 doc.

## Multi-Base-Station voice blob replication

✅ Shipped in `server` 1.3.1 — see Phase 6 doc. After each device-registry pull, peer Base Stations also pull `GET /api/sync/channels` and `GET /api/sync/voice-notes` (plus audio blobs for new notes).
