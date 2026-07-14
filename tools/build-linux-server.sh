#!/bin/zsh
# tools/build-linux-server.sh
#
# Builds server/ into a Linux amd64 binary with full audio (mic + speaker)
# on a native Linux host. cgo needs Linux libopus / ALSA (or Pulse) headers
# from the build machine — cross-compiling full-audio Linux from macOS is
# not supported (use a Linux box, VM, or container).
#
# Usage (on Linux):
#   ./tools/build-linux-server.sh [output-path]
#   (default: /Volumes/JohnDovey/tmp/walkietalkie-server-linux-amd64
#    or ./walkietalkie-server-linux-amd64 if the volume is unavailable)
#
# Dependencies (Debian/Ubuntu example):
#   sudo apt install build-essential libopus-dev libopusfile-dev pkg-config
#   # plus ALSA/Pulse for malgo at runtime

set -e

cd "$(dirname "$0")/.."
source tools/go-env.sh
if [[ -f ~/source-john-dovey.sh ]]; then
    source ~/source-john-dovey.sh --quiet
fi

if [[ "$(uname -s)" != "Linux" ]]; then
    echo "tools/build-linux-server.sh: full-audio Linux builds require a Linux host." >&2
    echo "This machine is $(uname -s). Use a Linux VM/container, or run this script on Linux." >&2
    echo "Cross-compiling from macOS would produce a silent/broken audio binary — refused." >&2
    exit 1
fi

DEFAULT_OUT="/Volumes/JohnDovey/tmp/walkietalkie-server-linux-amd64"
if [[ ! -d /Volumes/JohnDovey/tmp ]]; then
    DEFAULT_OUT="$(pwd)/walkietalkie-server-linux-amd64"
fi
OUT="${1:-$DEFAULT_OUT}"
mkdir -p "$(dirname "$OUT")"

if ! command -v pkg-config >/dev/null; then
    echo "pkg-config not found — install pkg-config and libopus-dev" >&2
    exit 1
fi
if ! pkg-config --exists opus; then
    echo "libopus not found via pkg-config — install libopus-dev (and libopusfile-dev)" >&2
    exit 1
fi

echo "🐧 Building server (linux/amd64) -> $OUT" >&2
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -a -o "$OUT" ./server
echo "✅ Wrote $OUT" >&2
file "$OUT" >&2
