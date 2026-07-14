#!/bin/zsh
# tools/build-macos-meshsniff.sh
set -e
cd "$(dirname "$0")/.."
source tools/go-env.sh
if [[ -f ~/source-john-dovey.sh ]]; then
    source ~/source-john-dovey.sh --quiet
fi

OUT_DIR="${1:-/Volumes/JohnDovey/tmp}"
mkdir -p "$OUT_DIR"
VER="$(tr -d '[:space:]' < meshsniff/VERSION)"

build_one() {
    local goarch="$1"
    local out="$OUT_DIR/walkietalkie-meshsniff-${VER}-darwin-${goarch}"
    echo "Building meshsniff (darwin/${goarch}) -> $out"
    GOOS=darwin GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$out" ./meshsniff/cmd/meshsniff
}

build_one arm64
build_one amd64
lipo -create -output "$OUT_DIR/walkietalkie-meshsniff-${VER}-darwin-universal" \
    "$OUT_DIR/walkietalkie-meshsniff-${VER}-darwin-arm64" \
    "$OUT_DIR/walkietalkie-meshsniff-${VER}-darwin-amd64"
echo "universal -> $OUT_DIR/walkietalkie-meshsniff-${VER}-darwin-universal"
ls -lh "$OUT_DIR"/walkietalkie-meshsniff-${VER}-darwin-*
