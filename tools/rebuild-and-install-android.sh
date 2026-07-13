#!/bin/zsh
# tools/rebuild-and-install-android.sh
#
# One-shot dev convenience: rebuild the shared Go core into core.aar, then
# build and install the Android debug APK on a connected device. Run this
# whenever core/ has changed and you want to test on real hardware.
#
# Usage:
#   ./tools/rebuild-and-install-android.sh

set -e

cd "$(dirname "$0")/.."

tools/gomobile-bind-android.sh

cd android
./android-build.sh assembleDebug installDebug
