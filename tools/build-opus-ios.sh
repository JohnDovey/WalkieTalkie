#!/bin/zsh
# tools/build-opus-ios.sh
#
# Builds libopus as an XCFramework for iOS device + simulator and installs it
# at ios/ThirdParty/Opus.xcframework (gitignored — regenerate before Xcode).
#
# Cache/build root: /Volumes/JohnDovey/tmp/walkietalkie-opus-ios (volume-scoped).
#
# Usage:
#   ./tools/build-opus-ios.sh

set -e

cd "$(dirname "$0")/.."
if [[ -f ~/source-john-dovey.sh ]]; then
    source ~/source-john-dovey.sh --quiet
fi

OPUS_VER="${OPUS_VER:-1.5.2}"
ROOT="${JOHN_DOVEY:-/Volumes/JohnDovey}/tmp/walkietalkie-opus-ios"
SRC="$ROOT/opus-$OPUS_VER"
OUT_FW="$(pwd)/ios/ThirdParty/Opus.xcframework"
TARBALL_URL="https://downloads.xiph.org/releases/opus/opus-${OPUS_VER}.tar.gz"

mkdir -p "$ROOT" ios/ThirdParty

if [[ ! -d "$SRC" ]]; then
    echo "⬇️  Fetching opus $OPUS_VER..."
    curl -fsSL "$TARBALL_URL" | tar -xz -C "$ROOT"
fi

build_slice() {
    local sdk="$1"   # iphoneos | iphonesimulator
    local archs="$2" # e.g. arm64  or  "arm64 x86_64"
    local dest="$ROOT/install-$sdk"
    local builddir="$ROOT/build-$sdk"

    if [[ -f "$dest/lib/libopus.a" ]]; then
        echo "✅ Reusing $dest/lib/libopus.a"
        return
    fi

    rm -rf "$builddir" "$dest"
    mkdir -p "$builddir" "$dest"
    local sdkroot
    sdkroot="$(xcrun --sdk "$sdk" --show-sdk-path)"
    local minver=16.0
    local cflags="-arch ${archs// / -arch } -isysroot $sdkroot"
    if [[ "$sdk" == "iphoneos" ]]; then
        cflags="$cflags -miphoneos-version-min=$minver"
    else
        cflags="$cflags -mios-simulator-version-min=$minver"
    fi

    (
        cd "$builddir"
        "$SRC/configure" \
            --disable-shared --enable-static \
            --disable-doc --disable-extra-programs \
            --disable-asm \
            --host=arm-apple-darwin \
            --prefix="$dest" \
            CC="$(xcrun --sdk "$sdk" -f clang)" \
            CFLAGS="$cflags" \
            LDFLAGS="$cflags"
        make -j"$(sysctl -n hw.ncpu)"
        make install
    )
    echo "✅ Installed opus for $sdk -> $dest"
}

# Device: arm64 only
build_slice iphoneos arm64
# Simulator: arm64 (Apple Silicon) + x86_64 (Intel)
# Build each arch separately then lipo — configure multi-arch is fragile.
build_sim_arch() {
    local arch="$1"
    local dest="$ROOT/install-iphonesimulator-$arch"
    local builddir="$ROOT/build-iphonesimulator-$arch"
    if [[ -f "$dest/lib/libopus.a" ]]; then
        echo "✅ Reusing $dest/lib/libopus.a"
        return
    fi
    rm -rf "$builddir" "$dest"
    mkdir -p "$builddir" "$dest"
    local sdkroot
    sdkroot="$(xcrun --sdk iphonesimulator --show-sdk-path)"
    local cflags="-arch $arch -isysroot $sdkroot -mios-simulator-version-min=16.0"
    (
        cd "$builddir"
        "$SRC/configure" \
            --disable-shared --enable-static \
            --disable-doc --disable-extra-programs \
            --disable-asm \
            --host="${arch}-apple-darwin" \
            --prefix="$dest" \
            CC="$(xcrun --sdk iphonesimulator -f clang)" \
            CFLAGS="$cflags" \
            LDFLAGS="$cflags"
        make -j"$(sysctl -n hw.ncpu)"
        make install
    )
}

build_sim_arch arm64
build_sim_arch x86_64

SIM_UNI="$ROOT/install-iphonesimulator"
mkdir -p "$SIM_UNI/lib" "$SIM_UNI/include"
cp -R "$ROOT/install-iphonesimulator-arm64/include/"* "$SIM_UNI/include/" 2>/dev/null || \
  cp -R "$ROOT/install-iphoneos/include/"* "$SIM_UNI/include/"
lipo -create \
    "$ROOT/install-iphonesimulator-arm64/lib/libopus.a" \
    "$ROOT/install-iphonesimulator-x86_64/lib/libopus.a" \
    -output "$SIM_UNI/lib/libopus.a"

rm -rf "$OUT_FW"
xcodebuild -create-xcframework \
    -library "$ROOT/install-iphoneos/lib/libopus.a" \
    -headers "$ROOT/install-iphoneos/include" \
    -library "$SIM_UNI/lib/libopus.a" \
    -headers "$SIM_UNI/include" \
    -output "$OUT_FW"

# Flat include path for the OpusCodec.c bridging sources (opus/opus.h).
mkdir -p ios/ThirdParty/include
rm -rf ios/ThirdParty/include/opus
cp -R "$ROOT/install-iphoneos/include/opus" ios/ThirdParty/include/opus

echo "✅ Wrote $OUT_FW"
