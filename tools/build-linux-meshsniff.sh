#!/usr/bin/env bash
# tools/build-linux-meshsniff.sh — run on Linux.
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck disable=SC1091
source tools/go-env.sh

if [[ "$(uname -s)" == "Darwin" ]]; then
  echo "build-linux-meshsniff.sh must run on Linux (CGO-free but intended for Linux targets)" >&2
  exit 1
fi

OUT_DIR="${1:-/Volumes/JohnDovey/tmp}"
mkdir -p "$OUT_DIR"
VER="$(tr -d '[:space:]' < meshsniff/VERSION)"
out="$OUT_DIR/walkietalkie-meshsniff-${VER}-linux-amd64"
echo "Building meshsniff (linux/amd64) -> $out"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$out" ./meshsniff/cmd/meshsniff
ls -lh "$out"
