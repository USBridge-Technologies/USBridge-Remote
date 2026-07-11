#!/bin/bash
# Build USBridgeClient for Linux
# Output: dist/linux/USBridgeClient.bin (+ config.yaml if present)
#
# Build deps (install before running this script):
#   Moonlight HW decode:  libavcodec-dev libavutil-dev libswscale-dev libpulse-dev
#   Moonlight core:       opus openssl pkg-config cmake
#   RTP video mode:       gstreamer1.0-* (optional, see install_gstreamer.sh in dist)
#
# One-liner: sudo apt-get install -y libavcodec-dev libavutil-dev libswscale-dev libpulse-dev \
#              libopus-dev libssl-dev pkg-config cmake

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

# go-gst (used for non-Moonlight RTP path) generates format-security warnings.
export CGO_CFLAGS="${CGO_CFLAGS:-} -Wno-format-security"

# Verify Moonlight HW decode build deps are present before spending time compiling.
for pkg in libavcodec libavutil libswscale libpulse-simple; do
    if ! pkg-config --exists "$pkg" 2>/dev/null; then
        echo "❌ Missing build dep: $pkg"
        echo "   Install: sudo apt-get install -y libavcodec-dev libavutil-dev libswscale-dev libpulse-dev"
        exit 1
    fi
done

VERSION=$(cat "$REPO_ROOT/VERSION" 2>/dev/null || echo "1.0.0")
go build -ldflags "-X main.version=$VERSION" -o "$OUT_DIR/USBridgeClient.bin" ./cmd

[ -f "$REPO_ROOT/config.yaml" ] && cp -f "$REPO_ROOT/config.yaml" "$OUT_DIR/"

# Создаем скрипт установки зависимостей для Moonlight HW-decode
cat > "$OUT_DIR/install_moonlight_deps.sh" << 'EOF'
#!/bin/bash
set -e
# Install runtime libraries required for Moonlight hardware decode (libavcodec/ALSA).
# These are dynamically linked — required on the target machine.
echo "Installing Moonlight HW decode runtime dependencies..."
if [ -f /etc/debian_version ]; then
    sudo apt-get update && sudo apt-get install -y \
        libavcodec60 libavutil58 libswscale7 \
        libpulse0 \
        libva2 libva-drm2  # VA-API for Intel/AMD GPU acceleration
elif [ -f /etc/redhat-release ] || [ -f /etc/fedora-release ]; then
    sudo dnf install -y ffmpeg-libs pulseaudio-libs libva
else
    echo "Install ffmpeg-libs (libavcodec), pulseaudio-libs, and libva via your package manager."
fi
echo "Done. Moonlight streaming uses libavcodec (VA-API/NVDEC/SW) + PulseAudio/PipeWire."
EOF
chmod +x "$OUT_DIR/install_moonlight_deps.sh"

# Создаем скрипт установки GStreamer (для RTP видео-режима)
cat > "$OUT_DIR/install_gstreamer.sh" << 'EOF'
#!/bin/bash
set -e
# GStreamer is required only for the legacy RTP video mode.
# Moonlight streaming uses libavcodec (VA-API/NVDEC) + ALSA natively — no GStreamer.
echo "Installing GStreamer for Linux (RTP video mode only)..."
if [ -f /etc/debian_version ]; then
    sudo apt-get update && sudo apt-get install -y \
        gstreamer1.0-tools gstreamer1.0-libav \
        gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
        gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly \
        gstreamer1.0-alsa gstreamer1.0-pulseaudio
elif [ -f /etc/redhat-release ] || [ -f /etc/fedora-release ]; then
    sudo dnf install -y gstreamer1 gstreamer1-plugins-base \
        gstreamer1-plugins-good gstreamer1-plugins-bad-free gstreamer1-libav
else
    echo "Install GStreamer 1.0 via your package manager (gstreamer1.0-tools gstreamer1.0-libav)."
fi
EOF
chmod +x "$OUT_DIR/install_gstreamer.sh"

# Создаём README для dist
cat > "$OUT_DIR/README.txt" << 'README'
USBridgeClient for Linux
=========================

Run:
  ./USBridgeClient.bin

Video modes:
  Moonlight streaming — libavcodec hardware decode (VA-API/NVDEC/software fallback) + PulseAudio/PipeWire audio.
    Run ./install_moonlight_deps.sh to install runtime libraries (libavcodec, libpulse0, libva).

  Legacy RTP mode — requires GStreamer.
    Run ./install_gstreamer.sh to install.

Hardware acceleration:
  Intel/AMD:  VA-API (install libva2 libva-drm2)
  NVIDIA:     NVDEC (install nvidia drivers with CUDA support)
  Fallback:   software decode (works everywhere, higher CPU usage)

Configuration:
  config.yaml next to the binary, or ~/.config/usbridge-client/
README

echo -e "\nCreating archive..."
cd "$OUT_DIR"
tar -czf "../USBridgeClient-linux.tar.gz" ./*
cd "$REPO_ROOT"

echo "✅ Done: $OUT_DIR/USBridgeClient.bin"
echo "📦 Archive: dist/USBridgeClient-linux.tar.gz"
