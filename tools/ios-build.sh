#!/bin/zsh
# tools/ios-build.sh
# Bind Go core + Opus (if missing), regenerate XcodeGen project, build for iphoneos.
set -e
cd "$(dirname "$0")/.."
if [[ -f ~/source-john-dovey.sh ]]; then
    source ~/source-john-dovey.sh --quiet
fi
source tools/go-env.sh

if [[ ! -f ios/Frameworks/Core.xcframework/Info.plist ]]; then
    ./tools/gomobile-bind-ios.sh
fi
if [[ ! -f ios/ThirdParty/Opus.xcframework/Info.plist ]]; then
    ./tools/build-opus-ios.sh
fi

(cd ios && xcodegen generate)

DERIVED="${JOHN_DOVEY:-/Volumes/JohnDovey}/tmp/walkietalkie-ios-derived"
mkdir -p "$DERIVED"

echo "📱 Building WalkieTalkie (iphoneos)…"
(
    cd ios
    xcodebuild -scheme WalkieTalkie -sdk iphoneos \
        -destination 'generic/platform=iOS' \
        -derivedDataPath "$DERIVED" \
        CODE_SIGNING_ALLOWED=NO \
        ONLY_ACTIVE_ARCH=NO \
        build
)

echo "✅ iOS build OK"
