# Usage & traffic stats

Base Station tracks activity into hourly bbolt buckets (`usage_stats`) and exposes it on **`/stats`** and **`GET /api/stats?range=today|week|month|all`**.

## What is counted

| Category | Metrics |
|----------|---------|
| Voice notes (DM) | uploads + upload bytes, downloads + download bytes, acks |
| Private channels | invites, accepts, closes; channel clip uploads + bytes |
| General / live PTT | talk sessions; Opus bytes sent & received **on this Base Station process only** |
| Nodes | new devices first seen, touch count; lifetime unique + devices currently known |

Live mesh audio between two phones that never involves this Base Station’s PeerConnections is **not** counted (P2P by design). The Stats page notes this.

## Storage

Hourly keys `YYYY-MM-DDTHH` (UTC) in `walkietalkie.db`, flushed every ~30s. API rolls up to daily series for charts.
