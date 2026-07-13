# WalkieTalkie — Cross-Platform PTT App: Implementation Plan

## Context

The user wants a cross-platform push-to-talk (PTT) "walkie talkie" app: press a button, talk, and be heard live on every other device running the app, regardless of platform. Devices must auto-discover each other (LAN or direct device-to-device — Bluetooth/Wi-Fi/whatever's best) with zero manual pairing, and a Go server component must maintain a live registry of every device ever seen (ID, user-set name, connected/disconnected status, last-seen time, current + last-known GPS), exposed through an unauthenticated Bootstrap/jQuery web UI on a configurable port (default 9091). Build priority is explicit: Android first, then a Go desktop app (Mac/Windows/Linux), then iPhone, then Android Wear/Apple Watch last. The user explicitly asked to research and reuse existing libraries rather than reinvent protocols.

The repo (`/Volumes/JohnDovey/Projects/WalkieTalkie`) currently has only scaffolding — `docs/`, `Manual/` (`.ebhtml` format, not Markdown), `.cursor/rules/*.mdc` — no application code. This plan is the first real design pass before any code is written.

Two scope decisions were already confirmed with the user during planning:
1. **Off-LAN discovery**: when devices share no LAN, BLE still detects nearby devices of any platform and reports id/name/GPS/last-seen up to the registry, but **BLE never carries audio** — audio only flows over LAN or same-OS ad hoc links.
2. **Audio topology**: P2P mesh is the default (direct WebRTC connections between reachable devices), with the Go server also acting as a **relay fallback** for pairs that can't reach each other directly (different subnets/NAT) — relaying is not a mandatory part of the audio path.
3. **The server is itself a full participating node**, not just a registry/relay — it has its own mic/speaker via `server/audio`, joins the mesh, and can talk/listen like any other device. It auto-registers its own `Device` record with `Name` defaulting to **"Base Station: `<machine hostname>`"** (via `os.Hostname()`), following the same Name/announce rules as any other node — the web UI's Settings page (server config only, per above) doesn't rename it, but nothing stops a later local rename control from reusing the same `NameUpdate` mechanism.
4. **Multiple Base Stations on one LAN synchronize their registries with each other, on a regular interval, with last-seen as the authoritative tiebreaker.** Added 2026-07-13. See "Multi-Base-Station synchronization" below for the mechanism — this is a distinct merge rule from the single-peer `PeerReport` forwarding in decision 1/§ Core data model: that's one device vouching for one peer it saw; this is two servers reconciling their whole device lists against each other.

## Research findings driving the design

- **`pion/webrtc` (v4)**, the dominant pure-Go WebRTC stack (LiveKit and most self-hosted WebRTC media servers are built on it), handles ICE/SDP/RTP and **negotiates Opus by default** — but it does **not** itself encode/decode audio samples. That's a real gap: actual mic-PCM→Opus and Opus→speaker-PCM codec work still needs something else in the pipeline (see Audio layer below — this is a correction to an earlier assumption that WebRTC "just handles Opus" end-to-end).
- **WebRTC works on a pure LAN with no STUN/TURN server** — ICE gathers host candidates from local interfaces directly, which same-subnet peers connect over immediately. STUN/TURN (or our own relay) is only needed cross-subnet — this is exactly where the server-relay fallback slots in.
- **`gomobile bind`** is actively maintained and compiles one Go package into both an Android `.aar` and an iOS `.xcframework`, enabling a single shared Go **core** (protocol, registry, discovery, mesh orchestration) reused across Android, iOS, and the native Go desktop build — only thin native shells (UI, mic/speaker I/O, GPS, BLE) differ per platform.
- **mDNS/DNS-SD** is the standards-based, cross-platform-interoperable LAN discovery mechanism — plain UDP multicast, so the *same* Go library (`grandcat/zeroconf` recommended; more features/backoff than `hashicorp/mdns`, which has known IPv6/enumeration quirks) works identically on desktop and, via gomobile, on Android/iOS. No need for Android `NsdManager` or iOS Bonjour APIs.
- **Off-LAN ad hoc discovery does not unify across ecosystems.** Apple's MultipeerConnectivity and Google's Nearby Connections are confirmed non-interoperable (a Google engineer's own words: "'multipeer' doesn't mean cross-platform"). Only BLE advertising/scanning works on both Android and iOS for cross-ecosystem presence — but `tinygo-org/bluetooth` (the cross-platform Go BLE library) explicitly does **not** support Android or iOS, so BLE has to be native platform code (Android `BluetoothLeScanner`/`AdvertiseCallback`, iOS `CoreBluetooth`) reporting into the shared core via a callback bridge, not a shared Go BLE package. BLE payloads are too small to reliably carry GPS, reinforcing the "presence-only" decision above.
- **iOS has a purpose-built `PushToTalk` framework** (`PTChannelManager`, iOS 16+, WWDC22 session 10117) for exactly this use case — background-capable transmit/receive with system-level UI. Use it instead of hand-rolling background audio session management on iOS.
- **Android** needs a foreground service declaring the `microphone` service type (required since Android 10) to keep recording while backgrounded/screen-off — standard API, no extra framework needed.
- **Audio capture/codec, corrected**: `pion/mediadevices` (the natural pairing with pion/webrtc for mic/speaker I/O) requires **cgo** (via `malgo`/miniaudio) for capture, and real Opus encode/decode also generally means a cgo binding (`hraban/opus`, wrapping libopus) somewhere in the pipeline. cgo is a real complication for `gomobile bind` cross-compilation to Android/iOS. Resolution: keep the **shared `core`** module free of cgo and audio-format-agnostic — it only moves already-encoded Opus frames between a generic `AudioSource`/`AudioSink` interface and the pion track. Actual capture + Opus encode/decode is native per platform: Android via `MediaCodec` (hardware-accelerated Opus, no cgo needed), iOS via a small Opus binding bundled into the iOS build only (since AVFoundation has no native Opus), and desktop via `pion/mediadevices` + cgo (fine there — the desktop `server` binary has no gomobile/cross-compile constraint). This needs a Phase 1 spike to nail down the exact iOS codec binding choice.
- **`bbolt`** (pure Go, ACID, no cgo) is a good fit for the registry/settings store; SQLite is the fallback if relational GPS-history queries are wanted later.
- **Desktop needs no native GUI toolkit** — since the spec already requires a Bootstrap+jQuery web UI on port 9091, the "Go app" for Mac/Windows/Linux *is* that web server plus the mesh audio engine in one process. `getlantern/systray` (cgo) is available later for a tray-icon affordance, not required for v1.

## Monorepo layout

```
WalkieTalkie/
├── go.work                      # ties core + server together for local dev
├── core/                        # shared Go module (github.com/JohnDovey/WalkieTalkie/core) — gomobile-bound, no cgo
│   ├── proto/                   # wire message envelope + payload types, versioned
│   ├── registry/                # Device model, bbolt-backed store, upsert/precedence rules
│   ├── discovery/
│   │   ├── mdns/                # grandcat/zeroconf wrapper: Register/Browse, TXT record encode/decode
│   │   └── ble/                 # interface only — BLEBridge — no Go impl for mobile, see below
│   ├── signaling/                # per-node HTTP offer/answer endpoint + client
│   ├── media/                    # pion/webrtc mesh manager, generic AudioSource/AudioSink interfaces
│   ├── relay/                    # pion-based relay/SFU primitives, shared by server
│   ├── config/                   # settings struct; persistence path resolved per-platform (see below)
│   └── mobile/                   # gomobile-bind facade (primitive-typed exported funcs/callbacks)
├── server/                       # Go main package = desktop app AND the "Go server" — also a talk/listen node itself (cgo OK here)
│   ├── main.go
│   ├── api/                      # REST + WS handlers (devices, settings, peer-reports)
│   ├── web/                      # Bootstrap+jQuery templates/static assets
│   ├── relay/                    # wires core/relay into the server process
│   └── audio/                    # pion/mediadevices-based mic/speaker capture + Opus codec (cgo)
├── android/                      # Gradle project — duplicate ClonesApp conventions (see android-build.mdc)
│   ├── android-build.sh
│   ├── gradle.properties
│   ├── local.properties          # gitignored, sdk.dir=/Volumes/JohnDovey/Android/Sdk
│   ├── settings.gradle.kts
│   ├── app/                      # phone app module — Kotlin, MediaCodec Opus, BLE, FusedLocation
│   └── wear/                     # phase 5, added later
├── ios/                           # phase 4 — Swift/Xcode app + core.xcframework consumer
│   └── WalkieTalkieWatch/         # phase 5, added later — see watchOS open question below
├── tools/                         # gomobile bind wrapper scripts; project-scoped GOPATH/GOCACHE redirection
├── docs/                           # existing — this plan + follow-on design docs
└── Manual/                         # existing — update chapters as features land
```

`go.work` lets `server` depend on local `core` without publishing/tagging during development. Keeping `core/discovery/ble` interface-only (real implementation lives in native platform shims calling back through `core/mobile`) and keeping all cgo (audio capture/codec on desktop, BLE on mobile) *outside* `core` is the key structural rule that keeps `gomobile bind` reliable.

## Core data model & wire protocol

`core/registry/device.go` — canonical `Device` record: `ID` (UUIDv4, generated once per install, not MAC-derived), `Name` (user-editable **on the device**, not from the browser — see clarification below), `Platform`, `Status` (connected/disconnected), `LastSeen`, `CurrentLocation`/`LastKnownLocation` (`{Lat, Lon, Accuracy, Timestamp}`), `DiscoveryMethods` (`["mdns"]`/`["ble"]`/`["direct"]`, can combine), `ReportedBy` (device IDs that forwarded this entry — empty if the server heard from it directly), `Capabilities` (e.g. `["audio"]` vs `["presence-only"]` for BLE-only entries), `ProtocolVersion`.

**Confirmed**: the web UI's Settings page is for *server* config (port, GPS interval, relay toggle) only — not per-device renaming from the browser. Devices set their own name locally; the server just displays it.

Wire envelope: JSON over HTTP/WS for v1 (simplest to debug; protobuf is a possible later optimization, not needed now): `{ "type": "...", "version": 1, "sender": "<device-id>", "ts": "...", "payload": {...} }`.

Message types:
- **Announce** — device → server on connect: `{id, name, platform, capabilities}`; mirrored in the mDNS TXT record for LAN discovery.
- **GPSUpdate** — device → server on a configurable interval (default ~30s): `{lat, lon, accuracy, ts}`.
- **NameUpdate** — device → server immediately on next connect if the name changed while offline.
- **PeerReport — the concrete mechanism for "A forwards B's details to the server"**:
  1. Device A's discovery layer (mDNS browse or BLE scan callback) updates A's own local core registry cache the moment it sees B — this happens even with no server connection, satisfying "discoverable without being connected."
  2. When A has a live connection to the server, it periodically (throttled per peer, e.g. every 30–60s or on change) sends a `peer_report`: `{reporter: "A", peer: {id, name, platform, discoveryMethod, lastSeenByReporter, gps: null-or-{...}}}`.
  3. `server/api/devices.go`'s peer-report handler calls `registry.UpsertFromReport(...)`. Precedence: a device's own direct self-reported data always outranks a peer report about it (a stale BLE report from A must never flip B's status to disconnected or overwrite B's own GPS if B is also connected directly); among peer reports, most-recent wins. The server appends A to `B.ReportedBy` so the UI can show "seen via A (BLE), 2 min ago" for devices it never directly heard from.
- **Disconnect** — WS close sets `Status=disconnected` and freezes `LastKnownLocation`.

## Discovery layer

**LAN (mDNS/DNS-SD)**: `grandcat/zeroconf`, service type `_walkietalkie._tcp.local.`, instance name = device ID (stable, unlike display names). TXT record: `id`, `name` (url-encoded), `ver`, `sig` (signaling port), `plat`, and optionally `lat`/`lon`/`acc` (GPS, when known) and `api` (the server REST port — set only by `server/main.go`, absent on mobile nodes; see "Multi-Base-Station synchronization" below, this is how one Base Station recognizes another). `core/discovery/mdns` exposes `Register`/`Browse` wired directly into `registry.UpsertFromDirectContact`. Confirmed: `zeroconf.Server.SetText` supports in-place TXT updates, so re-announcing on a GPS refresh or local rename doesn't need a full unregister+re-register.

**BLE presence bridge (off-LAN)**: `core/discovery/ble` defines a Go interface only (`ReportPeerSeen(id, rssi, payload)`); real scanning/advertising is native — `android/.../BleDiscoveryManager.kt` (`BluetoothLeScanner`/`AdvertiseCallback`) and `ios/.../BleDiscoveryManager.swift` (`CBCentralManager`/`CBPeripheralManager`), both feeding results into gomobile-exported functions. BLE-discovered peers are presence-only stubs (`gps: null`, `capabilities: ["presence-only"]`) — reading GPS over a BLE GATT characteristic is a possible future enhancement, explicitly out of MVP scope.

## Multi-Base-Station synchronization

**Requirement (added 2026-07-13)**: if more than one Base Station (a `server` instance, not a phone/mobile node) is running on the same LAN, once they discover each other they must synchronize their device registries with each other on a regular, ongoing interval — not just once. **Last-seen is the authoritative tiebreaker**: for any device both Base Stations know about, whichever record has the more recent `LastSeen` timestamp wins the merge, wholesale (name, status, GPS, discovery methods — the newer record replaces the older one, not a field-by-field patch).

This is deliberately a **different merge rule** from the existing `PeerReport` mechanism (see Core data model above): `PeerReport` is one ordinary device vouching for a single peer it happened to see, where direct self-reported data should always outrank secondhand hearsay. Base-Station-to-Base-Station sync is two equally-authoritative registries reconciling their *entire* device lists against each other — either side could have the more recent, equally legitimate sighting of a given device (e.g. it roamed from being in range of Base Station A to being in range of Base Station B), so pure last-seen-wins is the right rule here and would be wrong for `PeerReport`.

**How Base Stations recognize each other**: only a `server` instance runs `server/api`'s REST surface (a phone/mobile node, via `core/mobile`, does not) — so the mDNS TXT record gets one more optional field, `api=<port>`, set only by `server/main.go` (never by `core/mobile`). A peer whose mDNS sighting includes a non-zero `api` field is a Base Station; a sighting without it (Android/iOS) is an ordinary node and is never a sync target.

**Sync mechanism**: on discovering another Base Station via mDNS, each side independently starts a repeating ticker (interval configurable, default matching the existing GPS-update cadence — new `Settings.SyncIntervalSeconds`, default 30) that does a plain `GET http://<peer-host>:<peer-api-port>/api/devices` against the peer (reusing the *existing* endpoint — no new API surface needed on the "provider" side) and merges the returned list into its own registry. Both sides pulling independently is sufficient for full bidirectional convergence without needing a push endpoint. New `registry.Store` method: `MergeRemoteRegistry(remote []Device) (updated int, err error)` — for each incoming device, replace the local record if-and-only-if the incoming one's `LastSeen` is strictly newer (or the device is unknown locally); otherwise keep the local record untouched. This is intentionally simpler than `UpsertFromReport`'s precedence rules and should not reuse that function.

**Known limitation to flag, not solve now**: last-seen comparison is wall-clock (`time.Time`) across potentially different machines with no enforced clock sync (no NTP guarantee assumed). Minor clock drift between Base Stations could make an actually-older sighting look newer. Acceptable for v1 — flagged in Risks below rather than solved (e.g. by switching to a vector-clock or hybrid-logical-clock scheme), since typical LAN machines' clocks are close enough in practice for a walkie-talkie app's purposes.

**Where this lands in the phased plan**: Phase 1 already proves two desktop instances discovering each other and exchanging PTT audio, but not yet registry sync — add sync as explicit work in **Phase 3 (Desktop hardening)**, since that phase is already about multiple desktop/Base-Station instances together; extend its Verify step to also assert convergence (kill a device's direct connection to Base Station A, confirm Base Station B — which never saw it directly — nonetheless shows correct last-seen/status for it within one sync interval, purely via A→B registry sync).

## Audio layer

`core/media/`: `session.go` (one `PTTSession` per local user, lazily opens one `pion.PeerConnection` per reachable peer — full mesh), `mesh_manager.go` (bounded-timeout direct-connect attempt per peer pair, falls back to server relay on failure), `ptt_controller.go` (`StartTalking()`/`StopTalking()` — Android wires to button down/up, desktop to a hotkey/on-screen button, iOS to `PTChannelManager`'s transmit delegate callbacks instead of a raw button).

**Signaling without a dedicated server, on LAN**: every node runs its own small HTTP endpoint (`core/signaling`); the mDNS TXT record's `sig` field tells peers where to reach it. A wants to call B → `POST http://<B-ip>:<sig-port>/offer` with A's SDP offer, B answers synchronously in the response body. Same-subnet ICE host candidates connect directly per the research above — start single-round-trip, add a trickle-ICE WS only if reliability requires it.

**Server-relay fallback** (`core/relay` + `server/relay`): reuses pion primitives — each of two unreachable peers opens a `PeerConnection` to the server, which forwards `TrackRemote` audio onward as a `TrackLocal` to the other (a minimal SFU from pion's own APIs, no separate SFU library). Only activates when direct ICE fails.

**Codec/capture split (corrected from initial WebRTC-does-everything assumption)**: pion negotiates Opus but doesn't encode/decode it. Keep `core` cgo-free by doing capture+codec natively: Android via `MediaCodec` (hardware Opus, no cgo), iOS via a bundled Opus binding (needs a Phase-1 spike to pick one, since AVFoundation has no native Opus), desktop via `pion/mediadevices` + cgo in `server/audio` (no gomobile constraint there). `core/media` only deals in already-encoded Opus frames via a generic `AudioSource`/`AudioSink` interface.

## Registry + web UI

**Storage**: `bbolt`, buckets `devices` (keyed by ID, JSON `Device`) and `config` (settings). A future `gps_history` bucket is a natural extension, not required now.

**Runtime data location**: the project's `/Volumes/JohnDovey/tmp/walkietalkie-*` convention is for *this dev machine's* build tooling (GOPATH/GOCACHE/TMPDIR) only — the **shipped app's** registry/config on any machine it runs on must use the OS-appropriate per-user app-data dir (`os.UserConfigDir()`: `~/Library/Application Support/WalkieTalkie/` macOS, `~/.config/walkietalkie/` Linux, `%APPDATA%\WalkieTalkie\` Windows), never a hardcoded `/Volumes/JohnDovey` path.

**API** (`server/api`): `GET /api/devices`, `GET /api/devices/{id}`, `POST /api/devices/announce`, `POST /api/devices/{id}/location`, `PUT /api/devices/{id}/name` (device-originated only), `POST /api/devices/peer-reports`, `GET`/`PUT /api/settings` (port change restarts `http.Server` and surfaces a "reconnect at http://host:NEWPORT" notice; settings also gains `SyncIntervalSeconds`, see Multi-Base-Station synchronization above). No dedicated sync endpoint is needed — Base-Station-to-Base-Station sync reuses `GET /api/devices` as-is. Start with jQuery polling (`$.getJSON` every few seconds) rather than a WS push channel — matches the plain Bootstrap+jQuery spirit of the spec; WS is a later enhancement.

**Pages** (`server/web`, Go `html/template` + Bootstrap + jQuery, no auth): `/` device list (name, platform icon, status badge, last-seen, GPS, discovery-method badge including "via <reporter> (BLE)"); `/settings` (port, GPS interval, relay toggle).

## Phased delivery

**Phase 1 — Shared core foundation** (prerequisite the Android work builds on, not a separate priority tier) — ✅ done, verified 2026-07-13
- Repo scaffolding: `go.work`, `core` (`registry`, `proto`, `config`, `discovery/mdns`), minimal `server` (bbolt + `GET /api/devices` + bare Bootstrap page). Protocol v0.1.0.
- WebRTC mesh MVP on **desktop only**, two instances on the same LAN — first end-to-end milestone. Each instance auto-registers itself as "Base Station: `<hostname>`" and participates in the mesh as a real talk/listen node, not just a registry.
- **Verified**: two `server` processes on one LAN discover each other via mDNS within ~1s, each showing up as "Base Station: `<hostname>`"; the WebRTC connection reaches "connected" (ICE/DTLS confirmed via connection-state logging, not just signaling not-erroring); both web UIs show the other as Connected with correct last-seen. A glare bug (simultaneous mutual discovery opening two PeerConnections per pair, leaking the loser) was found and fixed during this verification.
- Not yet implemented: multi-Base-Station registry sync (see below) — deferred to Phase 3, since Phase 1's milestone only required two instances to discover and talk, not to reconcile full device lists.

**Phase 2 — Android** (top priority) — in progress 2026-07-13
- `gomobile bind` core → `.aar` — done; required two undocumented fixes: `-ldflags="-checklinkname=0"` (pion's `wlynxg/anet` dependency uses `//go:linkname` into `net.zoneCache`, restricted by Go 1.23+), and binding `./mobile` together with `./media` in one `gomobile bind` invocation (binding `./mobile` alone silently drops `StartNode` with no error, since gobind needs to see `media.AudioSource`/`AudioSink` explicitly to generate their Java bindings). Both captured in `tools/gomobile-bind-android.sh`.
- `android/` scaffolded per `android-build.mdc`/ClonesApp precedent; PTT UI (Compose), foreground service (`microphone` type) hosting core's mesh/registry/mDNS lifecycle, BLE presence bridge (`BluetoothLeScanner`/`AdvertiseCallback`), GPS via `FusedLocationProviderClient` — implemented, first `assembleDebug` build in progress.
- Real `MediaCodec` Opus capture/playback not yet implemented — the app currently uses placeholder silent `AudioSource`/`AudioSink` stubs so the rest of the pipeline (service lifecycle, discovery, mesh) could be verified independently first.
- **Verify** (not yet done): Android + one desktop instance on the same Wi-Fi discover each other and exchange PTT audio both ways; with Android Wi-Fi off, confirm a third device's BLE scan still reports the phone's presence to the server (validates offline-forwarding end to end).

**Phase 3 — Desktop hardening (Mac/Windows/Linux)** — packaging, not new architecture; core loop already proven in Phase 1
- Windows/Linux build scripts (dev machine is macOS); confirm `pion/mediadevices` on all three OSes.
- Settings port-change flow; optional `getlantern/systray` tray icon.
- **Multi-Base-Station registry synchronization** (new requirement, added 2026-07-13 — see the dedicated section above): mDNS TXT `api` field, `registry.Store.MergeRemoteRegistry`, per-peer sync ticker on discovering another Base Station, `Settings.SyncIntervalSeconds`.
- **Verify**: three-way mesh (Win+Mac+Linux) plus the Phase-2 Android device, group PTT; plus registry-sync convergence (kill one device's direct connection to Base Station A, confirm Base Station B shows correct last-seen/status for it within one sync interval via A→B sync alone).

**Phase 4 — iPhone**
- `gomobile bind` xcframework (`ios,iossimulator`); Swift app integrating `PTChannelManager` for background PTT with system transmit UI (not hand-rolled background audio); `CoreBluetooth` BLE bridge; `CoreLocation` GPS.
- **Verify**: iPhone joins the Android+desktop mesh; `PTChannelManager`'s system UI reflects the active transmitter; PTT works with the screen locked.

**Phase 5 — Wearables (lowest priority)**
- `android/wear`: Wear OS is a full Android runtime, reuses the same AAR directly — just Wear UI + companion pairing for mic/BLE hardware.
- `ios/WalkieTalkieWatch`: **open question** — gomobile's iOS targets historically exclude watchOS, so the watch app may need to relay through the paired iPhone via `WatchConnectivity` rather than embedding `core` directly. Needs its own research spike at the start of this phase, not before.

## Risks and tradeoffs

- **No-auth web UI exposes live GPS** to anyone reaching port 9091 (LAN by default, or the internet if a router misconfigures port-forwarding) — an explicit accepted tradeoff per the spec; document a "do not expose beyond a trusted LAN" warning, and treat any future internet-facing ask as a separate redesign requiring real auth/TLS.
- **BLE presence-only** means cross-ecosystem off-LAN entries only ever show id/name/proximity, never GPS or audio — the UI must visually distinguish these from fully-connected entries to avoid users thinking "discovered" means "fully working."
- **Mesh scaling**: full P2P mesh is O(n²); expect noticeable degradation past roughly 6–8 simultaneous participants. A "force-relay above N participants" mode is a plausible future addition, out of scope for v1.
- **gomobile/cgo cross-build complexity**: Android needs the NDK alongside `ANDROID_HOME`; iOS xcframework builds need Xcode on this Mac (no Linux/Windows CI path). Keeping `core` itself cgo-free (audio codec/capture and BLE both pushed to native platform shims) is a hard constraint to keep this tractable, not just a nicety.
- **mDNS quirks**: `zeroconf`/`hashicorp-mdns` both have known rough edges (enumeration staleness, IPv6). Budget real time in Phase 1 to validate against actual Wi-Fi hardware, since corporate/guest networks with IGMP-snooping oddities behave differently from a typical home router.
- **Signaling endpoints are unauthenticated** too — any device reaching a node's signaling port can POST a bogus SDP offer. Low risk on a private LAN, consistent with the app's no-auth theme, but worth documenting alongside the web-UI risk.
- **Multi-Base-Station sync assumes reasonably close wall clocks**: last-seen-wins conflict resolution compares `time.Time` values from potentially different machines with no enforced NTP sync. Clock drift could make an actually-older sighting look newer during a merge. Accepted as a v1 limitation rather than solved with vector/hybrid-logical clocks — typical LAN machines' clocks are close enough in practice for this app's purposes.

## Verification approach

Each phase's "Verify" step above is a go/no-go gate — don't start Android work until the Phase 1 two-desktop PTT milestone works end-to-end; don't start iOS until Android+desktop are proven together. Update the relevant `Manual/chapters/NNN-*.ebhtml` chapter whenever a phase lands a user-facing capability (PTT button, device list, settings page), per the existing `manual-directory.mdc` convention. This plan document itself should be saved to `docs/2026-07-13-implementation-plan.md` per the `docs-directory.mdc` convention.
