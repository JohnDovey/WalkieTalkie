# WalkieTalkie v1.8.0 — Installation

Cross-platform LAN push-to-talk. Open source: https://github.com/JohnDovey/WalkieTalkie

**This release:** Base Station `1.8.0` · Android phone `1.6.0` · Wear OS `0.2.0` · iOS source `0.8.0` (build from Xcode; no IPA in this zip).

## What's in this folder

| Path | What |
|------|------|
| `binaries/` | Ready-to-run Base Station servers + Android APKs |
| `manual/` | User guide (PDF, EPUB, DOCX) |
| `docs/` | Design / phase notes (developers) |
| `INSTALL.md` | This file |
| `README.md` | Project overview |
| `SHA256SUMS.txt` | Checksums for every binary and manual export |

## Quick start (recommended)

1. **Start a Base Station** on a Mac or Windows PC on your Wi-Fi (see below).
2. Open **http://\<that-machine\>:9091** in a browser for the dashboard.
3. Install the **Android APK** on phone(s) on the same Wi-Fi.
4. Grant mic / location / nearby-devices permissions when asked.
5. Press and hold **Hold to Talk** — peers on the LAN hear you live.

There is **no login**. Anyone on the LAN who can reach port **9091** can see the dashboard (including GPS). Keep it on a trusted network; do not port-forward it to the internet.

## Base Station (desktop)

### macOS

| Binary | Use when |
|--------|----------|
| `binaries/walkietalkie-server-1.8.0-darwin-arm64` | Apple Silicon (M1/M2/M3/…) |
| `binaries/walkietalkie-server-1.8.0-darwin-amd64` | Intel Mac |
| `binaries/walkietalkie-server-1.8.0-darwin-universal` | Either (larger) |

```sh
chmod +x binaries/walkietalkie-server-1.8.0-darwin-universal
./binaries/walkietalkie-server-1.8.0-darwin-universal
```

Then open http://localhost:9091

If macOS quarantines a downloaded binary: **System Settings → Privacy & Security**, or:

```sh
xattr -dr com.apple.quarantine binaries/walkietalkie-server-1.8.0-darwin-*
```

### Windows

Run `binaries/walkietalkie-server-1.8.0-windows-amd64.exe` (x86_64). Allow the firewall prompt for private networks. Open http://localhost:9091

### Linux

No prebuilt Linux binary in this zip (builds need a Linux host with libopus). See `README.md` → `./tools/build-linux-server.sh` on a Linux machine.

### Useful flags (testing)

- `--port 9092` — listen on another port
- `--data-dir /path/to/data` — separate registry/DB
- `--name "Base Station: Kitchen"` — display name
- `--no-audio` — skip mic/speaker (two instances on one PC)
- `--no-tray` — skip system tray

## Android phone (`1.6.0`)

File: `binaries/walkietalkie-android-1.6.0-debug.apk` (**debug-signed** for sideload testing).

1. Enable “Install unknown apps” for your file manager / `adb`.
2. Install the APK, or: `adb install -r binaries/walkietalkie-android-1.6.0-debug.apk`
3. Disable battery optimization for WalkieTalkie on aggressive OEMs (e.g. Xiaomi HyperOS), or the mesh may die after ~10s.

## Wear OS (`0.2.0`)

File: `binaries/walkietalkie-wear-0.2.0-debug.apk`

Sideload onto a Wear OS watch on the same Wi-Fi as a Base Station (or use as a companion Hold-to-Talk surface). See Manual → Wearables.

## iPhone (`0.8.0`)

Not packaged as an IPA (requires your Apple Developer Team ID). From a clone of the repo:

```sh
./tools/gomobile-bind-ios.sh
./tools/build-opus-ios.sh
cd ios && xcodegen generate
# copy Config/Local.xcconfig.example → Local.xcconfig, set DEVELOPMENT_TEAM
xcodebuild -scheme WalkieTalkie -sdk iphoneos build
```

Details: Manual → The iPhone app, and `docs/2026-07-14-ios-phase4.md`.

## New in this release (summary)

- **GPS history** trails on each Base Station (`GET /api/devices/{id}/gps-history`)
- **N-party private channels** (invite more peers; clips fan out to all members)
- **SFU voice-note DataChannel** (Direct → SFU → HTTP send order)
- **Multi-Base clock skew** soft floor + HTTP Date / `/api/time` correction
- Phase 6 private live Talk, Hub rooms, Hub→direct bridges (carried from `1.6`–`1.7`)

Full notes: `docs/2026-07-14-gps-history-nparty-sfu-notes.md` and Manual PDF/EPUB.

**After this zip (on `main`):** optional **MeshBridge** companion (`meshbridge/` **0.1.1**) syncs devices + voice notes across dual LAN / punch into the Base Station **Remote Users** tab — not live Talk. See Manual → MeshBridge and `docs/2026-07-14-meshbridge-plan.md`. Not included in the `v1.8.0` binary zip.


## Docs included

- `manual/walkie-talkie-mesh-chat.pdf` / `.epub` / `.docx` — end-user guide  
- `docs/` — implementation plan, Phase 5/6, voice notes design, this release note  

## Verify downloads

```sh
shasum -a 256 -c SHA256SUMS.txt
```

## Support / source

https://github.com/JohnDovey/WalkieTalkie — issues and source trees under `Manual/`, `docs/`, `tools/`.
