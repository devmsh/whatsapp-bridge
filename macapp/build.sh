#!/usr/bin/env bash
# Builds "WhatsApp Bridge.app" — a native macOS shell around the existing Go
# bridge and its React UI.
#
#   ./macapp/build.sh              build + install to ~/Applications
#   ./macapp/build.sh --web        rebuild web/dist first
#   ./macapp/build.sh --no-install leave the .app in macapp/build/
#
# Requires only the Command Line Tools (swiftc, codesign, iconutil) — no Xcode.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MACAPP="$REPO/macapp"
BUILD="$MACAPP/build"
APP="$BUILD/WhatsApp Bridge.app"
GO="${GO:-$HOME/.local/go/bin/go}"
BUNDLE_ID="com.devmsh.whatsapp-bridge"

REBUILD_WEB=0
INSTALL=1
for arg in "$@"; do
  case "$arg" in
    --web) REBUILD_WEB=1 ;;
    --no-install) INSTALL=0 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

say() { printf '\033[1;32m==>\033[0m %s\n' "$1"; }

# ---------------------------------------------------------------- web UI ----
# Note: `pnpm build` is skipped by default — the committed web/dist is used
# as-is. Pass --web to regenerate it (vite is invoked directly because pnpm
# does not run esbuild's install script in this repo).
if [ "$REBUILD_WEB" = "1" ]; then
  say "Building web UI"
  (cd "$REPO/web" && npx vite build)
fi

# ------------------------------------------------------------- Go bridge ----
say "Building Go bridge"
(cd "$REPO" && "$GO" build -o "$BUILD/whatsapp-bridge-v2" .)

# ---------------------------------------------------------------- Swift ------
say "Compiling Swift shell"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

swiftc -O \
  -target arm64-apple-macosx13.0 \
  -framework AppKit -framework WebKit -framework UserNotifications -framework ServiceManagement \
  -o "$APP/Contents/MacOS/WhatsAppBridge" \
  "$MACAPP"/Sources/*.swift

# ------------------------------------------------------------------ icon -----
say "Generating icon"
ICONSET="$BUILD/AppIcon.iconset"
rm -rf "$ICONSET"
swift "$MACAPP/make-icon.swift" "$ICONSET" >/dev/null
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"

# ---------------------------------------------------------------- bundle -----
say "Assembling bundle"
cp "$BUILD/whatsapp-bridge-v2" "$APP/Contents/MacOS/whatsapp-bridge-v2"

VERSION="$(cd "$REPO" && git rev-list --count HEAD 2>/dev/null || echo 1)"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>WhatsApp Bridge</string>
  <key>CFBundleDisplayName</key><string>WhatsApp Bridge</string>
  <key>CFBundleIdentifier</key><string>$BUNDLE_ID</string>
  <key>CFBundleExecutable</key><string>WhatsAppBridge</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>1.0</string>
  <key>CFBundleVersion</key><string>$VERSION</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>LSUIElement</key><false/>

  <!-- Read by Config.swift; override per-machine with:
       defaults write $BUNDLE_ID BridgeHome -string /path/to/repo -->
  <key>BridgeHome</key><string>$REPO</string>
  <key>BridgePort</key><integer>8082</integer>
  <key>BridgePATH</key><string>$HOME/.local/bin:/opt/homebrew/opt/node@22/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/bin:/bin:/usr/sbin:/sbin</string>

  <!-- The bridge talks to 127.0.0.1 over plain HTTP. -->
  <key>NSAppTransportSecurity</key>
  <dict>
    <key>NSAllowsLocalNetworking</key><true/>
  </dict>
</dict>
</plist>
PLIST

# ------------------------------------------------------------------ sign -----
# Ad-hoc is enough for this Mac and is what UNUserNotificationCenter needs.
# App Sandbox is deliberately NOT enabled: the bridge shells out to node,
# codex, whisper-cli and ffmpeg.
say "Signing (ad-hoc)"
codesign --force --deep --sign - --identifier "$BUNDLE_ID" "$APP"
codesign --verify --deep --strict "$APP" && echo "signature OK"

# --------------------------------------------------------------- install -----
if [ "$INSTALL" = "1" ]; then
  say "Installing to ~/Applications"
  mkdir -p "$HOME/Applications"
  rm -rf "$HOME/Applications/WhatsApp Bridge.app"
  cp -R "$APP" "$HOME/Applications/"
  echo
  echo "Installed: ~/Applications/WhatsApp Bridge.app"
  echo "Launch it with:  open -a '$HOME/Applications/WhatsApp Bridge.app'"
  echo "(Launch via Finder or 'open' — running the binary directly disables notifications.)"
else
  echo
  echo "Built: $APP"
fi
