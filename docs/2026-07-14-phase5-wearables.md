# Phase 5 — Wearables

Started 2026-07-14. Wear OS standalone mesh participant + watchOS WatchConnectivity relay.

## Status

**In progress** — Wear OS `0.2.0` (Talk / Settings / About pager); watchOS relay stub with phone→watch status push.

Software builds verified:
- `./android-build.sh :mesh:assembleDebug :app:assembleDebug :wear:assembleDebug` ✅
- `xcodegen generate` + `xcodebuild` (iphoneos + embedded Watch, `CODE_SIGNING_ALLOWED=NO`) ✅

## Decisions

### Wear OS
- Wear OS is a full Android runtime → reuse `android/mesh/libs/core.aar` via shared `:mesh` library.
- Platform string passed to Go: `wear`.
- Watch app id: `com.walkietalkie.wear` (separate install from phone; both can join the LAN mesh over Wi‑Fi).
- UI: horizontal pager — Hold to Talk, nickname Settings, About (tappable Base Station URL + mesh blurb).
- Shared Kotlin lives under `android/mesh/` (audio, PTT service, BLE, location, nickname).
- Phone and wear each set `MeshIdentity` in their `Application` (`android` / `wear` + `VERSION`).

### watchOS (research spike)
- **Confirmed direction:** do **not** embed `Core.xcframework` on the Watch.
- **Watch role:** thin UI over **WatchConnectivity** to the paired iPhone (`startTalking` / `stopTalking`).
- Phone pushes status on the mesh poll timer (`WatchConnectivityBridge.pushStatusToWatch`).
- Scaffold: `ios/WalkieTalkieWatch/` + phone `WatchConnectivityBridge.swift`.

## Build

```bash
./tools/gomobile-bind-android.sh   # → android/mesh/libs/core.aar
cd android
./android-build.sh :wear:assembleDebug
./android-build.sh :app:assembleDebug
```

Wear VERSION: `android/wear/VERSION` → `0.2.0`.

```bash
cd ios && xcodegen generate
```

## Verify (needs hardware)
- Wear OS watch on Wi‑Fi joins Base Station mesh; Hold to Talk works watch ↔ desktop/phone.
- Apple Watch: Talk relay via paired iPhone (Team ID + devices).

## Non-goals this slice
- App Store / Play wear listing polish
- Full Chats/VM UI on the watch face
- Independent watchOS mesh without iPhone
- Play Services Wearable nickname sync between phone and Wear OS (both use local NicknameStore for now)
