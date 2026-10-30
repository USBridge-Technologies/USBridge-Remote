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

if [[ $# -gt 0 ]]; then
    echo -e "${RED}Unknown argument: $1${NC}" >&2
    exit 1
fi

# No streamer is bundled on Windows: the agent downloads Sunshine (the
# default) from Streamers-Forks' signed release on its first start
# (internal/forkrelease, app/sunshine_update.go) and updates it from there.
# RustShine is never built from source or bundled here either. streamhost.rustshineBackend is always compiled into the
# agent binary regardless (see internal/streamhost/factory.go's own doc
# comment: the old //go:build rustshine compile-time seam is gone, gating
# is entirely runtime via internal/entitlement), so there was never a real
# reason for a build-time streamer choice here in the first place -- an
# entitled supporter gets RustShine exclusively via
# entitlement.StageRustShine's signed-release download, never a locally
# built one. This script used to also offer `-streamer rustshine`, a
# dev-only convenience that compiled the private itsme228/rust-shine
# checkout from source and swapped it in at build time instead -- removed
# entirely (see fetch_rustshine.sh's own removal) so there is exactly one
# way RustShine ever reaches an agent install: the subscription-gated
# download, never a build artifact.
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

LDFLAGS="${USBRIDGE_WINDOWS_LDFLAGS:--H=windowsgui} -X main.version=$VERSION"

# opengl32.dll delay-loaded: glfw links -lopengl32, which made Windows load the
# system opengl32.dll with the exe, before main could point the DLL search at
# softgl\ (cmd/usbridge_agent/softgl_windows.go). Linked against a delay-import
# library instead, it loads on its first use, after that. Built from the
# toolchain's own libopengl32.a; -L puts it ahead of that one.
TOOL_PREFIX="${CC_BIN%gcc*}"
# binutils by the compiler's prefix when there is one (a Linux cross
# toolchain), plain otherwise (MSYS2 UCRT64 has x86_64-w64-mingw32-gcc but an
# unprefixed nm/dlltool/objdump).
bintool() {
    if command -v "${TOOL_PREFIX}$1" >/dev/null 2>&1; then echo "${TOOL_PREFIX}$1"; else echo "$1"; fi
}
DELAYLIB_DIR="$REPO_ROOT/.cache/delaylib"
mkdir -p "$DELAYLIB_DIR"
OPENGL32_IMPLIB="$("$CC_BIN" -print-file-name=libopengl32.a)"
{
    echo "LIBRARY opengl32.dll"
    echo "EXPORTS"
    "$(bintool nm)" -g --defined-only "$OPENGL32_IMPLIB" | awk '$2 == "T" { print $3 }' | grep -v '^__' | sort -u
} > "$DELAYLIB_DIR/opengl32.def"
"$(bintool dlltool)" --input-def "$DELAYLIB_DIR/opengl32.def" --dllname opengl32.dll --output-delaylib "$DELAYLIB_DIR/libopengl32.a"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -L$DELAYLIB_DIR"
echo -e "${GREEN}✓${NC} opengl32.dll delay-loaded ($(grep -cv '^LIBRARY\|^EXPORTS' "$DELAYLIB_DIR/opengl32.def") exports)"

echo -e "${YELLOW}Compiling...${NC}"
go build -trimpath -ldflags "$LDFLAGS" -o "$OUTPUT_PATH" "$BUILD_PKG"

# A normal import of opengl32.dll would undo the software OpenGL fallback.
if "$(bintool objdump)" -p "$OUTPUT_PATH" | sed -n '/^The Import Tables/,/^The Delay Import Tables\|^PE File Base Relocations\|^There is a/p' | grep -qi 'DLL Name: opengl32.dll'; then
    echo -e "${RED}✗${NC} $OUTPUT_PATH imports opengl32.dll directly; it must be delay-loaded" >&2
    exit 1
fi

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
        tap-windows6.dll|vulkan-1.dll|combase.dll)
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
                # `|| true`: `_resolve_dll` returns non-zero when nothing
                # matches (falls off the end of its search loop), and this
                # is a plain (non-`local`-combined) assignment, so under
                # `set -e` that exit status would otherwise abort the whole
                # build right here instead of hitting the "not found"
                # warning below -- confirmed live: an executable walked here
                # once depended on `combase.dll` (now also added to
                # `is_system_dll` since it's a real, always-present Windows
                # component, never something to bundle), which wasn't in any
                # of `DLL_SEARCH_DIRS`, and silently killed the entire
                # script (including every step after this one) with no error
                # message beyond bash's own implicit exit.
                resolved="$(_resolve_dll "$dep" || true)"
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

# ── Software OpenGL (Mesa llvmpipe) ───────────────────────────────────────────
# For machines whose display driver has no OpenGL 2.1 (a VM without 3D, a
# remote desktop session): Fyne can't open a window there. The GUI checks the
# system OpenGL at start and loads these from softgl\ only when it falls
# short (cmd/usbridge_agent/softgl_windows.go). mesa-dist-win, MIT.
MESA_VERSION="26.2.4"
MESA_SHA256="36a30a04d8cab6e2bea2d3c747c326d15024d9711dd80f45ef97ac6f2c3c5086"
MESA_ARCHIVE="$REPO_ROOT/.cache/mesa3d-${MESA_VERSION}-release-mingw.7z"
mkdir -p "$(dirname "$MESA_ARCHIVE")"
if ! echo "$MESA_SHA256  $MESA_ARCHIVE" | sha256sum -c --status 2>/dev/null; then
    echo "Downloading Mesa $MESA_VERSION (software OpenGL)..."
    curl -fsSL -o "$MESA_ARCHIVE" "https://github.com/pal1000/mesa-dist-win/releases/download/${MESA_VERSION}/mesa3d-${MESA_VERSION}-release-mingw.7z"
    echo "$MESA_SHA256  $MESA_ARCHIVE" | sha256sum -c --status || { echo -e "${RED}✗${NC} Mesa archive checksum mismatch" >&2; exit 1; }
fi
rm -rf "$DIST_DIR/softgl" && mkdir -p "$DIST_DIR/softgl"
MESA_TMP="$(mktemp -d)"
if command -v 7z >/dev/null 2>&1; then
    7z x -y -o"$MESA_TMP" "$MESA_ARCHIVE" x64/opengl32.dll x64/libgallium_wgl.dll >/dev/null
else
    bsdtar -xf "$MESA_ARCHIVE" -C "$MESA_TMP" x64/opengl32.dll x64/libgallium_wgl.dll
fi
cp "$MESA_TMP/x64/opengl32.dll" "$MESA_TMP/x64/libgallium_wgl.dll" "$DIST_DIR/softgl/"
rm -rf "$MESA_TMP"
echo -e "${GREEN}✓${NC} Software OpenGL in softgl/ (Mesa $MESA_VERSION)"

# ── Streaming hosts: none bundled ─────────────────────────────────────────────
# Sunshine and RustShine are both downloaded by the agent at runtime -- see
# this script's own top comment. Remove a dist/windows/sunshine or
# rustshine left over from an older build of this same script before
# zipping below, so a stale streamer can't silently ride along.
rm -rf "$DIST_DIR/rustshine" "$DIST_DIR/sunshine"

# ── Tailscale binaries/wintun not needed: agent uses embedded userspace tsnet library ───
rm -f "$DIST_DIR/tailscale.exe" "$DIST_DIR/tailscaled.exe" "$DIST_DIR/wintun.dll"

cat > "$DIST_DIR/README.txt" <<'README'
USBridgeAgent for Windows
=========================

Built from MSYS2 UCRT64.

Run:
  USBridgeAgent.exe

Video/input: Sunshine (Moonlight GameStream host) is downloaded by the agent
on its first start (signed Streamers-Forks release, kept up to date from there)
and started automatically, including a one-time admin credential bootstrap. The agent itself is not in the
video/input path; it only pairs with and relays PINs to Sunshine's local API
(port 47990) on behalf of usbridge_client.

Networking:
  Embedded Tailscale (tsnet) operates in userspace without requiring extra drivers.

Configuration:
  config.yaml next to the executable, or %USERPROFILE%\.config\usbridge-agent\config.yaml

Logs:
  logs\app.log next to the executable
  If USBRIDGE_LOG_DIR is set, logs are written there instead.
README

ARCHIVE="$REPO_ROOT/dist/USBridgeAgent-Windows-x86_64-${VERSION}.zip"
ARCHIVE_TMP="${ARCHIVE}.tmp.$$"
rm -f "$ARCHIVE_TMP"
echo -e "${YELLOW}Creating archive...${NC}"
(cd "$DIST_DIR" && zip -r "$ARCHIVE_TMP" .)
# Zip into a fresh temp name rather than straight into $ARCHIVE: Info-Zip's Windows
# build renames its own temp file onto the target via plain rename(), which (unlike
# POSIX) refuses to overwrite an existing file. If anything still holds a handle on
# an old $ARCHIVE (AV scan, indexer, OneDrive under Desktop), that rename fails with
# "Could not create output file" even right after `rm -f`. `mv -f` uses Windows'
# overwrite-capable move instead, so it succeeds here.
mv -f "$ARCHIVE_TMP" "$ARCHIVE"
echo -e "${GREEN}✓${NC} Archive: $ARCHIVE"

echo -e "${GREEN}Done.${NC}"
echo "Binary: $OUTPUT_PATH"
