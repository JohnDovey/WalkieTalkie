# Phase 5 — Wearables

Started 2026-07-14. First slice: **Wear OS** standalone mesh participant (embeds the same `core.aar` as the phone) + **watchOS** WatchConnectivity relay stub.

## Status

**In progress** — Wear OS `:wear` + shared `:mesh` library; watchOS WatchConnectivity relay stub under `ios/WalkieTalkieWatch/`.

Software builds verified 2026-07-14:
- `./android-build.sh :mesh:assembleDebug :app:assembleDebug :wear:assembleDebug` ✅
- `xcodegen generate` + `xcodebuild` (iphoneos + embedded Watch, `CODE_SIGNING_ALLOWED=NO`) ✅

## Decisions

### Wear OS
- Wear OS is a full Android runtime → reuse `android/mesh/libs/core.aar` via shared `:mesh` library.
- Platform string passed to Go: `wear`.
- Watch app id: `com.walkietalkie.wear` (separate install from phone; both can join the LAN mesh over Wi‑Fi).
- First UI: Hold to Talk + short device status (peer count / Base Station). GPS/BLE reuse phone mesh helpers where hardware permits.
- Shared Kotlin lives under `android/mesh/` (audio, PTT service, BLE, location, nickname) so phone and watch don’t diverge.
- Phone and wear each set `MeshIdentity` in their `Application` (`android` / `wear` + `VERSION`).

### watchOS (research spike)
- **Confirmed direction:** do **not** embed `Core.xcframework` on the Watch. `gomobile bind -target=ios` does not produce a watchOS slice; pion/WebRTC + mDNS on watchOS would be a poor fit anyway (battery, networking).
- **Watch role:** thin UI over **WatchConnectivity** to the paired iPhone:
  - Watch → phone: `startTalking` / `stopTalking` (and later VM stubs).
  - Phone → watch: device list snippet / talking indicator / errors.
- Phone keeps owning `Mobile.startNode`, Opus codec, BLE, GPS.
- Scaffold: `ios/WalkieTalkieWatch/` + phone `WatchConnectivityBridge.swift` (regenerate Xcodeproj via `xcodegen`).

## Build

```bash
./tools/gomobile-bind-android.sh   # → android/mesh/libs/core.aar
cd android
./android-build.sh :wear:assembleDebug
./android-build.sh :app:assembleDebug
```

Wear VERSION: `android/wear/VERSION` → `0.1.0`.

```bash
cd ios && xcodegen generate
# Watch target embeds with the iPhone app; needs Team ID for device.
```

## Verify (needs hardware)
- Wear OS watch on Wi‑Fi joins Base Station mesh; Hold to Talk works watch ↔ desktop/phone.
- Apple Watch: Talk relay via paired iPhone (Team ID + devices).

## Non-goals this slice
- App Store / Play wear listing polish
- Full Chats/VM UI on the watch face
- Independent watchOS mesh without iPhone
- Play Services Wearable nickname sync between phone and Wear OS (both use local NicknameStore for now)
