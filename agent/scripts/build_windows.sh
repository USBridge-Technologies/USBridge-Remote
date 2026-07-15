#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

VERSION="$(tr -d ' \t\n\r' < "$REPO_ROOT/VERSION" 2>/dev/null || echo "0.0.0")"

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

    WINDRES_BIN=""
    if command -v x86_64-w64-mingw32-windres >/dev/null 2>&1; then
        WINDRES_BIN="$(command -v x86_64-w64-mingw32-windres)"
    elif command -v windres >/dev/null 2>&1 && [[ "$(command -v windres)" == /ucrt64/bin/* ]]; then
        WINDRES_BIN="$(command -v windres)"
    elif [[ -x "/c/msys64/ucrt64/bin/windres.exe" ]]; then
        WINDRES_BIN="/c/msys64/ucrt64/bin/windres.exe"
    else
        echo -e "${RED}windres not found (looked in PATH and /c/msys64/ucrt64/bin)${NC}"
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

# ── DLL utilities ─────────────────────────────────────────────────────────────
OBJDUMP_BIN="${OBJDUMP_BIN:-}"
for _od in \
    "/ucrt64/bin/objdump.exe" \
    "/mingw64/bin/objdump.exe" \
    "/c/msys64/ucrt64/bin/objdump.exe"
do
    if [[ -x "$_od" ]]; then OBJDUMP_BIN="$_od"; break; fi
done
if [[ -z "$OBJDUMP_BIN" ]] && command -v objdump >/dev/null 2>&1; then
    OBJDUMP_BIN="$(command -v objdump)"
fi

# Directories to search for DLLs
DLL_SEARCH_DIRS=()
for _pfx in "/ucrt64/bin" "/mingw64/bin" "/clang64/bin" \
             "/c/msys64/ucrt64/bin" "/c/msys64/mingw64/bin" \
             "C:/msys64/ucrt64/bin" "C:/msys64/mingw64/bin"; do
    [[ -d "$_pfx" ]] && DLL_SEARCH_DIRS+=("$_pfx")
done

is_system_dll() {
    local name="${1,,}"
    case "$name" in
        api-ms-win-*.dll|ext-ms-win-*.dll|\
        kernel32.dll|user32.dll|gdi32.dll|advapi32.dll|shell32.dll|\
        ole32.dll|oleaut32.dll|comdlg32.dll|comctl32.dll|imm32.dll|\
        setupapi.dll|version.dll|winmm.dll|ws2_32.dll|secur32.dll|\
        rpcrt4.dll|crypt32.dll|bcrypt.dll|ntdll.dll|shlwapi.dll|\
        msvcrt.dll|ucrtbase.dll|dwmapi.dll|dxgi.dll|d3d11.dll|\
        d3dcompiler_47.dll|opengl32.dll|gdi32full.dll|\
        uuid.dll|wininet.dll|netapi32.dll|iphlpapi.dll|\
        msimg32.dll|userenv.dll|bcryptprimitives.dll|ncrypt.dll|\
        wsock32.dll|wldap32.dll|gdiplus.dll|dnsapi.dll|\
        dwrite.dll|usp10.dll|cfgmgr32.dll|wintrust.dll|\
        dbghelp.dll|psapi.dll|pdh.dll|wtsapi32.dll|\
        uxtheme.dll|ndfapi.dll|devobj.dll|hid.dll|\
        tap-windows6.dll|vulkan-1.dll)
            return 0 ;;
    esac
    return 1
}

_resolve_dll() {
    local name="$1"
    for _d in "${DLL_SEARCH_DIRS[@]}"; do
        [[ -f "$_d/$name" ]] && { printf "%s\n" "$_d/$name"; return; }
        local _fi
        _fi="$(find "$_d" -maxdepth 1 -iname "$name" 2>/dev/null | head -1)"
        [[ -n "$_fi" ]] && { printf "%s\n" "$_fi"; return; }
    done
}

_walk_deps() {
    local target_dir="$1"; shift
    [[ -n "$OBJDUMP_BIN" ]] || return 0
    local queue=("$@")
    local idx=0
    local visited=()
    for f in "${queue[@]}"; do visited+=("$(basename "$f" | tr '[:upper:]' '[:lower:]')"); done

    while [[ $idx -lt ${#queue[@]} ]]; do
        local file="${queue[$idx]}"; idx=$((idx+1))
        [[ -f "$file" ]] || continue
        while IFS= read -r dep; do
            [[ -z "$dep" ]] && continue
            is_system_dll "$dep" && continue
            local dep_lower="${dep,,}"
            local already=0
            for v in "${visited[@]}"; do [[ "$v" == "$dep_lower" ]] && { already=1; break; }; done
            [[ "$already" == "1" ]] && continue
            visited+=("$dep_lower")
            local _existing
            _existing="$(find "$target_dir" -maxdepth 1 -iname "$dep" 2>/dev/null | head -1)"
            if [[ -z "$_existing" ]]; then
                local resolved
                resolved="$(_resolve_dll "$dep")"
                if [[ -n "$resolved" && -f "$resolved" ]]; then
                    cp -L "$resolved" "$target_dir/"
                    echo -e "   ${GREEN}✓${NC} $(basename "$resolved") (dep of $(basename "$file"))"
                    queue+=("$target_dir/$(basename "$resolved")")
                else
                    echo -e "   ${YELLOW}⚠${NC} $dep not found (dep of $(basename "$file"))"
                fi
            else
                queue+=("$_existing")
            fi
        done < <("$OBJDUMP_BIN" -p "$file" 2>/dev/null | grep -i 'DLL Name:' | awk '{print $NF}' | tr -d '\r')
    done
}

# ── MinGW / UCRT64 runtime DLLs ──────────────────────────────────────────────
# The agent uses CGO (Fyne requires it for OpenGL on Windows). On a clean
# machine without MSYS2 these runtime DLLs are missing → silent crash on launch.
echo -e "\n${YELLOW}Bundling MinGW runtime DLLs...${NC}"
_runtime_copied=0
for _dll in \
    "libgcc_s_seh-1.dll" \
    "libwinpthread-1.dll" \
    "libstdc++-6.dll"
do
    _src="$(_resolve_dll "$_dll")"
    if [[ -n "$_src" && -f "$_src" ]]; then
        cp -L "$_src" "$DIST_DIR/"
        echo -e "   ${GREEN}✓${NC} $_dll"
        _runtime_copied=$(( _runtime_copied + 1 ))
    else
        echo -e "   ${YELLOW}⚠${NC} $_dll not found (may be statically linked or not needed)"
    fi
done
echo -e "${GREEN}✓${NC} MinGW runtime: $_runtime_copied DLL(s) staged"

# Walk transitive deps of the agent exe to catch anything we missed
if [[ -n "$OBJDUMP_BIN" ]]; then
    echo -e "${YELLOW}Walking exe dependencies...${NC}"
    _walk_deps "$DIST_DIR" "$OUTPUT_PATH"
    echo -e "${GREEN}✓${NC} Dep walk complete"
else
    echo -e "${YELLOW}⚠${NC} objdump not found — skipping dep walk"
    echo "   Install: pacman -S --needed mingw-w64-ucrt-x86_64-binutils"
fi

# ── Sunshine (Moonlight GameStream host) ──────────────────────────────────────
source "$SCRIPT_DIR/fetch_sunshine.sh"
fetch_sunshine_windows "$DIST_DIR/sunshine"

# ── Tailscale + wintun.dll ────────────────────────────────────────────────────
# wintun.dll gives the embedded tsnet a real WireGuard kernel-mode TUN adapter
# instead of pure-userspace networking, which enables direct P2P connections
# (no DERP relay). tailscale.exe / tailscaled.exe are included for optional
# system-level VPN management from the command line.
#
# Env overrides:
#   USBRIDGE_SKIP_TAILSCALE=1        skip entirely (offline/dev builds)
#   USBRIDGE_TAILSCALE_FORCE=1       re-download even if already staged
#   USBRIDGE_TAILSCALE_VERSION=1.x   pin a specific version; default: latest stable
#   TAILSCALE_ROOT=/path             use local binaries instead of downloading
echo -e "\n${YELLOW}Bundling Tailscale + wintun.dll...${NC}"

if [[ "${USBRIDGE_SKIP_TAILSCALE:-0}" == "1" ]]; then
    echo -e "${YELLOW}USBRIDGE_SKIP_TAILSCALE=1 — skipping${NC}"
else
    _ts_already_staged=0
    if [[ "${USBRIDGE_TAILSCALE_FORCE:-0}" != "1" ]] \
        && [[ -f "$DIST_DIR/wintun.dll" ]] \
        && [[ -f "$DIST_DIR/tailscale.exe" ]] \
        && [[ -f "$DIST_DIR/tailscaled.exe" ]]; then
        echo -e "${GREEN}✓${NC} Tailscale already staged, skipping download"
        _ts_already_staged=1
    fi

    if [[ "$_ts_already_staged" == "0" ]]; then
        # 1. Try local TAILSCALE_ROOT or MSYS2 system paths
        _ts_bin_dir=""
        for _candidate in \
            "${TAILSCALE_ROOT:-}" \
            "${TAILSCALE_ROOT:-}/bin" \
            "/ucrt64/bin" \
            "/mingw64/bin" \
            "/clang64/bin"
        do
            [[ -z "$_candidate" ]] && continue
            if [[ -f "$_candidate/wintun.dll" ]] || [[ -f "$_candidate/tailscale.exe" ]]; then
                _ts_bin_dir="$_candidate"
                echo -e "${GREEN}✓${NC} Tailscale found locally: $_ts_bin_dir"
                break
            fi
        done

        # 2. Download from pkgs.tailscale.com
        if [[ -z "$_ts_bin_dir" ]]; then
            if ! command -v curl >/dev/null 2>&1; then
                echo -e "${RED}curl not found — cannot download Tailscale${NC}"
                echo "Set TAILSCALE_ROOT=/path/to/dir/with/wintun.dll or USBRIDGE_SKIP_TAILSCALE=1"
                exit 1
            fi

            _ts_version="${USBRIDGE_TAILSCALE_VERSION:-}"
            if [[ -z "$_ts_version" ]]; then
                _ts_version=$(curl -fsSL \
                    "https://api.github.com/repos/tailscale/tailscale/releases/latest" \
                    2>/dev/null \
                    | python -c "import sys,json; print(json.load(sys.stdin)['tag_name'].lstrip('v'))" \
                    2>/dev/null || true)
            fi
            [[ -z "$_ts_version" ]] && _ts_version="1.98.8"
            echo "   Tailscale version: $_ts_version"

            _ts_cache="$REPO_ROOT/.cache/tailscale-windows"
            mkdir -p "$_ts_cache"
            _ts_msi="$_ts_cache/tailscale-setup-${_ts_version}-amd64.msi"
            _ts_url="https://pkgs.tailscale.com/stable/tailscale-setup-${_ts_version}-amd64.msi"

            if [[ ! -f "$_ts_msi" ]]; then
                echo "   Downloading: $_ts_url"
                curl -fL --progress-bar -o "$_ts_msi" "$_ts_url"
            else
                echo -e "${GREEN}✓${NC} Using cached MSI: $_ts_msi"
            fi

            if ! command -v msiexec.exe >/dev/null 2>&1; then
                echo -e "${RED}msiexec.exe not found — cannot extract MSI${NC}"
                echo "Run this script from MSYS2 on Windows, or set TAILSCALE_ROOT manually"
                exit 1
            fi

            _ts_extract="$_ts_cache/extracted"
            rm -rf "$_ts_extract"
            _ts_msi_win="$(cygpath -w "$_ts_msi" 2>/dev/null || echo "$_ts_msi")"
            _ts_extract_win="$(cygpath -w "$_ts_extract" 2>/dev/null || echo "$_ts_extract")"

            echo "   Extracting MSI (no installation)..."
            powershell -NoProfile -NonInteractive -Command \
                "\$p = Start-Process msiexec.exe -ArgumentList '/a \"$_ts_msi_win\" /qn TARGETDIR=\"$_ts_extract_win\"' -Wait -PassThru -WindowStyle Hidden; exit \$p.ExitCode"

            _ts_found="$(find "$_ts_extract" -name "tailscale.exe" 2>/dev/null | head -1)"
            if [[ -z "$_ts_found" ]]; then
                echo -e "${RED}tailscale.exe not found in extracted MSI${NC}"
                exit 1
            fi
            _ts_bin_dir="$(dirname "$_ts_found")"
            echo -e "${GREEN}✓${NC} Tailscale extracted to $_ts_bin_dir"
        fi

        # 3. Copy to dist
        _ts_copied=0
        for _f in wintun.dll tailscale.exe tailscaled.exe; do
            if [[ -f "$_ts_bin_dir/$_f" ]]; then
                cp -L "$_ts_bin_dir/$_f" "$DIST_DIR/"
                echo -e "   ${GREEN}✓${NC} $_f"
                _ts_copied=$(( _ts_copied + 1 ))
            elif [[ "$_f" == "wintun.dll" ]]; then
                echo -e "   ${RED}❌ wintun.dll not found in $_ts_bin_dir${NC}"
                exit 1
            else
                echo -e "   ${YELLOW}⚠${NC} $_f not found in $_ts_bin_dir"
            fi
        done
        echo -e "${GREEN}✓${NC} Tailscale: $_ts_copied file(s) staged"
    fi

    # Walk deps of tailscale binaries (they may need DLLs beyond wintun.dll)
    if [[ -n "$OBJDUMP_BIN" ]]; then
        _ts_bins=()
        for _f in tailscale.exe tailscaled.exe; do
            [[ -f "$DIST_DIR/$_f" ]] && _ts_bins+=("$DIST_DIR/$_f")
        done
        if [[ ${#_ts_bins[@]} -gt 0 ]]; then
            echo -e "${YELLOW}Walking Tailscale binary deps...${NC}"
            _walk_deps "$DIST_DIR" "${_ts_bins[@]}"
        fi
    fi
fi

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

Networking:
  wintun.dll      — WireGuard TUN adapter; enables direct P2P Tailscale
                    connections without DERP relay
  tailscale.exe   — Tailscale CLI (optional, for manual VPN management)
  tailscaled.exe  — Tailscale daemon (optional, for system-level routing)

Configuration:
  config.yaml next to the executable, or %USERPROFILE%\.config\usbridge-agent\config.yaml

Logs:
  logs\app.log next to the executable
  If USBRIDGE_LOG_DIR is set, logs are written there instead.
README

ARCHIVE="$REPO_ROOT/dist/USBridgeAgent-Windows-x86_64-${VERSION}.zip"
rm -f "$ARCHIVE"
echo -e "${YELLOW}Creating archive...${NC}"
(cd "$DIST_DIR" && zip -r "$ARCHIVE" .)
echo -e "${GREEN}✓${NC} Archive: $ARCHIVE"

echo -e "${GREEN}Done.${NC}"
echo "Binary: $OUTPUT_PATH"
