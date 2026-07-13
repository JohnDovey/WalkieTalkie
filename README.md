# WalkieTalkie

A cross-platform push-to-talk (PTT) app: press a button, talk, and be heard live on every other device running the app — Android, desktop (Mac/Windows/Linux), iPhone, and eventually Android Wear/Apple Watch — regardless of platform. Devices auto-discover each other over the LAN (mDNS) or, off-LAN, via Bluetooth LE presence detection, with no manual pairing.

See [`docs/2026-07-13-implementation-plan.md`](docs/2026-07-13-implementation-plan.md) for the full design and phased build plan, and [`Manual/`](Manual/) for the end-user manual.

## Status

Build priority is Android first, then desktop, then iPhone, then wearables last — see the plan doc for why.

- **Phase 1 — shared Go core + desktop server**: ✅ done and verified. Two desktop instances on a LAN discover each other via mDNS and hold a real WebRTC voice connection.
- **Phase 2 — Android**: in progress. The Go core is bound into an Android AAR via `gomobile`; the app (Kotlin/Compose), foreground service, BLE presence bridge, and GPS updates are wired up. Real mic/speaker audio (MediaCodec Opus) is not yet implemented — currently using placeholder silent audio so the rest of the pipeline could be verified independently.
- **Phase 3 (desktop hardening + multi-Base-Station registry sync)**, **Phase 4 (iPhone)**, **Phase 5 (wearables)**: not started.

## Repo layout

```
core/      shared Go module (registry, discovery, WebRTC mesh, signaling) — no cgo, gomobile-bound into Android/iOS
server/    the Go desktop app AND the "Base Station" server: bbolt registry, REST API, Bootstrap/jQuery dashboard
android/   Kotlin/Compose Android app, consuming core/ via a gomobile-built AAR
tools/     dev scripts: Go env setup, the gomobile→Android AAR build wrapper
docs/      plans and design docs
Manual/    the end-user manual (.ebhtml format — see Manual/README.md)
```

## Running the desktop server

```sh
source tools/go-env.sh   # redirects GOPATH/GOCACHE off this dev machine's internal disk
cd server
go run .                 # starts the Base Station on http://localhost:9091
```

Open `http://localhost:9091` for the device dashboard, `http://localhost:9091/settings` for server settings (port, etc — no login, by design). Useful flags for running more than one instance on one machine (development only): `--port`, `--data-dir`, `--name`, `--no-audio`.

## Building the Android app

```sh
tools/gomobile-bind-android.sh   # builds core/ -> android/app/libs/core.aar
cd android
./android-build.sh assembleDebug
```

Requires the Android SDK/NDK at `$ANDROID_HOME` (see `.cursor/rules/dev-environment.mdc`) and `libopus`/`libopusfile` installed on the build machine (`brew install opus opusfile` on macOS) for the desktop server's audio codec.

## Versioning

Each app is versioned independently via a `VERSION` file in its own directory (`server/VERSION`, and later `android/VERSION`, `ios/VERSION`, ...) using Major.Minor.Patch: patch for a bug fix, minor for a new feature (including completing a plan phase), major reserved for actual releases.
