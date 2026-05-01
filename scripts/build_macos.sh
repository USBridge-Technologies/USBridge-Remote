#!/bin/bash
# Build USBridgeClient for macOS as a native .app bundle
# Requirements: Go, Homebrew GStreamer

set -e

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
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
BINARY_NAME="$OUTPUT_NAME"

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

has_rpath() {
    local binary="$1"
    local rpath="$2"
    otool -l "$binary" 2>/dev/null | grep -Fq "path $rpath "
}

ensure_rpath() {
    local binary="$1"
    local rpath="$2"

    if ! command -v install_name_tool >/dev/null 2>&1; then
        return
    fi

    if has_rpath "$binary" "$rpath"; then
        return
    fi

    install_name_tool -add_rpath "$rpath" "$binary"
}

create_app_icon() {
    local icon_png="$1"
    local icon_icns="$2"
    local iconset_dir

    if ! command -v sips >/dev/null 2>&1 || ! command -v iconutil >/dev/null 2>&1; then
        return
    fi

    iconset_dir="$(mktemp -d)/AppIcon.iconset"
    mkdir -p "$iconset_dir"
    sips -z 16 16 "$icon_png" --out "$iconset_dir/icon_16x16.png" >/dev/null 2>&1
    sips -z 32 32 "$icon_png" --out "$iconset_dir/icon_16x16@2x.png" >/dev/null 2>&1
    sips -z 32 32 "$icon_png" --out "$iconset_dir/icon_32x32.png" >/dev/null 2>&1
    sips -z 64 64 "$icon_png" --out "$iconset_dir/icon_32x32@2x.png" >/dev/null 2>&1
    sips -z 128 128 "$icon_png" --out "$iconset_dir/icon_128x128.png" >/dev/null 2>&1
    sips -z 256 256 "$icon_png" --out "$iconset_dir/icon_128x128@2x.png" >/dev/null 2>&1
    sips -z 256 256 "$icon_png" --out "$iconset_dir/icon_256x256.png" >/dev/null 2>&1
    sips -z 512 512 "$icon_png" --out "$iconset_dir/icon_256x256@2x.png" >/dev/null 2>&1
    sips -z 512 512 "$icon_png" --out "$iconset_dir/icon_512x512.png" >/dev/null 2>&1
    sips -z 1024 1024 "$icon_png" --out "$iconset_dir/icon_512x512@2x.png" >/dev/null 2>&1

    iconutil -c icns "$iconset_dir" -o "$icon_icns" >/dev/null 2>&1 || true
    rm -rf "$(dirname "$iconset_dir")"
}

echo -e "${GREEN}🍎 Building USBridgeClient for macOS${NC}"

# 1. Проверка GStreamer
echo -e "\n${YELLOW}📦 Проверка зависимостей...${NC}"
GST_LAUNCH=""
for p in "gst-launch-1.0" "/opt/homebrew/bin/gst-launch-1.0" "/usr/local/bin/gst-launch-1.0"; do
    if command -v "$p" &>/dev/null || [ -x "$p" ]; then
        GST_LAUNCH="$p"
        break
    fi
done

if [ -z "$GST_LAUNCH" ]; then
    echo -e "${RED}❌ GStreamer не найден. Установите:${NC}"
    echo "   brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad"
    exit 1
fi
echo -e "   gst-launch: $GST_LAUNCH"

# 2. Сборка .app bundle
echo -e "\n${YELLOW}🔨 Компиляция .app...${NC}"
# Убираем артефакты от старых/прерванных fyne package запусков.
rm -f "$REPO_ROOT/cmd/fyne_metadata_init.go"
rm -rf "$REPO_ROOT/cmd/$APP_BUNDLE_NAME"

# Исправление линковки: pkg-config может возвращать устаревший путь (1.26.10),
# а GStreamer обновлён (1.28.x). Добавляем -L/opt/homebrew/lib для поиска libs.
HOMEBREW_PREFIX="/opt/homebrew"
[ -d "/usr/local/lib" ] && [ ! -d "/opt/homebrew/lib" ] && HOMEBREW_PREFIX="/usr/local"
export CGO_ENABLED=1
export CGO_LDFLAGS="-L${HOMEBREW_PREFIX}/lib ${CGO_LDFLAGS:-}"
export CGO_CPPFLAGS="-I${HOMEBREW_PREFIX}/include ${CGO_CPPFLAGS:-}"
# Подавить предупреждение format-security из зависимости go-gst (gst_debug.go)
export CGO_CFLAGS="${CGO_CFLAGS:-} -Wno-format-security"
rm -rf "$DIST_DIR"
mkdir -p "$APP_MACOS_DIR" "$APP_RESOURCES_DIR"
APP_BINARY_PATH="$APP_MACOS_DIR/$BINARY_NAME"
go build -ldflags="-s -w" -o "$APP_BINARY_PATH" ./cmd

# Добавляем стабильные rpath к Homebrew lib, чтобы .app запускался напрямую из Finder.
ensure_rpath "$APP_BINARY_PATH" "/opt/homebrew/lib"
ensure_rpath "$APP_BINARY_PATH" "/usr/local/lib"

chmod +x "$APP_BINARY_PATH"

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

if command -v codesign >/dev/null 2>&1; then
    codesign --force --deep --sign - "$DIST_DIR/$APP_BUNDLE_NAME" >/dev/null 2>&1 || true
fi
touch "$DIST_DIR/$APP_BUNDLE_NAME"

echo -e "${GREEN}   ✅ App bundle: $DIST_DIR/$APP_BUNDLE_NAME${NC}"

# 3. Подготовка dist
echo -e "\n${YELLOW}📁 Подготовка dist...${NC}"

# Копируем config если есть рядом с .app
[ -f config.yaml ] && cp config.yaml "$DIST_DIR/"

# Создаем скрипт установки FFmpeg
cat > "$DIST_DIR/install_ffmpeg.sh" << 'EOF'
#!/bin/bash
set -e
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGET_DIR="$DIR/USBridgeClient.app/Contents/MacOS"
mkdir -p "$TARGET_DIR"
echo "Downloading FFmpeg for macOS..."
curl -L https://evermeet.cx/ffmpeg/get/zip -o ffmpeg.zip
unzip -o ffmpeg.zip -d "$TARGET_DIR"
rm ffmpeg.zip
chmod +x "$TARGET_DIR/ffmpeg"
echo "FFmpeg installed to $TARGET_DIR"
EOF
chmod +x "$DIST_DIR/install_ffmpeg.sh"

# Создаем скрипт установки GStreamer
cat > "$DIST_DIR/install_gstreamer.sh" << 'EOF'
#!/bin/bash
set -e
echo "Downloading and installing GStreamer for macOS via Homebrew..."
if ! command -v brew >/dev/null 2>&1; then
    echo "Homebrew is required but not installed. Please install Homebrew first."
    exit 1
fi
brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad
echo "GStreamer installed system-wide. USBridgeClient will find it automatically."
EOF
chmod +x "$DIST_DIR/install_gstreamer.sh"

# 4. Создаём README для dist
cat > "$DIST_DIR/README.txt" << 'README'
USBridgeClient for macOS
=========================

Run:
  Open USBridgeClient.app

Requirements:
  - Run ./install_ffmpeg.sh to download FFmpeg locally (required for video decoding fallback).
  - Run ./install_gstreamer.sh to install GStreamer via Homebrew (required for main video decoding).
  - macOS 10.15+

Configuration:
  config.yaml next to the .app, or ~/.config/usbridge-client/

Application log:
  ~/Library/Logs/USBridgeClient/app.log
  If USBRIDGE_LOG_DIR is set, logs are written there instead.
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
