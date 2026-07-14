# WalkieTalkie

A cross-platform push-to-talk (PTT) app: press a button, talk, and be heard live on every other device running the app — Android, desktop (Mac/Windows/Linux), iPhone, and eventually Android Wear/Apple Watch — regardless of platform. Devices auto-discover each other over the LAN (mDNS) or, off-LAN, via Bluetooth LE presence detection, with no manual pairing.

See [`docs/2026-07-13-implementation-plan.md`](docs/2026-07-13-implementation-plan.md) for the full design and phased build plan, and [`Manual/`](Manual/) for the end-user manual.

## Status

Build priority is Android first, then desktop, then iPhone, then wearables last — see the plan doc for why.

- **Phase 1 — shared Go core + desktop server**: ✅ done and verified.
- **Phase 2 — Android**: ✅ working on real hardware (live WebRTC Opus PTT, mDNS + BLE presence, GPS, voice notes / private channels via Base Station).
- **Phase 3 (desktop hardening + multi-Base-Station registry sync)**: ✅ done (registry sync, map, Old Nodes, Windows/macOS/Linux packaging scripts, system tray, Base Station mesh SFU / relay threshold). Three-OS hardware mesh not run on this Mac-only setup.
- **Phase 4 (iPhone)**: 🟡 in progress — SwiftUI shell + bind/Opus + voice notes/private channels with live Talk; `iphoneos` build verified. Device mesh / locked-screen PTT needs Team ID + hardware. See `docs/2026-07-14-ios-phase4.md`.
- **Phase 5 (wearables)**: 🟡 software complete — Wear OS `0.2.0` + watchOS WatchConnectivity relay; hardware verify pending. See `docs/2026-07-14-phase5-wearables.md`.
- **Phase 6 (private live Talk)**: ✅ software complete — live unicast (mesh/SFU), named Hub rooms, room-scoped channel Talk, Hub→direct Talk/note bridges, multi-Base voice sync, P2P notes + Base mirror. See `docs/2026-07-14-phase6-private-live-talk.md`.

**Current release track:** server `1.8.0`, android `1.6.0`, wear `0.2.0`, ios `0.8.0` (GitHub release `v1.8.0`). MeshBridge companion on `main`: `0.1.1` (not yet in a numbered product release zip).

## Repo layout

```
core/      shared Go module (registry, discovery, WebRTC mesh, signaling) — no cgo, gomobile-bound into Android/iOS
server/    the Go desktop app AND the "Base Station" server: bbolt registry, REST API, Bootstrap/jQuery dashboard
android/   Kotlin/Compose phone + Wear OS apps; shared `:mesh` library consumes core via gomobile AAR
ios/       SwiftUI iPhone app (+ WatchConnectivity watch stub); Core XCFramework (see docs/2026-07-14-ios-phase4.md)
tools/     dev scripts: Go env setup, gomobile→Android AAR / iOS XCFramework, Opus iOS, Windows/macOS/Linux server + MeshBridge builds
docs/      plans and design docs (including voice messages / private channels / MeshBridge)
Manual/    the end-user manual (.ebhtml format — see Manual/README.md)
meshbridge/ companion process: bridge Base Stations across LAN/WAN — live Talk stays local (see meshbridge/README.md)
```

## MeshBridge

Runs **next to** a Base Station. Syncs remote Base **devices + voice notes** into the dashboard **Remote Users** tab — not live Talk.

| Transport | When |
|-----------|------|
| `manual` | Known remote Base `http://host:port` |
| `ethernet` | This Mac on LAN A (Wi‑Fi) + cable into router B; mDNS on that iface |
| `wifi` | Secondary Wi‑Fi NIC + SSID |
| `punch` | QuakeMesh-style UDP punch / hub when Bases cannot share a LAN |

```sh
source tools/go-env.sh
go run ./meshbridge/cmd/meshbridge
# status: http://127.0.0.1:9095
# config: ~/Library/Application Support/WalkieTalkie/meshbridge/settings.json  (macOS)
./tools/build-macos-meshbridge.sh
```

Details: [`meshbridge/README.md`](meshbridge/README.md), [`docs/2026-07-14-meshbridge-plan.md`](docs/2026-07-14-meshbridge-plan.md), Manual chapter **MeshBridge**.

## Building the iOS app

```sh
tools/gomobile-bind-ios.sh       # → ios/Frameworks/Core.xcframework (gitignored)
tools/build-opus-ios.sh          # → ios/ThirdParty/Opus.xcframework (gitignored)
cd ios && xcodegen generate      # regenerates WalkieTalkie.xcodeproj
# Optional: copy Config/Local.xcconfig.example → Local.xcconfig and set DEVELOPMENT_TEAM
xcodebuild -scheme WalkieTalkie -sdk iphoneos build
```

Requires Xcode (`DEVELOPER_DIR` from `source-john-dovey.sh`). Simulator builds need an installed iOS Simulator runtime.
## Voice messages and private channels

Async voice notes and invite-only private channels use a LAN Base Station for store-and-forward when peers are offline or SFU-only; when both peers have a direct mesh link, clips transfer over a WebRTC DataChannel instead. See [`docs/2026-07-13-voice-message-and-private-channels.md`](docs/2026-07-13-voice-message-and-private-channels.md) and Phase 6.

## Running the desktop server

```sh
source tools/go-env.sh   # redirects GOPATH/GOCACHE off this dev machine's internal disk
cd server
go run .                 # starts the Base Station on http://localhost:9091
```

Open `http://localhost:9091` for the device dashboard, `http://localhost:9091/settings` for server settings (port, etc — no login, by design). Useful flags for running more than one instance on one machine (development only): `--port`, `--data-dir`, `--name`, `--no-audio`, `--no-tray`.

Release-style binaries (full audio):

```sh
./tools/build-macos-server.sh    # arm64 + amd64 + universal → /Volumes/JohnDovey/tmp/
./tools/build-windows-server.sh  # Windows amd64 .exe → /Volumes/JohnDovey/tmp/
./tools/build-linux-server.sh    # Linux amd64 (run on Linux; refuses Darwin)
```

## Building the Android app

```sh
tools/gomobile-bind-android.sh   # → android/mesh/libs/core.aar
cd android
./android-build.sh :app:assembleDebug    # phone
./android-build.sh :wear:assembleDebug   # Wear OS Hold-to-Talk (0.2.0)
```

Requires the Android SDK/NDK at `$ANDROID_HOME` (see `.cursor/rules/dev-environment.mdc`) and `libopus`/`libopusfile` installed on the build machine (`brew install opus opusfile` on macOS) for the desktop server's audio codec.

## Versioning

Each app is versioned independently via a `VERSION` file in its own directory (`server/VERSION`, `android/VERSION`, `android/wear/VERSION`, `ios/VERSION`, `meshbridge/VERSION`, …) using Major.Minor.Patch: patch for a bug fix, minor for a new feature (including completing a plan phase), major reserved for actual releases.
