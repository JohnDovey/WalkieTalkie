# WalkieTalkie

A cross-platform push-to-talk (PTT) app: press a button, talk, and be heard live on every other device running the app — Android, desktop (Mac/Windows/Linux), iPhone, and eventually Android Wear/Apple Watch — regardless of platform. Devices auto-discover each other over the LAN (mDNS) or, off-LAN, via Bluetooth LE presence detection, with no manual pairing.

See [`docs/2026-07-13-implementation-plan.md`](docs/2026-07-13-implementation-plan.md) for the full design and phased build plan, and [`Manual/`](Manual/) for the end-user manual.

## Status

Build priority is Android first, then desktop, then iPhone, then wearables last — see the plan doc for why.

- **Phase 1 — shared Go core + desktop server**: ✅ done and verified.
- **Phase 2 — Android**: ✅ working on real hardware (live WebRTC Opus PTT, mDNS + BLE presence, GPS, voice notes / private channels via Base Station).
- **Phase 3 (desktop hardening + multi-Base-Station registry sync)**: ✅ done (registry sync, map, Old Nodes, Windows/macOS/Linux packaging scripts, system tray, Base Station mesh SFU / relay threshold). Three-OS hardware mesh not run on this Mac-only setup.
- **Phase 4 (iPhone)**: 🟡 in progress — SwiftUI shell + bind/Opus + voice notes/private channels (`0.3.0` adds live private Talk); `iphoneos` build verified. Device mesh / locked-screen PTT needs Team ID + hardware. See `docs/2026-07-14-ios-phase4.md`.
- **Phase 5 (wearables)**: 🟡 software complete — Wear OS `0.2.0` + watchOS WatchConnectivity relay; hardware verify pending. See `docs/2026-07-14-phase5-wearables.md`.
- **Phase 6 (private live Talk)**: 🟡 in progress — live unicast (mesh/SFU), multi-Base voice sync, P2P voice notes when DirectConnected. See `docs/2026-07-14-phase6-private-live-talk.md`.

**Current release track:** server `1.5.0`, android `1.3.0`, wear `0.2.0`, ios `0.5.0`.

## Repo layout

```
core/      shared Go module (registry, discovery, WebRTC mesh, signaling) — no cgo, gomobile-bound into Android/iOS
server/    the Go desktop app AND the "Base Station" server: bbolt registry, REST API, Bootstrap/jQuery dashboard
android/   Kotlin/Compose phone + Wear OS apps; shared `:mesh` library consumes core via gomobile AAR
ios/       SwiftUI iPhone app (+ WatchConnectivity watch stub); Core XCFramework (see docs/2026-07-14-ios-phase4.md)
tools/     dev scripts: Go env setup, gomobile→Android AAR / iOS XCFramework, Opus iOS, Windows/macOS/Linux server builds
docs/      plans and design docs (including voice messages / private channels)
Manual/    the end-user manual (.ebhtml format — see Manual/README.md)
```

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
./android-build.sh :wear:assembleDebug   # Wear OS Hold-to-Talk (0.1.0)
```

Requires the Android SDK/NDK at `$ANDROID_HOME` (see `.cursor/rules/dev-environment.mdc`) and `libopus`/`libopusfile` installed on the build machine (`brew install opus opusfile` on macOS) for the desktop server's audio codec.

## Versioning

Each app is versioned independently via a `VERSION` file in its own directory (`server/VERSION`, `android/VERSION`, `android/wear/VERSION`, `ios/VERSION`, …) using Major.Minor.Patch: patch for a bug fix, minor for a new feature (including completing a plan phase), major reserved for actual releases.
