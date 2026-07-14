#!/bin/zsh
# tools/gomobile-bind-ios.sh
#
# Builds core/mobile + core/media into an iOS/iOS-simulator XCFramework at
# ios/Frameworks/Core.xcframework.
#
# Same non-obvious requirements as tools/gomobile-bind-android.sh:
#
#   -ldflags="-checklinkname=0"
#     pion's wlynxg/anet uses //go:linkname into net.zoneCache (Go 1.23+).
#
#   binding ./mobile AND ./media together
#     gomobile only generates bindings for packages it is given; StartNode
#     takes media.AudioSource/AudioSink — without ./media those symbols are
#     silently omitted from the ObjC/Swift API.
#
# Usage:
#   ./tools/gomobile-bind-ios.sh
#
# Requires: Xcode (DEVELOPER_DIR from source-john-dovey.sh), gomobile, and an
# accepted Xcode license on this machine.

set -e

cd "$(dirname "$0")/.."
source tools/go-env.sh
if [[ -f ~/source-john-dovey.sh ]]; then
    source ~/source-john-dovey.sh --quiet
fi

export PATH="$GOPATH/bin:$PATH"

# Go 1.26+ requires golang.org/x/mobile in the module graph (tool directive)
# or gomobile bind refuses to run. Keep it via go get -tool if missing.
(
    cd core
    if ! go list -m golang.org/x/mobile >/dev/null 2>&1; then
        echo "adding golang.org/x/mobile as a module tool dependency..."
        go get -tool golang.org/x/mobile/cmd/gobind@latest
    fi
    if ! command -v gomobile >/dev/null || ! go list -m golang.org/x/mobile >/dev/null 2>&1; then
        echo "installing gomobile/gobind into \$GOPATH/bin..."
        go install golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind
    fi
)

if ! command -v xcodebuild >/dev/null; then
    echo "xcodebuild not found — source source-john-dovey.sh and ensure Xcode is installed" >&2
    exit 1
fi

mkdir -p ios/Frameworks
OUT="$(pwd)/ios/Frameworks/Core.xcframework"
rm -rf "$OUT"

echo "🍎 Binding core/mobile + core/media -> ios/Frameworks/Core.xcframework"
(
    cd core
    # ios,iossimulator produces a universal XCFramework for device + sim.
    gomobile bind -target=ios,iossimulator \
        -ldflags="-checklinkname=0" \
        -o "$OUT" \
        ./mobile ./media
)

echo "✅ Wrote $OUT"
ls -la "$OUT"
