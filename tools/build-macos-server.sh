#!/bin/zsh
# tools/build-macos-server.sh
#
# Builds server/ into macOS binaries with full audio support (mic capture +
# speaker playback) for both Apple Silicon (arm64) and Intel (amd64).
#
# Why not a plain `go build`: server/audio uses cgo (hraban/opus via
# pkg-config, malgo/miniaudio). Homebrew's opus is only for the host
# Homebrew prefix (arm64 under /opt/homebrew on Apple Silicon, or x86_64
# under /usr/local on Intel), so a cross-arch cgo link needs matching
# libraries. This script builds opus/libogg/opusfile from Xiph source for
# each target arch via clang -arch, caches them under /Volumes/JohnDovey/tmp,
# then produces both binaries (and a universal lipo binary).
#
# Usage:
#   ./tools/build-macos-server.sh [output-dir]
#   (default output-dir: /Volumes/JohnDovey/tmp)
#
# Writes:
#   walkietalkie-server-darwin-arm64
#   walkietalkie-server-darwin-amd64
#   walkietalkie-server-darwin-universal

set -e

cd "$(dirname "$0")/.."
source tools/go-env.sh
if [[ -f ~/source-john-dovey.sh ]]; then
    source ~/source-john-dovey.sh --quiet
fi

OUT_DIR="${1:-/Volumes/JohnDovey/tmp}"
mkdir -p "$OUT_DIR"

if ! command -v clang >/dev/null; then
    echo "clang not found — install Xcode / Command Line Tools (or source source-john-dovey.sh)" >&2
    exit 1
fi
if ! command -v pkg-config >/dev/null; then
    echo "🔧 Installing pkg-config via Homebrew..."
    brew install pkg-config
fi
if ! command -v lipo >/dev/null; then
    echo "lipo not found — need Xcode / Command Line Tools" >&2
    exit 1
fi

fetch_and_verify() {
    local url="$1" sha="$2" out="$3"
    if [[ -f "$out" ]] && echo "$sha  $out" | shasum -a 256 -c - >/dev/null 2>&1; then
        return 0
    fi
    curl -fsSL -o "$out" "$url"
    echo "$sha  $out" | shasum -a 256 -c -
}

# Build static opus/libogg/opusfile for one macOS arch into
# /Volumes/JohnDovey/tmp/opus-darwin-<arch>-build/install (once per arch).
# Only the install-dir path is printed on stdout (for $(...)); progress goes
# to stderr so callers don't capture make noise into PKG_CONFIG_PATH.
ensure_opus_for_arch() {
    local goarch="$1"   # arm64 | amd64
    local clang_arch="$2" # arm64 | x86_64
    local triple="$3"   # aarch64-apple-darwin | x86_64-apple-darwin

    local cross_root="/Volumes/JohnDovey/tmp/opus-darwin-${goarch}-build"
    local install_dir="$cross_root/install"

    if [[ -f "$install_dir/lib/libopusfile.a" ]]; then
        echo "$install_dir"
        return 0
    fi

    echo "🔧 Building opus/libogg/opusfile for darwin/${goarch} (clang -arch ${clang_arch}, one-time)..." >&2
    mkdir -p "$cross_root"
    (
        cd "$cross_root"

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

        local cc="clang -arch ${clang_arch}"
        local jobs
        jobs="$(sysctl -n hw.ncpu)"

        (cd opus-1.6.1 && ./configure --host="$triple" --prefix="$install_dir" \
            --enable-static --disable-shared --disable-doc --disable-extra-programs \
            CC="$cc" CFLAGS="-arch ${clang_arch}" LDFLAGS="-arch ${clang_arch}" && \
            make -j"$jobs" && make install)

        export PKG_CONFIG_PATH="$install_dir/lib/pkgconfig"
        (cd libogg-1.3.6 && ./configure --host="$triple" --prefix="$install_dir" \
            --enable-static --disable-shared \
            CC="$cc" CFLAGS="-arch ${clang_arch}" LDFLAGS="-arch ${clang_arch}" && \
            make -j"$jobs" && make install)

        # --disable-http: avoid openssl; we only need opusfile to link (hraban/opus).
        (cd opusfile-0.12 && ./configure --host="$triple" --prefix="$install_dir" \
            --enable-static --disable-shared --disable-http --disable-doc \
            CC="$cc" CFLAGS="-arch ${clang_arch}" LDFLAGS="-arch ${clang_arch}" && \
            make -j"$jobs" && make install)
    ) >&2

    echo "$install_dir"
}

# opusfile.pc lists ogg/opus under Requires.private — plain pkg-config --libs
# omits those; cgo doesn't pass --static, so force it via PKG_CONFIG.
setup_pkg_config_static() {
    local cross_root="$1"
    mkdir -p "$cross_root/bin"
    local wrapper="$cross_root/bin/pkg-config-static"
    cat > "$wrapper" <<'PKGEOF'
#!/bin/sh
exec pkg-config --static "$@"
PKGEOF
    chmod +x "$wrapper"
    echo "$wrapper"
}

build_one() {
    local goarch="$1"
    local clang_arch="$2"
    local triple="$3"
    local out="$4"

    local install_dir
    install_dir="$(ensure_opus_for_arch "$goarch" "$clang_arch" "$triple")"
    local cross_root="/Volumes/JohnDovey/tmp/opus-darwin-${goarch}-build"
    local pkg_config
    pkg_config="$(setup_pkg_config_static "$cross_root")"

    echo "🍎 Building server (darwin/${goarch}) -> $out"
    (
        cd server
        export PKG_CONFIG_PATH="$install_dir/lib/pkgconfig"
        export PKG_CONFIG_LIBDIR="$install_dir/lib/pkgconfig"
        export PKG_CONFIG="$pkg_config"
        # -a: force full rebuild so cgo doesn't reuse a stale flag cache from
        # another arch (same gotcha as tools/build-windows-server.sh).
        GOOS=darwin GOARCH="$goarch" CGO_ENABLED=1 \
            CC="clang" \
            CGO_CFLAGS="-arch ${clang_arch}" \
            CGO_LDFLAGS="-arch ${clang_arch}" \
            go build -a -o "$out" .
    )
    file "$out"
}

OUT_ARM64="$OUT_DIR/walkietalkie-server-darwin-arm64"
OUT_AMD64="$OUT_DIR/walkietalkie-server-darwin-amd64"
OUT_UNIVERSAL="$OUT_DIR/walkietalkie-server-darwin-universal"

build_one arm64 arm64 aarch64-apple-darwin "$OUT_ARM64"
build_one amd64 x86_64 x86_64-apple-darwin "$OUT_AMD64"

echo "🔗 Creating universal binary -> $OUT_UNIVERSAL"
lipo -create -output "$OUT_UNIVERSAL" "$OUT_ARM64" "$OUT_AMD64"
file "$OUT_UNIVERSAL"

echo "✅ Wrote:"
echo "   $OUT_ARM64"
echo "   $OUT_AMD64"
echo "   $OUT_UNIVERSAL"
