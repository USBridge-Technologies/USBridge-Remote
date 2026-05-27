#!/bin/bash
# Build USBridgeClient for Linux
# Output: dist/linux/USBridgeClient.bin (+ config.yaml if present)

set -euo pipefail

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

OUT_DIR="$REPO_ROOT/dist/linux"
mkdir -p "$OUT_DIR"

# Suppress format-security warnings from the go-gst dependency (gst_debug.go)
export CGO_CFLAGS="${CGO_CFLAGS:-} -Wno-format-security"

go build -o "$OUT_DIR/USBridgeClient.bin" ./cmd

[ -f "$REPO_ROOT/config.yaml" ] && cp -f "$REPO_ROOT/config.yaml" "$OUT_DIR/"

# Создаем скрипт установки FFmpeg
cat > "$OUT_DIR/install_ffmpeg.sh" << 'EOF'
#!/bin/bash
set -e
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
echo "Downloading FFmpeg for Linux..."
URL="https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-linux64-gpl.tar.xz"
curl -L "$URL" -o "$DIR/ffmpeg.tar.xz"
mkdir -p "$DIR/ffmpeg_tmp"
tar -xJf "$DIR/ffmpeg.tar.xz" -C "$DIR/ffmpeg_tmp" --strip-components=1
mv "$DIR/ffmpeg_tmp/bin/ffmpeg" "$DIR/ffmpeg"
rm -rf "$DIR/ffmpeg_tmp" "$DIR/ffmpeg.tar.xz"
chmod +x "$DIR/ffmpeg"
echo "FFmpeg downloaded to $DIR/ffmpeg"
EOF
chmod +x "$OUT_DIR/install_ffmpeg.sh"

# Создаем скрипт установки GStreamer
cat > "$OUT_DIR/install_gstreamer.sh" << 'EOF'
#!/bin/bash
set -e
echo "Installing GStreamer for Linux..."
if [ -f /etc/debian_version ]; then
    echo "Detected Debian/Ubuntu. Installing GStreamer via apt..."
    sudo apt-get update && sudo apt-get install -y libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev libgstreamer-plugins-bad1.0-dev gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav gstreamer1.0-tools gstreamer1.0-x gstreamer1.0-alsa gstreamer1.0-gl gstreamer1.0-gtk3 gstreamer1.0-qt5 gstreamer1.0-pulseaudio
elif [ -f /etc/redhat-release ] || [ -f /etc/fedora-release ]; then
    echo "Detected RHEL/CentOS/Fedora. Installing GStreamer via dnf/yum..."
    sudo dnf install -y gstreamer1-devel gstreamer1-plugins-base-tools gstreamer1-plugins-base-devel gstreamer1-plugins-good gstreamer1-plugins-good-extras gstreamer1-plugins-bad-free gstreamer1-plugins-bad-free-devel gstreamer1-plugins-ugly-free || sudo yum install -y gstreamer1-devel gstreamer1-plugins-base-tools gstreamer1-plugins-base-devel gstreamer1-plugins-good gstreamer1-plugins-good-extras gstreamer1-plugins-bad-free gstreamer1-plugins-bad-free-devel gstreamer1-plugins-ugly-free
else
    echo "Universal Linux detected. Please install GStreamer 1.0 plugins manually via your package manager."
fi
EOF
chmod +x "$OUT_DIR/install_gstreamer.sh"

# Создаём README для dist
cat > "$OUT_DIR/README.txt" << 'README'
USBridgeClient for Linux
=========================

Run:
  ./USBridgeClient.bin

Requirements:
  - Run ./install_ffmpeg.sh to download FFmpeg locally (required for video decoding fallback).
  - Run ./install_gstreamer.sh to install GStreamer via package manager (required for main video decoding).

Configuration:
  config.yaml next to the binary, or ~/.config/usbridge-client/
README

echo -e "\nCreating archive..."
cd "$OUT_DIR"
tar -czf "../USBridgeClient-linux.tar.gz" ./*
cd "$REPO_ROOT"

echo "✅ Done: $OUT_DIR/USBridgeClient.bin"
echo "📦 Archive: dist/USBridgeClient-linux.tar.gz"
