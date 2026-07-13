#!/bin/zsh
# tools/go-env.sh
#
# Redirects Go's GOPATH/GOCACHE/TMPDIR to project-scoped directories on the
# JohnDovey volume, per this repo's volume-storage convention (see
# .cursor/rules/volume-storage.mdc). Source this before running `go`/`gomobile`
# commands for this project — it does not touch the global `go env`.
#
# Usage:
#   source tools/go-env.sh

WT_VOLUME="/Volumes/JohnDovey"

if [[ -d "$WT_VOLUME" ]]; then
    export GOPATH="$WT_VOLUME/tmp/walkietalkie-gopath"
    export GOCACHE="$WT_VOLUME/tmp/walkietalkie-gocache"
    export GOMODCACHE="$GOPATH/pkg/mod"
    export TMPDIR="$WT_VOLUME/tmp/walkietalkie-scratch"
    mkdir -p "$GOPATH" "$GOCACHE" "$TMPDIR"
else
    echo "WalkieTalkie: $WT_VOLUME not mounted, falling back to default Go env" >&2
fi
