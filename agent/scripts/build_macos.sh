#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

VERSION="$(tr -d ' \t\n\r' < "$REPO_ROOT/VERSION" 2>/dev/null || echo "0.0.0")"

OUTPUT_NAME="USBridgeAgent"
DIST_DIR="$REPO_ROOT/dist/macos"
APP_BUNDLE="$DIST_DIR/${OUTPUT_NAME}.app"
APP_CONTENTS="$APP_BUNDLE/Contents"
APP_MACOS="$APP_CONTENTS/MacOS"
APP_RESOURCES="$APP_CONTENTS/Resources"
BIN_PATH="$APP_MACOS/$OUTPUT_NAME"
CODESIGN_IDENTITY="${USBRIDGE_CODESIGN_IDENTITY:-}"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# create_dmg_with_drag_layout <volname> <src_dir> <output_dmg> <app_bundle_name>
# Builds a DMG with the classic drag-to-Applications Finder window: app icon
# on the left, an /Applications symlink on the right (src_dir must already
# contain that symlink). Falls back to a plain hdiutil create if the
# Finder/AppleScript step fails (e.g. no GUI session available) so a cosmetic
# layout failure never breaks the build — the DMG still works either way.
create_dmg_with_drag_layout() {
    local volname="$1" src_dir="$2" output_dmg="$3" app_name="$4"
    local tmp_dmg mount_point
    tmp_dmg="$(mktemp -u "${TMPDIR:-/tmp}/${volname}-XXXXXX").dmg"
    mount_point="/Volumes/$volname"

    if hdiutil create -volname "$volname" -srcfolder "$src_dir" -ov -format UDRW -fs HFS+ "$tmp_dmg" >/dev/null 2>&1 \
        && hdiutil attach "$tmp_dmg" -mountpoint "$mount_point" -nobrowse -quiet 2>/dev/null; then
        osascript \
            -e "tell application \"Finder\"" \
            -e "tell disk \"$volname\"" \
            -e "open" \
            -e "set current view of container window to icon view" \
            -e "set toolbar visible of container window to false" \
            -e "set statusbar visible of container window to false" \
            -e "set the bounds of container window to {400, 100, 940, 420}" \
            -e "set viewOptions to the icon view options of container window" \
            -e "set arrangement of viewOptions to not arranged" \
            -e "set icon size of viewOptions to 96" \
            -e "set position of item \"$app_name\" of container window to {130, 150}" \
            -e "set position of item \"Applications\" of container window to {410, 150}" \
            -e "close" \
            -e "open" \
            -e "update without registering applications" \
            -e "delay 1" \
            -e "end tell" \
            -e "end tell" \
            >/dev/null 2>&1 || true
        hdiutil detach "$mount_point" -quiet 2>/dev/null || hdiutil detach "$mount_point" -force -quiet 2>/dev/null || true
        rm -f "$output_dmg"
        if hdiutil convert "$tmp_dmg" -format UDZO -o "$output_dmg" >/dev/null 2>&1; then
            rm -f "$tmp_dmg"
            return 0
        fi
    fi

    echo -e "${YELLOW}Finder drag-layout step failed — falling back to a plain DMG${NC}" >&2
    hdiutil detach "$mount_point" -force -quiet 2>/dev/null || true
    rm -f "$tmp_dmg" "$output_dmg"
    hdiutil create -volname "$volname" -srcfolder "$src_dir" -ov -format UDZO "$output_dmg"
}

if [[ "$(uname -s)" != "Darwin" ]]; then
    echo -e "${RED}Этот скрипт должен запускаться на macOS${NC}"
    exit 1
fi

echo -e "${GREEN}Building usbridge_agent for macOS${NC}"

mkdir -p "$APP_MACOS" "$APP_RESOURCES"
rm -rf "$APP_BUNDLE"
mkdir -p "$APP_MACOS" "$APP_RESOURCES"

export CGO_ENABLED=1

echo -e "${YELLOW}Compiling app bundle...${NC}"
go build -ldflags "-X main.version=$VERSION" -o "$BIN_PATH" ./cmd/usbridge_agent
chmod +x "$BIN_PATH"

# Icon generation
if [ -f "$REPO_ROOT/assets/icons/appicon-256.png" ]; then
    echo -e "${YELLOW}Generating app icon...${NC}"
    ICONSET_DIR="$DIST_DIR/icon.iconset"
    mkdir -p "$ICONSET_DIR"
    
    # Generate standard sizes
    # sips -z height width source --out destination
    sips -z 16 16   "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_16x16.png" > /dev/null 2>&1
    sips -z 32 32   "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_16x16@2x.png" > /dev/null 2>&1
    sips -z 32 32   "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_32x32.png" > /dev/null 2>&1
    sips -z 64 64   "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_32x32@2x.png" > /dev/null 2>&1
    sips -z 128 128 "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_128x128.png" > /dev/null 2>&1
    sips -z 256 256 "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_128x128@2x.png" > /dev/null 2>&1
    sips -z 256 256 "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_256x256.png" > /dev/null 2>&1
    sips -z 512 512 "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_256x256@2x.png" > /dev/null 2>&1
    
    iconutil -c icns "$ICONSET_DIR" -o "$APP_RESOURCES/AppIcon.icns"
    rm -rf "$ICONSET_DIR"
fi

cat > "$APP_CONTENTS/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>en</string>
    <key>CFBundleDisplayName</key>
    <string>USBridgeAgent</string>
    <key>CFBundleExecutable</key>
    <string>USBridgeAgent</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundleIdentifier</key>
    <string>io.usbridge.agent</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>USBridgeAgent</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>__VERSION__</string>
    <key>CFBundleVersion</key>
    <string>__VERSION__</string>
    <key>LSMinimumSystemVersion</key>
    <string>12.3</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>NSCameraUsageDescription</key>
    <string>USBridge needs camera access so a connected Moonlight client can stream your camera as a video source.</string>
</dict>
</plist>
PLIST
sed -i '' "s/__VERSION__/$VERSION/g" "$APP_CONTENTS/Info.plist"

# Bundle Sunshine before signing — adding files after signing breaks the seal.
source "$SCRIPT_DIR/fetch_sunshine.sh"
fetch_sunshine_macos "$APP_MACOS/sunshine"

# Bundle Tailscale CLI (statically linked Go binary — no dylib deps to walk).
# The daemon itself is not bundled: on macOS we rely on a system-installed
# Tailscale (Tailscale.app or `brew install tailscale` + tailscaled service),
# same as internal/tailscale/service_darwin.go already assumes.
echo -e "${YELLOW}Bundling Tailscale CLI...${NC}"
TS_SRC=""
for _d in /opt/homebrew/bin /usr/local/bin; do
    [ -f "$_d/tailscale" ] && TS_SRC="$_d/tailscale" && break
done
if [ -n "$TS_SRC" ]; then
    cp -L "$TS_SRC" "$APP_MACOS/tailscale"
    chmod 755 "$APP_MACOS/tailscale"
    echo -e "${GREEN}✓${NC} MacOS/tailscale"
else
    echo -e "${YELLOW}⚠${NC} tailscale не найден в /opt/homebrew/bin или /usr/local/bin — установите: brew install tailscale"
fi

# Auto-detect Developer ID if not explicitly set
if [ -z "$CODESIGN_IDENTITY" ] && command -v security >/dev/null 2>&1; then
    CODESIGN_IDENTITY=$(security find-identity -v -p codesigning 2>/dev/null \
        | { grep "Developer ID Application" || true; } | head -1 \
        | awk '{print $2}')
fi

ENTITLEMENTS="$SCRIPT_DIR/entitlements-macos.plist"

if [ -n "$CODESIGN_IDENTITY" ] && command -v codesign >/dev/null 2>&1; then
    echo -e "${YELLOW}Signing app bundle with identity: $CODESIGN_IDENTITY${NC}"
    # Sign inner Sunshine.app first (--deep doesn't reach bundles nested in MacOS/)
    SUNSHINE_APP="$APP_MACOS/sunshine/Sunshine.app"
    if [ -d "$SUNSHINE_APP" ]; then
        codesign --force --deep --sign "$CODESIGN_IDENTITY" \
            --options runtime --entitlements "$ENTITLEMENTS" "$SUNSHINE_APP"
    fi
    codesign --force --deep --sign "$CODESIGN_IDENTITY" \
        --options runtime --entitlements "$ENTITLEMENTS" "$APP_BUNDLE"
else
    # Go linker embeds an adhoc linker-signed signature which macOS rejects as
    # "damaged" unless we replace it with a proper codesign call. Use ad-hoc (-).
    echo -e "${YELLOW}No Developer ID found — signing ad-hoc to strip linker-signed flag${NC}"
    SUNSHINE_APP="$APP_MACOS/sunshine/Sunshine.app"
    if [ -d "$SUNSHINE_APP" ]; then
        codesign --force --deep --sign - "$SUNSHINE_APP"
    fi
    codesign --force --deep --sign - "$APP_BUNDLE"
fi

# No README.txt in the DMG — a symlink to /Applications alongside the .app
# gives the standard drag-to-install Finder window instead, which is more
# discoverable than a text file nobody opens.
ln -sf /Applications "$DIST_DIR/Applications"

ARCHIVE="$REPO_ROOT/dist/USBridgeAgent-macOS-arm64-${VERSION}.dmg"
rm -f "$ARCHIVE"
echo -e "${YELLOW}Creating disk image...${NC}"
create_dmg_with_drag_layout "USBridgeAgent" "$DIST_DIR" "$ARCHIVE" "${OUTPUT_NAME}.app"
echo -e "${GREEN}✓${NC} Disk image: $ARCHIVE"

# ── Notarization (optional) ───────────────────────────────────────────────────
# Requires credentials stored in Keychain once via:
#   xcrun notarytool store-credentials "usbridge-notarytool" \
#       --apple-id "..." --team-id "..." --password "xxxx-xxxx-xxxx-xxxx"
# Skip with: USBRIDGE_SKIP_NOTARIZE=1
NOTARIZE_PROFILE="${USBRIDGE_NOTARIZE_PROFILE:-usbridge-notarytool}"
if [[ "${USBRIDGE_SKIP_NOTARIZE:-0}" != "1" ]] && command -v xcrun >/dev/null 2>&1; then
    # Check that the keychain profile actually exists before trying
    if xcrun notarytool history --keychain-profile "$NOTARIZE_PROFILE" >/dev/null 2>&1; then
        echo -e "${YELLOW}Notarizing (this takes 1-3 minutes)...${NC}"
        xcrun notarytool submit "$ARCHIVE" \
            --keychain-profile "$NOTARIZE_PROFILE" \
            --wait
        echo -e "${YELLOW}Stapling notarization ticket...${NC}"
        # Staple the .app itself, not just the .dmg — if the ticket only
        # lives on the disk image, an app dragged out of it to /Applications
        # has no local ticket (Gatekeeper falls back to an online check).
        xcrun stapler staple "$APP_BUNDLE"
        create_dmg_with_drag_layout "USBridgeAgent" "$DIST_DIR" "$ARCHIVE" "${OUTPUT_NAME}.app"
        echo -e "${GREEN}✓${NC} Notarized & stapled: $ARCHIVE"
    else
        echo -e "${YELLOW}Keychain profile '$NOTARIZE_PROFILE' not found — skipping notarization${NC}"
        echo "  Run once to enable: xcrun notarytool store-credentials \"$NOTARIZE_PROFILE\" --apple-id ... --team-id ... --password ..."
    fi
fi

# ── Signature validation ─────────────────────────────────────────────────────
echo -e "${YELLOW}Validating code signatures...${NC}"
sig_ok=true

# codesign deep verify
if codesign --verify --deep --strict "$APP_BUNDLE" 2>&1; then
    echo -e "  ${GREEN}✓${NC} codesign --verify --deep --strict: OK"
else
    echo -e "  ${RED}✗${NC} codesign deep verify FAILED"
    sig_ok=false
fi

# Check all binaries inside MacOS/ are signed
while IFS= read -r -d '' bin; do
    if codesign --verify --strict "$bin" 2>/dev/null; then
        echo -e "  ${GREEN}✓${NC} signed: ${bin#$APP_BUNDLE/}"
    else
        echo -e "  ${RED}✗${NC} unsigned or invalid: ${bin#$APP_BUNDLE/}"
        sig_ok=false
    fi
done < <(find "$APP_BUNDLE/Contents/MacOS" -type f -perm +111 -print0 2>/dev/null)

# Gatekeeper assessment (informational — fails for ad-hoc / unnotarized)
gk_result=$(spctl --assess -v "$APP_BUNDLE" 2>&1 || true)
if echo "$gk_result" | grep -q "accepted"; then
    echo -e "  ${GREEN}✓${NC} Gatekeeper: accepted (notarized)"
elif echo "$gk_result" | grep -q "CSSMERR_TP_NOT_TRUSTED\|rejected"; then
    echo -e "  ${YELLOW}⚠${NC}  Gatekeeper: not accepted (ad-hoc signed / not notarized)"
else
    echo -e "  ${YELLOW}⚠${NC}  Gatekeeper: $gk_result"
fi

if $sig_ok; then
    echo -e "${GREEN}✓${NC} Signature validation passed"
else
    echo -e "${RED}✗${NC} Signature validation found issues — check output above"
fi

echo -e "${GREEN}Done.${NC}"
echo "Bundle: $APP_BUNDLE"
echo "Install/update for stable permissions: $REPO_ROOT/scripts/install_macos.sh"
