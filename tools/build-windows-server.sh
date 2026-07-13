#!/bin/zsh
# tools/build-windows-server.sh
#
# Cross-compiles server/ into a Windows x86_64 .exe from this Mac, with
# full audio support (mic capture + speaker playback) — not just the
# no-audio registry/web-UI/relay path.
#
# Why this is nontrivial: server/audio uses cgo (gopkg.in/hraban/opus.v2
# for Opus encode/decode, github.com/gen2brain/malgo for mic/speaker via
# miniaudio). malgo/miniaudio cross-compiles fine with just a C compiler
# for the target (mingw-w64), but hraban/opus links against libopus via
# `#cgo pkg-config: opus`, which needs an actual Windows-targeted libopus
# build, not the macOS one Homebrew's `opus` formula provides. Its
# stream.go/stream_errors.go files also require opusfile (`#cgo pkg-config:
# opusfile`) purely to compile as part of the same Go package — we never
# use opusfile's actual OggOpusFile functionality, Go just needs the whole
# imported package to link — which in turn needs libogg. All three (opus,
# libogg, opusfile) are built here from their official Xiph.org source
# tarballs via the mingw-w64 cross-compiler, cached under
# /Volumes/JohnDovey/tmp so this only happens once per machine.
#
# Usage:
#   ./tools/build-windows-server.sh [output-path]
#   (default output: /Volumes/JohnDovey/tmp/walkietalkie-server-windows-amd64.exe)

set -e

cd "$(dirname "$0")/.."
source tools/go-env.sh
if [[ -f ~/source-john-dovey.sh ]]; then
    source ~/source-john-dovey.sh --quiet
fi

OUT="${1:-/Volumes/JohnDovey/tmp/walkietalkie-server-windows-amd64.exe}"
CROSS_ROOT="/Volumes/JohnDovey/tmp/opus-mingw-build"
INSTALL_DIR="$CROSS_ROOT/install"

if ! command -v x86_64-w64-mingw32-gcc >/dev/null; then
    echo "🔧 Installing mingw-w64 (Windows cross-compiler) via Homebrew..."
    brew install mingw-w64
fi

fetch_and_verify() {
    local url="$1" sha="$2" out="$3"
    if [[ -f "$out" ]] && echo "$sha  $out" | shasum -a 256 -c - >/dev/null 2>&1; then
        return 0
    fi
    curl -fsSL -o "$out" "$url"
    echo "$sha  $out" | shasum -a 256 -c -
}

if [[ ! -f "$INSTALL_DIR/lib/libopusfile.a" ]]; then
    echo "🔧 Cross-compiling opus/libogg/opusfile for x86_64-w64-mingw32 (one-time, a few minutes)..."
    mkdir -p "$CROSS_ROOT"
    cd "$CROSS_ROOT"

    fetch_and_verify https://ftp.osuosl.org/pub/xiph/releases/opus/opus-1.6.1.tar.gz \
        6ffcb593207be92584df15b32466ed64bbec99109f007c82205f0194572411a1 opus-1.6.1.tar.gz
    fetch_and_verify https://ftp.osuosl.org/pub/xiph/releases/ogg/libogg-1.3.6.tar.gz \
        83e6704730683d004d20e21b8f7f55dcb3383cdf84c0daedf30bde175f774638 libogg-1.3.6.tar.gz
    fetch_and_verify https://ftp.osuosl.org/pub/xiph/releases/opus/opusfile-0.12.tar.gz \
        118d8601c12dd6a44f52423e68ca9083cc9f2bfe72da7a8c1acb22a80ae3550b opusfile-0.12.tar.gz

    rm -rf opus-1.6.1 libogg-1.3.6 opusfile-0.12
    tar xzf opus-1.6.1.tar.gz
    tar xzf libogg-1.3.6.tar.gz
    tar xzf opusfile-0.12.tar.gz

    (cd opus-1.6.1 && ./configure --host=x86_64-w64-mingw32 --prefix="$INSTALL_DIR" \
        --enable-static --disable-shared --disable-doc --disable-extra-programs \
        CC=x86_64-w64-mingw32-gcc && make -j"$(sysctl -n hw.ncpu)" && make install)

    export PKG_CONFIG_PATH="$INSTALL_DIR/lib/pkgconfig"
    (cd libogg-1.3.6 && ./configure --host=x86_64-w64-mingw32 --prefix="$INSTALL_DIR" \
        --enable-static --disable-shared CC=x86_64-w64-mingw32-gcc && \
        make -j"$(sysctl -n hw.ncpu)" && make install)

    # --disable-http avoids needing openssl; we never use opusfile's URL
    # support, just need the package to link (see header comment above).
    (cd opusfile-0.12 && ./configure --host=x86_64-w64-mingw32 --prefix="$INSTALL_DIR" \
        --enable-static --disable-shared --disable-http --disable-doc \
        CC=x86_64-w64-mingw32-gcc && make -j"$(sysctl -n hw.ncpu)" && make install)
fi

# opusfile.pc lists ogg/opus under Requires.private (correct, since they're
# implementation details of the static lib) — but plain `pkg-config --libs`
# omits Requires.private entries; only `--static` includes the full
# transitive static link line. cgo doesn't pass --static itself, so force
# it via a wrapper and the PKG_CONFIG env var cgo respects.
mkdir -p "$CROSS_ROOT/bin"
PKG_CONFIG_WRAPPER="$CROSS_ROOT/bin/pkg-config-static"
cat > "$PKG_CONFIG_WRAPPER" <<'PKGEOF'
#!/bin/sh
exec pkg-config --static "$@"
PKGEOF
chmod +x "$PKG_CONFIG_WRAPPER"

echo "🪟 Cross-compiling server -> $OUT"
cd server
export PKG_CONFIG_PATH="$INSTALL_DIR/lib/pkgconfig"
export PKG_CONFIG_LIBDIR="$INSTALL_DIR/lib/pkgconfig"
export PKG_CONFIG="$PKG_CONFIG_WRAPPER"
# -a: force a full rebuild rather than reusing Go's build cache. Found the
# hard way: a non-forced rebuild after fixing the PKG_CONFIG env var above
# silently kept relinking against a stale (incomplete) flag resolution
# cached from an earlier attempt, reproducing the exact "undefined
# reference to ogg_*" link failure that -a fixed.
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
    go build -a -o "$OUT" .

echo "✅ Wrote $OUT"
