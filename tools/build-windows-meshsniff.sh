#!/bin/zsh
# tools/build-windows-meshsniff.sh
set -e
cd "$(dirname "$0")/.."
source tools/go-env.sh
if [[ -f ~/source-john-dovey.sh ]]; then
    source ~/source-john-dovey.sh --quiet
fi

OUT_DIR="${1:-/Volumes/JohnDovey/tmp}"
mkdir -p "$OUT_DIR"
VER="$(tr -d '[:space:]' < meshsniff/VERSION)"
out="$OUT_DIR/walkietalkie-meshsniff-${VER}-windows-amd64.exe"
echo "Building meshsniff (windows/amd64) -> $out"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$out" ./meshsniff/cmd/meshsniff
ls -lh "$out"
