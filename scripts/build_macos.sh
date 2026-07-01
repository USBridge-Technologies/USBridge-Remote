#!/bin/bash
# Build USBridgeClient for macOS as a self-contained .app bundle.
# All required Homebrew dylibs are copied into Contents/Frameworks/ so the
# app runs on machines without Homebrew installed.
#
# Requirements:
#   Required:  Go, Xcode Command Line Tools
#   Optional:  GStreamer (Homebrew) — bundled automatically if installed;
#              needed only for legacy RTP video mode.
#              Moonlight streaming uses VideoToolbox + CoreAudio (no GStreamer required).

set -e

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=> Building Moonlight Core..."
"$SCRIPTS_DIR/build_moonlight.sh" || { echo "❌ Failed to build Moonlight Core"; exit 1; }

REPO_ROOT="$(cd "$SCRIPTS_DIR/.." && pwd)"
cd "$REPO_ROOT"

if [ -z "${USBRIDGE_LOGGING_ACTIVE:-}" ]; then
    export USBRIDGE_LOGGING_ACTIVE=1
    LOG_DIR="$REPO_ROOT/logs"
    mkdir -p "$LOG_DIR"
    LOG_FILE="$LOG_DIR/$(basename "$0" .sh).log"
    exec > >(tee -a "$LOG_FILE") 2>&1
    echo "=== $(date '+%Y-%m-%d %H:%M:%S') [$0] ==="
fi

OUTPUT_NAME="USBridgeClient"
DIST_ROOT="$REPO_ROOT/dist"
DIST_OS="$DIST_ROOT/macos"
if [ -d "$DIST_OS" ] && [ ! -w "$DIST_OS" ]; then
    echo -e "${RED}❌ Нет прав на запись в $DIST_OS${NC}"
    echo "   Исправьте права и повторите:"
    echo "   sudo chown -R \"$USER\":\"$USER\" \"$DIST_OS\""
    exit 1
fi
DIST_DIR="$DIST_OS"
APP_BUNDLE_NAME="${OUTPUT_NAME}.app"
APP_CONTENTS_DIR="$DIST_DIR/$APP_BUNDLE_NAME/Contents"
APP_MACOS_DIR="$APP_CONTENTS_DIR/MacOS"
APP_RESOURCES_DIR="$APP_CONTENTS_DIR/Resources"
APP_FRAMEWORKS_DIR="$APP_CONTENTS_DIR/Frameworks"
BINARY_NAME="$OUTPUT_NAME"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# --------------------------------------------------------------------------
# Dylib bundling — mirrors what build_windows.sh does for DLLs
# --------------------------------------------------------------------------

# Returns 0 (true) when a dylib path is a macOS system library that must NOT
# be bundled (it is always present on the target machine).
is_system_dylib() {
    case "$1" in
        /usr/lib/*|/System/Library/*) return 0 ;;
    esac
    return 1
}

# Given an absolute dylib path (which may be a Cellar path or an opt symlink
# path), return a concrete file path that actually exists on this build machine.
resolve_dylib() {
    local path="$1"
    [ -f "$path" ] && echo "$path" && return
    local name
    name="$(basename "$path")"
    local d
    for d in \
        /opt/homebrew/opt/*/lib \
        /opt/homebrew/lib \
        /usr/local/opt/*/lib \
        /usr/local/lib; do
        [ -f "$d/$name" ] && echo "$d/$name" && return
    done
}

# bundle_homebrew_dylibs <binary> <frameworks_dir>
#
# BFS walk of all non-system dylib dependencies starting from <binary>.
# Each found dylib is:
#   1. Copied to <frameworks_dir>/
#   2. Its own install name is rewritten to @rpath/<basename>
#   3. All references inside it (and inside <binary>) are fixed to @rpath/<basename>
#
# bash 3.2 compatible (no associative arrays).
bundle_homebrew_dylibs() {
    local binary="$1"
    local frameworks_dir="$2"
    mkdir -p "$frameworks_dir"

    # BFS: files whose transitive deps we still need to walk
    local queue=("$binary")
    local idx=0
    # Track visited basenames to avoid re-processing the same lib
    local visited=()
    local bundle_count=0

    while [ "$idx" -lt "${#queue[@]}" ]; do
        local file="${queue[$idx]}"
        idx=$((idx + 1))
        [ -f "$file" ] || continue

        # otool -L: line 1 is the file itself; subsequent lines are deps
        while IFS= read -r dep; do
            [ -z "$dep" ] && continue
            is_system_dylib "$dep" && continue
            local name
            name="$(basename "$dep")"

            # Skip if already visited
            local seen=0
            local v
            for v in "${visited[@]+"${visited[@]}"}"; do
                [ "$v" = "$name" ] && seen=1 && break
            done
            [ "$seen" = "1" ] && continue
            visited+=("$name")

            local src
            src="$(resolve_dylib "$dep")" || true
            if [ -z "$src" ]; then
                echo -e "   ${YELLOW}⚠${NC} Cannot resolve: $dep"
                continue
            fi

            local dest="$frameworks_dir/$name"
            if [ ! -f "$dest" ]; then
                cp -L "$src" "$dest"
                chmod 755 "$dest"
                # Rewrite the dylib's own install name so it can be found via @rpath
                install_name_tool -id "@rpath/$name" "$dest" 2>/dev/null || true
                bundle_count=$((bundle_count + 1))
                echo -e "   ${GREEN}✓${NC} Frameworks/$name"
                # Walk this dylib's own deps on the next iteration
                queue+=("$dest")
            fi
        done < <(otool -L "$file" 2>/dev/null | awk 'NR>1 {print $1}')
    done

    echo -e "${GREEN}✓${NC} Bundled $bundle_count Homebrew dylibs"

    # Phase 2: rewrite all load-command paths in the binary and every bundled dylib
    # from their absolute Homebrew paths to @rpath/<basename>.
    local all_files=("$binary")
    local nm
    for nm in "${visited[@]+"${visited[@]}"}"; do
        local f="$frameworks_dir/$nm"
        [ -f "$f" ] && all_files+=("$f")
    done

    local fixfile
    for fixfile in "${all_files[@]}"; do
        [ -f "$fixfile" ] || continue
        while IFS= read -r dep; do
            [ -z "$dep" ] && continue
            is_system_dylib "$dep" && continue
            local dep_name
            dep_name="$(basename "$dep")"
            [ "$dep" = "@rpath/$dep_name" ] && continue
            install_name_tool -change "$dep" "@rpath/$dep_name" "$fixfile" 2>/dev/null || true
        done < <(otool -L "$fixfile" 2>/dev/null | awk 'NR>1 {print $1}')
    done
}

# --------------------------------------------------------------------------

create_app_icon() {
    local icon_png="$1"
    local icon_icns="$2"
    local iconset_dir

    if ! command -v sips >/dev/null 2>&1 || ! command -v iconutil >/dev/null 2>&1; then
        return
    fi

    iconset_dir="$(mktemp -d)/AppIcon.iconset"
    mkdir -p "$iconset_dir"
    sips -z 16 16     "$icon_png" --out "$iconset_dir/icon_16x16.png"       >/dev/null 2>&1
    sips -z 32 32     "$icon_png" --out "$iconset_dir/icon_16x16@2x.png"    >/dev/null 2>&1
    sips -z 32 32     "$icon_png" --out "$iconset_dir/icon_32x32.png"       >/dev/null 2>&1
    sips -z 64 64     "$icon_png" --out "$iconset_dir/icon_32x32@2x.png"    >/dev/null 2>&1
    sips -z 128 128   "$icon_png" --out "$iconset_dir/icon_128x128.png"     >/dev/null 2>&1
    sips -z 256 256   "$icon_png" --out "$iconset_dir/icon_128x128@2x.png"  >/dev/null 2>&1
    sips -z 256 256   "$icon_png" --out "$iconset_dir/icon_256x256.png"     >/dev/null 2>&1
    sips -z 512 512   "$icon_png" --out "$iconset_dir/icon_256x256@2x.png"  >/dev/null 2>&1
    sips -z 512 512   "$icon_png" --out "$iconset_dir/icon_512x512.png"     >/dev/null 2>&1
    sips -z 1024 1024 "$icon_png" --out "$iconset_dir/icon_512x512@2x.png"  >/dev/null 2>&1

    iconutil -c icns "$iconset_dir" -o "$icon_icns" >/dev/null 2>&1 || true
    rm -rf "$(dirname "$iconset_dir")"
}

echo -e "${GREEN}🍎 Building USBridgeClient for macOS${NC}"

# 1. Check optional GStreamer (needed only for legacy RTP video mode)
echo -e "\n${YELLOW}📦 Проверка зависимостей...${NC}"

GST_LAUNCH=""
for p in "gst-launch-1.0" "/opt/homebrew/bin/gst-launch-1.0" "/usr/local/bin/gst-launch-1.0"; do
    if command -v "$p" &>/dev/null || [ -x "$p" ]; then
        GST_LAUNCH="$p"
        break
    fi
done

if [ -z "$GST_LAUNCH" ]; then
    echo -e "${YELLOW}⚠${NC}  GStreamer не найден — Moonlight работает без него (VideoToolbox/CoreAudio)."
    echo "   Для RTP видео-режима установите: brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad"
else
    echo -e "   ${GREEN}✓${NC} gst-launch: $GST_LAUNCH"
fi

# 2. Build binary
echo -e "\n${YELLOW}🔨 Компиляция .app...${NC}"
rm -f "$REPO_ROOT/cmd/fyne_metadata_init.go"
rm -rf "$REPO_ROOT/cmd/$APP_BUNDLE_NAME"

HOMEBREW_PREFIX="/opt/homebrew"
[ -d "/usr/local/lib" ] && [ ! -d "/opt/homebrew/lib" ] && HOMEBREW_PREFIX="/usr/local"
export CGO_ENABLED=1
export CGO_LDFLAGS="-L${HOMEBREW_PREFIX}/lib ${CGO_LDFLAGS:-}"
export CGO_CPPFLAGS="-I${HOMEBREW_PREFIX}/include ${CGO_CPPFLAGS:-}"
export CGO_CFLAGS="${CGO_CFLAGS:-} -Wno-format-security"
rm -rf "$DIST_DIR"
mkdir -p "$APP_MACOS_DIR" "$APP_RESOURCES_DIR" "$APP_FRAMEWORKS_DIR"
APP_BINARY_PATH="$APP_MACOS_DIR/$BINARY_NAME"
go build -ldflags="-s -w" -o "$APP_BINARY_PATH" ./cmd

# Tell the dynamic linker to look in Contents/Frameworks/ for bundled dylibs.
# This must come before bundle_homebrew_dylibs rewrites the load commands.
install_name_tool -add_rpath "@executable_path/../Frameworks" "$APP_BINARY_PATH" 2>/dev/null || true

chmod +x "$APP_BINARY_PATH"

# 3. Bundle all Homebrew dylibs the binary depends on (recursive)
echo -e "\n${YELLOW}📚 Bundling Homebrew dylibs → Contents/Frameworks/...${NC}"
bundle_homebrew_dylibs "$APP_BINARY_PATH" "$APP_FRAMEWORKS_DIR"

# 4. Bundle GStreamer plugins (optional — only needed for legacy RTP video mode)
GST_PLUGIN_SRC=""
for d in \
    /opt/homebrew/lib/gstreamer-1.0 \
    /opt/homebrew/opt/gstreamer/lib/gstreamer-1.0 \
    /usr/local/lib/gstreamer-1.0; do
    [ -d "$d" ] && GST_PLUGIN_SRC="$d" && break
done

if [ -n "$GST_PLUGIN_SRC" ]; then
    echo -e "\n${YELLOW}🔌 Bundling GStreamer plugins (RTP mode) → Contents/Frameworks/gstreamer-1.0/...${NC}"
    GST_PLUGIN_DEST="$APP_FRAMEWORKS_DIR/gstreamer-1.0"
    mkdir -p "$GST_PLUGIN_DEST"
    GST_PLUGINS=(
        libgstcoreelements.dylib
        libgstapp.dylib
        libgstrtp.dylib
        libgstrtpmanager.dylib
        libgstudp.dylib
        libgstvideoconvertscale.dylib
        libgstvideofilter.dylib
        libgstplayback.dylib
        libgsttypefindfunctions.dylib
        libgstjpeg.dylib
        libgstjpegformat.dylib
        libgstlibav.dylib
        libgstautodetect.dylib
        libgstaudioconvert.dylib
        libgstaudioresample.dylib
        libgstosxaudio.dylib
        libgstapplemedia.dylib
        libgstvideoparsers.dylib
        libgstvideoparsersbad.dylib
    )
    GST_PLUGIN_COUNT=0
    for plugin in "${GST_PLUGINS[@]}"; do
        if [ -f "$GST_PLUGIN_SRC/$plugin" ]; then
            cp -L "$GST_PLUGIN_SRC/$plugin" "$GST_PLUGIN_DEST/"
            chmod 755 "$GST_PLUGIN_DEST/$plugin"
            install_name_tool -id "@rpath/gstreamer-1.0/$plugin" "$GST_PLUGIN_DEST/$plugin" 2>/dev/null || true
            # Fix all Homebrew references inside the plugin
            while IFS= read -r dep; do
                [ -z "$dep" ] && continue
                is_system_dylib "$dep" && continue
                dep_name="$(basename "$dep")"
                install_name_tool -change "$dep" "@rpath/$dep_name" "$GST_PLUGIN_DEST/$plugin" 2>/dev/null || true
            done < <(otool -L "$GST_PLUGIN_SRC/$plugin" 2>/dev/null | awk 'NR>1 {print $1}')
            echo -e "   ${GREEN}✓${NC} gstreamer-1.0/$plugin"
            GST_PLUGIN_COUNT=$((GST_PLUGIN_COUNT + 1))
        fi
    done
    echo -e "${GREEN}✓${NC} GStreamer plugins: $GST_PLUGIN_COUNT bundled"
    echo "   (set GST_PLUGIN_PATH=\$BUNDLE/Contents/Frameworks/gstreamer-1.0 to use them)"
fi

# 5. Bundle QEMU (qemu-nbd, qemu-img — for VMDK/QCOW2/VDI image support)
echo -e "\n${YELLOW}📀 Bundling QEMU (qemu-nbd, qemu-img)...${NC}"
QEMU_COPIED=0
for _qtool in qemu-nbd qemu-img; do
    _src=""
    for _d in /opt/homebrew/bin /usr/local/bin; do
        [ -f "$_d/$_qtool" ] && _src="$_d/$_qtool" && break
    done
    if [ -n "$_src" ]; then
        _dest="$APP_MACOS_DIR/$_qtool"
        cp -L "$_src" "$_dest"
        chmod 755 "$_dest"
        # Allow it to find dylibs in the shared Frameworks/ dir
        install_name_tool -add_rpath "@executable_path/../Frameworks" "$_dest" 2>/dev/null || true
        # Bundle any new dylib deps it introduces (gnutls, libssh, zstd…) alongside
        # the ones already copied for the main binary — bundle_homebrew_dylibs skips
        # libs that are already present in Frameworks/.
        bundle_homebrew_dylibs "$_dest" "$APP_FRAMEWORKS_DIR"
        QEMU_COPIED=$((QEMU_COPIED + 1))
        echo -e "   ${GREEN}✓${NC} MacOS/$_qtool"
    else
        echo -e "   ${YELLOW}⚠${NC} $_qtool не найден — установите: brew install qemu"
    fi
done
[ "$QEMU_COPIED" -gt 0 ] && echo -e "${GREEN}✓${NC} QEMU: $QEMU_COPIED бинарников скопировано" || true

# 5b. Bundle Tailscale (Go binary — statically linked, no dylib deps)
echo -e "\n${YELLOW}🔗 Bundling Tailscale...${NC}"
_ts_src=""
for _d in /opt/homebrew/bin /usr/local/bin; do
    [ -f "$_d/tailscale" ] && _ts_src="$_d/tailscale" && break
done
if [ -n "$_ts_src" ]; then
    cp -L "$_ts_src" "$APP_MACOS_DIR/tailscale"
    chmod 755 "$APP_MACOS_DIR/tailscale"
    echo -e "${GREEN}✓${NC} MacOS/tailscale"
else
    echo -e "${YELLOW}⚠${NC} tailscale не найден — установите: brew install tailscale"
fi

# 6. Info.plist (written after bundling so icons/plist don't interfere with lib walk)
cat > "$APP_CONTENTS_DIR/Info.plist" << 'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>en</string>
    <key>CFBundleDisplayName</key>
    <string>USBridgeClient</string>
    <key>CFBundleExecutable</key>
    <string>USBridgeClient</string>
    <key>CFBundleIdentifier</key>
    <string>com.usbridge.client</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>USBridgeClient</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0.0</string>
    <key>CFBundleVersion</key>
    <string>1.0.0</string>
    <key>LSMinimumSystemVersion</key>
    <string>10.15</string>
    <key>NSCameraUsageDescription</key>
    <string>USBridgeClient uses the camera to scan QR codes for device connection.</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST

if [ -f "$REPO_ROOT/Icon.png" ]; then
    create_app_icon "$REPO_ROOT/Icon.png" "$APP_RESOURCES_DIR/AppIcon.icns"
    if [ -f "$APP_RESOURCES_DIR/AppIcon.icns" ]; then
        /usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string AppIcon" "$APP_CONTENTS_DIR/Info.plist" >/dev/null 2>&1 || true
    fi
fi

# 6. Sign — must run after all dylibs are copied into Frameworks/.
# codesign --deep does NOT sign flat .dylib files, only nested .framework bundles.
# After install_name_tool modifies the copied dylibs their original signatures are
# invalid, so we must explicitly sign each .dylib before signing the bundle.
if command -v codesign >/dev/null 2>&1; then
    find "$APP_FRAMEWORKS_DIR" -name "*.dylib" -type f | while IFS= read -r dylib; do
        codesign --force --sign - "$dylib" 2>/dev/null || true
    done
    codesign --force --deep --sign - "$DIST_DIR/$APP_BUNDLE_NAME" >/dev/null 2>&1 || true
fi
touch "$DIST_DIR/$APP_BUNDLE_NAME"

echo -e "${GREEN}   ✅ App bundle: $DIST_DIR/$APP_BUNDLE_NAME${NC}"

# 7. Dist extras
echo -e "\n${YELLOW}📁 Подготовка dist...${NC}"
[ -f config.yaml ] && cp config.yaml "$DIST_DIR/"

cat > "$DIST_DIR/README.txt" << 'README'
USBridgeClient for macOS
=========================

Run:
  Open USBridgeClient.app
  (All Homebrew dependencies are bundled — Homebrew not required on the target machine.)

Bundle layout:
  Contents/MacOS/USBridgeClient    — main binary
  Contents/MacOS/qemu-nbd          — QEMU NBD (VMDK/QCOW2/VDI image support)
  Contents/MacOS/qemu-img          — QEMU image tool
  Contents/MacOS/tailscale         — Tailscale CLI
  Contents/Frameworks/             — bundled dylibs (opus, openssl, glib, gstreamer, gnutls…)
  Contents/Frameworks/gstreamer-1.0/  — GStreamer plugins (if bundled)

Video modes:
  Moonlight streaming — VideoToolbox (GPU hardware decode) + CoreAudio audio.
    No external dependencies required.

  Legacy RTP mode — requires GStreamer plugins.
    If GStreamer was installed at build time its plugins are bundled above.
    To activate: export GST_PLUGIN_PATH="$BUNDLE/Contents/Frameworks/gstreamer-1.0"
    Or install GStreamer via Homebrew: brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad

Requirements:
  - macOS 10.15+

Configuration:
  config.yaml next to the .app, or ~/.config/usbridge-client/

Application log:
  ~/Library/Logs/USBridgeClient/app.log
README

echo -e "\n${YELLOW}📦 Создание архива...${NC}"
cd "$DIST_DIR"
zip -rq "../USBridgeClient-macOS.zip" ./*
cd "$REPO_ROOT"

echo -e "\n${GREEN}✅ Сборка завершена!${NC}"
echo -e "   Результат: $DIST_DIR/$APP_BUNDLE_NAME"
echo -e "   Архив:     dist/USBridgeClient-macOS.zip"
echo -e "   Запуск:    open \"$DIST_DIR/$APP_BUNDLE_NAME\""
echo -e "   Лог app:   ~/Library/Logs/USBridgeClient/app.log"
echo ""
