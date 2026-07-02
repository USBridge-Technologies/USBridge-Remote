#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

DIST_DIR="$REPO_ROOT/dist/linux"
EXE_NAME="usbridge-agent"
OUTPUT_PATH="$DIST_DIR/$EXE_NAME"
BUILD_PKG="./cmd/usbridge_agent"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}Building usbridge_agent for Linux${NC}"

if [[ "$(uname -s)" != "Linux" ]]; then
    echo -e "${YELLOW}Warning: This script is intended to run on Linux. Cross-compilation may require additional setup.${NC}"
fi

# Check for required dependencies (common for Fyne apps on Linux)
check_dep() {
    if ! pkg-config --exists "$1"; then
        echo -e "${RED}Missing dependency: $1${NC}"
        echo "Please install development headers (e.g., on Ubuntu: sudo apt install libgl1-mesa-dev libegl1-mesa-dev libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev libxxf86vm-dev libasound2-dev libgtk-3-dev)"
        # exit 1 # Don't exit, maybe user knows what they're doing or cross-compiling
    fi
}

check_dep "x11"
check_dep "gl"
check_dep "gtk+-3.0"

mkdir -p "$DIST_DIR"
rm -f "$OUTPUT_PATH"

export CGO_ENABLED=1
export GOOS=linux
export GOARCH=amd64

echo -e "${YELLOW}Compiling...${NC}"
# -trimpath for reproducible builds
# -ldflags "-s -w" to reduce binary size
go build -trimpath -ldflags "-s -w" -o "$OUTPUT_PATH" "$BUILD_PKG"

if [[ -f "$REPO_ROOT/config.yaml" ]]; then
    cp "$REPO_ROOT/config.yaml" "$DIST_DIR/config.yaml"
fi

source "$SCRIPT_DIR/fetch_sunshine.sh"
fetch_sunshine_linux "$DIST_DIR/sunshine"

cat > "$DIST_DIR/README.txt" <<'README'
USBridgeAgent for Linux
=======================

Run:
  ./usbridge-agent

Video/input: Sunshine (Moonlight GameStream host) is bundled in ./sunshine/ and
is started automatically by the agent (./sunshine/AppRun), including a
one-time admin credential bootstrap. The agent itself is not in the
video/input path; it only pairs with and relays PINs to Sunshine's local API
(port 47990) on behalf of usbridge_client.

Requirements:
  - libgtk-3-0, libgl1, and X11/Wayland libraries
  - For Wayland: xdg-desktop-portal and xdg-desktop-portal-wlr/gnome/kde (needed by Sunshine's capture backend)
  - Tailscale (optional): system daemon or use "Userspace" mode in app settings

Privileged Ports (e.g., 443):
  If you need to use port 443, run:
  sudo setcap 'cap_net_bind_service=+ep' ./usbridge-agent

Wayland Permissions (The "Tricky Way"):
  In Wayland, you can click "Request" under Screen Recording in the app UI
  BEFORE connecting. This will open a system dialog to approve a Remote Desktop
  session that will be reused for capture and input.

Configuration:
  config.yaml next to the executable, or ~/.config/usbridge-agent/config.yaml

Logs:
  ~/.config/usbridge-agent/logs/app.log (default)
  If USBRIDGE_LOG_DIR is set, logs are written there instead.
README

chmod +x "$OUTPUT_PATH"

echo -e "${GREEN}Done.${NC}"
echo "Binary: $OUTPUT_PATH"
