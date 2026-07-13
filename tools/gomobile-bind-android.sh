#!/bin/zsh
# tools/gomobile-bind-android.sh
#
# Builds core/mobile (and core/media, whose AudioSource/AudioSink
# interfaces core/mobile's StartNode exposes) into an Android AAR at
# android/app/libs/core.aar.
#
# Two non-obvious flags are required, both discovered the hard way:
#
#   -ldflags="-checklinkname=0"
#     pion's transitive dependency github.com/wlynxg/anet uses //go:linkname
#     into net.zoneCache for Android network-interface enumeration. Go 1.23+
#     restricts linkname by default, producing:
#       link: github.com/wlynxg/anet: invalid reference to net.zoneCache
#     anet's own README documents this exact flag as the fix.
#
#   binding ./mobile AND ./media together (not just ./mobile)
#     gomobile bind only generates Java bindings for exported types it's
#     explicitly told about. core/mobile.StartNode takes media.AudioSource/
#     AudioSink parameters — without also passing ./media, gobind silently
#     drops StartNode entirely (no error, just missing from the output)
#     rather than generating the media.AudioSource/AudioSink Java
#     interfaces those parameters need.
#
# Usage:
#   ./tools/gomobile-bind-android.sh

set -e

cd "$(dirname "$0")/.."
source tools/go-env.sh
if [[ -f ~/source-john-dovey.sh ]]; then
    source ~/source-john-dovey.sh --quiet
fi

export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$ANDROID_HOME/ndk/26.1.10909125}"
export PATH="$GOPATH/bin:$PATH"

if ! command -v gomobile >/dev/null; then
    echo "installing gomobile/gobind into \$GOPATH/bin..."
    (cd core && go install golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind)
fi

mkdir -p android/app/libs

echo "🤖 Binding core/mobile + core/media -> android/app/libs/core.aar"
(
    cd core
    gomobile bind -target=android -androidapi 26 \
        -ldflags="-checklinkname=0" \
        -o ../android/app/libs/core.aar \
        ./mobile ./media
)

echo "✅ Wrote android/app/libs/core.aar"
