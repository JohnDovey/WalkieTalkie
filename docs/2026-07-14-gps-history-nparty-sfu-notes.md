# GPS history, N-party channels, SFU notes, clock skew

Shipped 2026-07-14 with `server` **1.8.0** / android **1.6.0** / ios **0.8.0**.

## 1. `gps_history` bucket

`registry.SetLocation` appends a sample to bbolt bucket `gps_history` keyed `{deviceID}/{unixNano}` → JSON `GeoPoint`. Near-identical lat/lon within `GPSIntervalSeconds` is deduped. Cap **500** samples/device; samples older than **7 days** purged hourly from the Base Station (same cadence as voice purge).

API: `GET /api/devices/{id}/gps-history?limit=` returns oldest→newest trail points. History is **local to Bases that received the fix** — no multi-Base history sync in this slice.

## 2. True N-party private channels

Canonical membership is `Channel.Participants []string` (A/B always re-derived as `[0]`/`[1]` for older readers). Sync `UpsertChannelFromSync` **unions** participants and pending invites.

- `POST /api/channels/invite` — create 2-party (unchanged)
- `POST /api/channels/{id}/invite` `{from,to}` — add pending invite; Accept promotes into `participants`
- Channel clip upload fans out **one Note per other participant**
- `ChannelView.peers[]` for UIs; singular `peerId` kept for 1:1
- Hub→direct Talk bridge and live-talk peer lists use full membership

## 3. SFU DataChannel for voice notes

Labeled `"voicenote"` DataChannel in the **initial** SFU offer (Hub `OnDataChannel`, Client `CreateDataChannel` before `CreateOffer`). Hub stream-forwards framed chunks by `toId` or Hub-room fan-out — does not buffer full 8MiB.

Send order on phones (`core/mobile`): **Direct DC → SFU DC (if joined) → HTTP**. Successful Direct/SFU still mirrors to Base.

## 4. Multi-Base sync clock drift

1. Sync pull estimates peer offset from HTTP `Date` (fallback `GET /api/time`) and subtracts it from incoming `LastSeen` / location timestamps before merge.
2. Soft floor: remote must be ahead of local by more than `SyncClockSkewSeconds` (default **3**, editable in Settings) to win `MergeRemoteRegistry`.

Voice/channel merges remain ID/status-based (unchanged).
