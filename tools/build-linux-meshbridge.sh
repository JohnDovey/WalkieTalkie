#!/bin/zsh
# tools/build-linux-meshbridge.sh
# Builds MeshBridge for Linux amd64 (prefer native Linux; cross from Darwin ok — no cgo).
set -e
cd "$(dirname "$0")/.."
source tools/go-env.sh
OUT_DIR="${1:-/Volumes/JohnDovey/tmp}"
mkdir -p "$OUT_DIR"
VER="$(tr -d '[:space:]' < meshbridge/VERSION)"
OUT="$OUT_DIR/walkietalkie-meshbridge-${VER}-linux-amd64"
echo "🐧 Building meshbridge -> $OUT"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$OUT" ./meshbridge/cmd/meshbridge
ls -lh "$OUT"
