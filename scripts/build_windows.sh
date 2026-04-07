#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

DIST_DIR="$REPO_ROOT/dist/windows"
EXE_NAME="USBridgeAgent.exe"
OUTPUT_PATH="$DIST_DIR/$EXE_NAME"
BUILD_PKG="./cmd/usbridge_agent"

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

cat > "$DIST_DIR/README.txt" <<'README'
USBridgeAgent for Windows
=========================

Built from MSYS2 UCRT64.

Run:
  USBridgeAgent.exe

Requirements:
  - ffmpeg.exe must be installed and available in PATH, or configured via config.yaml

Configuration:
  config.yaml next to the executable, or %USERPROFILE%\.config\usbridge-agent\config.yaml

Logs:
  logs\app.log next to the executable
  If USBRIDGE_LOG_DIR is set, logs are written there instead.
README

echo -e "${GREEN}Done.${NC}"
echo "Binary: $OUTPUT_PATH"
