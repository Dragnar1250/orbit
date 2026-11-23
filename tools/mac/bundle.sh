#!/bin/bash
# Builds Orbit.app for macOS (Apple Silicon + Intel in one), a zip and a disk image. Run on a Mac:
#   VERSION=1.18.0 tools/mac/bundle.sh   → dist/Orbit.app, dist/Orbit-mac.zip, dist/Orbit-mac.dmg
set -euo pipefail
cd "$(dirname "$0")/../.."
VERSION=${VERSION:-$(grep -oE 'const appVersion = "[^"]+"' config.go | cut -d'"' -f2)}
rm -rf dist/Orbit.app && mkdir -p dist
export CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=12.0
GOOS=darwin GOARCH=arm64 CC="clang -arch arm64" go build -trimpath -ldflags "-s -w" -o dist/orbit-arm64 .
GOOS=darwin GOARCH=amd64 CC="clang -arch x86_64" go build -trimpath -ldflags "-s -w" -o dist/orbit-amd64 .
APP=dist/Orbit.app
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
lipo -create -output "$APP/Contents/MacOS/Orbit" dist/orbit-arm64 dist/orbit-amd64
rm dist/orbit-arm64 dist/orbit-amd64
cp assets/menubar.png "$APP/Contents/Resources/menubar.png"
ICON=dist/Orbit.iconset && rm -rf "$ICON" && mkdir "$ICON"
for s in 16 32 128 256 512; do
  sips -z $s $s assets/icon-1024.png --out "$ICON/icon_${s}x${s}.png" >/dev/null
  sips -z $((s * 2)) $((s * 2)) assets/icon-1024.png --out "$ICON/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICON" -o "$APP/Contents/Resources/Orbit.icns" && rm -rf "$ICON"
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>Orbit</string>
  <key>CFBundleDisplayName</key><string>Orbit</string>
  <key>CFBundleIdentifier</key><string>com.dragnar1250.orbit</string>
  <key>CFBundleExecutable</key><string>Orbit</string>
  <key>CFBundleIconFile</key><string>Orbit</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${VERSION}</string>
  <key>CFBundleVersion</key><string>${VERSION}</string>
  <key>LSMinimumSystemVersion</key><string>12.0</string>
  <key>LSUIElement</key><true/>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSAppleEventsUsageDescription</key><string>Orbit reads which song Spotify or Apple Music is playing to show its lyrics, and can play, pause and skip from its menu.</string>
</dict></plist>
PLIST
codesign --force --deep --sign - "$APP"   # ad-hoc signature (no Apple developer account)
codesign --verify --verbose=2 "$APP"
(cd dist && ditto -c -k --keepParent Orbit.app Orbit-mac.zip)
# a disk image too: it keeps the app intact even when it is passed around through Windows,
# WhatsApp or Discord (a zip opened and zipped again on Windows loses what makes the app runnable)
rm -rf dist/dmg dist/Orbit-mac.dmg && mkdir dist/dmg
cp -R "$APP" dist/dmg/ && ln -s /Applications dist/dmg/Applications
hdiutil create -volname "Orbit" -srcfolder dist/dmg -ov -format UDZO dist/Orbit-mac.dmg >/dev/null
rm -rf dist/dmg
echo "built $APP ($VERSION): $(lipo -archs "$APP/Contents/MacOS/Orbit")"
