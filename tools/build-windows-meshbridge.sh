#!/bin/zsh
# tools/build-windows-meshbridge.sh
# Cross-compiles MeshBridge for Windows amd64 (CGO-free).
set -e
cd "$(dirname "$0")/.."
source tools/go-env.sh
OUT_DIR="${1:-/Volumes/JohnDovey/tmp}"
mkdir -p "$OUT_DIR"
VER="$(tr -d '[:space:]' < meshbridge/VERSION)"
OUT="$OUT_DIR/walkietalkie-meshbridge-${VER}-windows-amd64.exe"
echo "🪟 Building meshbridge -> $OUT"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$OUT" ./meshbridge/cmd/meshbridge
ls -lh "$OUT"
