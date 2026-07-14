#!/bin/zsh
# tools/build-macos-meshbridge.sh
#
# Builds meshbridge/ for darwin arm64 + amd64 + universal (no cgo).
#
# Usage:
#   ./tools/build-macos-meshbridge.sh [output-dir]
#   (default: /Volumes/JohnDovey/tmp)

set -e
cd "$(dirname "$0")/.."
source tools/go-env.sh
if [[ -f ~/source-john-dovey.sh ]]; then
    source ~/source-john-dovey.sh --quiet
fi

OUT_DIR="${1:-/Volumes/JohnDovey/tmp}"
mkdir -p "$OUT_DIR"
VER="$(tr -d '[:space:]' < meshbridge/VERSION)"

build_one() {
    local goarch="$1"
    local out="$OUT_DIR/walkietalkie-meshbridge-${VER}-darwin-${goarch}"
    echo "🍎 Building meshbridge (darwin/${goarch}) -> $out"
    GOOS=darwin GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$out" ./meshbridge/cmd/meshbridge
}

build_one arm64
build_one amd64
lipo -create -output "$OUT_DIR/walkietalkie-meshbridge-${VER}-darwin-universal" \
    "$OUT_DIR/walkietalkie-meshbridge-${VER}-darwin-arm64" \
    "$OUT_DIR/walkietalkie-meshbridge-${VER}-darwin-amd64"
echo "✅ universal -> $OUT_DIR/walkietalkie-meshbridge-${VER}-darwin-universal"
ls -lh "$OUT_DIR"/walkietalkie-meshbridge-${VER}-darwin-*
