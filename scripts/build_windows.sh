#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

DIST_DIR="$REPO_ROOT/dist/windows"
EXE_NAME="USBridgeAgent.exe"
OUTPUT_PATH="$DIST_DIR/$EXE_NAME"
BUILD_PKG="./cmd/usbridge_agent"
ICON_PNG_16="$REPO_ROOT/assets/icons/appicon-16.png"
ICON_PNG_32="$REPO_ROOT/assets/icons/appicon-32.png"
ICON_PNG_256="$REPO_ROOT/assets/icons/appicon-256.png"
ICON_ICO="$REPO_ROOT/cmd/usbridge_agent/appicon.ico"
ICON_RC="$REPO_ROOT/cmd/usbridge_agent/appicon.rc"
ICON_SYSO="$REPO_ROOT/cmd/usbridge_agent/appicon_windows_amd64.syso"
WINDRES_BIN="/c/msys64/ucrt64/bin/windres.exe"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}Building usbridge_agent for Windows (MSYS2 UCRT64)${NC}"

if [[ "${OS:-}" != "Windows_NT" ]] && [[ "$(uname -s 2>/dev/null || true)" != MINGW* ]] && [[ "$(uname -s 2>/dev/null || true)" != MSYS* ]]; then
    echo -e "${RED}Этот скрипт рассчитан на запуск в Windows/MSYS2 UCRT64${NC}"
    exit 1
fi

if [[ "${MSYSTEM:-}" != "UCRT64" ]]; then
    echo -e "${RED}Запустите скрипт именно из оболочки MSYS2 UCRT64${NC}"
    echo "Текущий MSYSTEM: ${MSYSTEM:-<empty>}"
    echo "Откройте 'MSYS2 UCRT64' и выполните:"
    echo "  ./scripts/build_windows.sh"
    exit 1
fi

add_to_path_if_exists() {
    local dir="$1"
    [[ -d "$dir" ]] || return 0
    case ":$PATH:" in
        *":$dir:"*) ;;
        *) export PATH="$dir:$PATH" ;;
    esac
}

add_to_path_if_exists "/ucrt64/bin"
add_to_path_if_exists "/usr/bin"

if ! command -v go >/dev/null 2>&1; then
    echo -e "${RED}Go не найден в PATH${NC}"
    echo "Установите пакет MSYS2 для UCRT64, например:"
    echo "  pacman -S --needed mingw-w64-ucrt-x86_64-go"
    exit 1
fi

CC_BIN="${CC:-}"
if [[ -z "$CC_BIN" ]]; then
    if command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1; then
        CC_BIN="x86_64-w64-mingw32-gcc"
    elif command -v gcc >/dev/null 2>&1 && [[ "$(command -v gcc)" == /ucrt64/bin/* ]]; then
        CC_BIN="gcc"
    else
        echo -e "${RED}UCRT64 gcc не найден${NC}"
        echo "Установите toolchain:"
        echo "  pacman -S --needed mingw-w64-ucrt-x86_64-gcc"
        exit 1
    fi
fi

CXX_BIN="${CXX:-}"
if [[ -z "$CXX_BIN" ]]; then
    if command -v x86_64-w64-mingw32-g++ >/dev/null 2>&1; then
        CXX_BIN="x86_64-w64-mingw32-g++"
    elif command -v g++ >/dev/null 2>&1 && [[ "$(command -v g++)" == /ucrt64/bin/* ]]; then
        CXX_BIN="g++"
    else
        echo -e "${RED}UCRT64 g++ не найден${NC}"
        echo "Установите toolchain:"
        echo "  pacman -S --needed mingw-w64-ucrt-x86_64-gcc"
        exit 1
    fi
fi

echo -e "${GREEN}✓${NC} Go: $(go version)"
echo -e "${GREEN}✓${NC} CC: $(command -v "$CC_BIN")"
echo -e "${GREEN}✓${NC} CXX: $(command -v "$CXX_BIN")"

mkdir -p "$DIST_DIR"
rm -f "$OUTPUT_PATH"

if [[ -f "$ICON_PNG_16" && -f "$ICON_PNG_32" && -f "$ICON_PNG_256" ]]; then
    echo -e "${YELLOW}Preparing Windows icon resources...${NC}"
    if ! command -v python >/dev/null 2>&1; then
        echo -e "${RED}python not found in PATH, cannot generate .ico from appicon PNG files${NC}"
        exit 1
    fi
    if [[ ! -x "$WINDRES_BIN" ]]; then
        echo -e "${RED}windres not found at $WINDRES_BIN${NC}"
        exit 1
    fi
    python "$REPO_ROOT/scripts/generate_windows_ico.py" "$ICON_ICO" "$ICON_PNG_16" "$ICON_PNG_32" "$ICON_PNG_256"
    cat > "$ICON_RC" <<'RC'
1 ICON "appicon.ico"
RC
    (
        cd "$REPO_ROOT/cmd/usbridge_agent"
        "$WINDRES_BIN" appicon.rc -O coff -o appicon_windows_amd64.syso
    )
elif [[ -f "$REPO_ROOT/assets/icons/Icon.png" ]]; then
    echo -e "${RED}Windows icon set is incomplete. Expected: appicon-16.png, appicon-32.png, appicon-256.png${NC}"
    exit 1
fi

export GOOS=windows
export GOARCH=amd64
export CGO_ENABLED=1
export CC="$CC_BIN"
export CXX="$CXX_BIN"
export CC_FOR_TARGET="$CC_BIN"
export CXX_FOR_TARGET="$CXX_BIN"
export CC_FOR_windows_amd64="$CC_BIN"
export CXX_FOR_windows_amd64="$CXX_BIN"
export GOCACHE="${GOCACHE:-$REPO_ROOT/.cache/go-build/windows-amd64}"

LDFLAGS="${USBRIDGE_WINDOWS_LDFLAGS:--H=windowsgui}"

echo -e "${YELLOW}Compiling...${NC}"
go build -trimpath -ldflags "$LDFLAGS" -o "$OUTPUT_PATH" "$BUILD_PKG"

if [[ -f "$REPO_ROOT/config.yaml" ]]; then
    cp "$REPO_ROOT/config.yaml" "$DIST_DIR/config.yaml"
fi

source "$SCRIPT_DIR/fetch_sunshine.sh"
fetch_sunshine_windows "$DIST_DIR/sunshine"

cat > "$DIST_DIR/README.txt" <<'README'
USBridgeAgent for Windows
=========================

Built from MSYS2 UCRT64.

Run:
  USBridgeAgent.exe

Video/input: Sunshine (Moonlight GameStream host) is bundled in .\sunshine\ and
is started automatically by the agent (.\sunshine\sunshine.exe), including a
one-time admin credential bootstrap. The agent itself is not in the
video/input path; it only pairs with and relays PINs to Sunshine's local API
(port 47990) on behalf of usbridge_client.

Configuration:
  config.yaml next to the executable, or %USERPROFILE%\.config\usbridge-agent\config.yaml

Logs:
  logs\app.log next to the executable
  If USBRIDGE_LOG_DIR is set, logs are written there instead.
README

echo -e "${GREEN}Done.${NC}"
echo "Binary: $OUTPUT_PATH"
