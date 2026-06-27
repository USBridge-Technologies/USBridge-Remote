#!/bin/bash
# Build USBridge Client for Windows: binary + dist folder with libraries
#
# Требования:
#   Обязательно:  Go, mingw-w64, Fyne
#   Для Moonlight (HW decode + WASAPI audio):
#     FFmpeg MinGW DLLs (avcodec, avutil, swscale, opus):
#       Скачать: https://github.com/BtbN/FFmpeg-Builds/releases (ffmpeg-master-latest-win64-gpl-shared.zip)
#       Распаковать и указать: export FFMPEG_ROOT="/path/to/ffmpeg-mingw"
#   Для RTP видео-режима (опционально):
#     export GSTREAMER_ROOT="C:/gstreamer/1.0/mingw_x86_64"
#
# Без GSTREAMER_ROOT — Moonlight работает через libavcodec (D3D11VA/SW) без GStreamer

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
DIST_WIN="dist/windows"
DIST_WIN_DLLS="$DIST_WIN/lib"
DIST_WIN_BIN="$DIST_WIN/bin"
APP_EXE_NAME="USBridge_Client_app.exe"
LAUNCHER_EXE_NAME="USBridge_Client.exe"
BUILD_CACHE_ROOT="$REPO_ROOT/.cache/build/windows-amd64"
GST_RUNTIME_STAMP_NAME=".gstreamer-runtime.stamp"
GST_RUNTIME_MANIFEST_NAME=".gstreamer-runtime.manifest"
MINIMAL_GST="${MINIMAL_GST:-1}"
MISSING_DLLS_FOUND=0

# Цвета
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

download_file() {
    local url="$1"
    local dest="$2"

    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url" -o "$dest"
        return
    fi
    if command -v wget >/dev/null 2>&1; then
        wget -q -O "$dest" "$url"
        return
    fi
    if command -v powershell >/dev/null 2>&1; then
        powershell -NoProfile -NonInteractive -Command \
            "[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12; Invoke-WebRequest -Uri '$url' -OutFile '$dest'"
        return
    fi

    echo -e "${RED}❌ Не найден downloader для $url${NC}"
    echo "   Нужен один из инструментов: curl, wget или powershell."
    exit 1
}

extract_zip() {
    local archive="$1"
    local dest="$2"

    mkdir -p "$dest"
    if command -v unzip >/dev/null 2>&1; then
        unzip -oq "$archive" -d "$dest"
        return
    fi
    if command -v bsdtar >/dev/null 2>&1; then
        bsdtar -xf "$archive" -C "$dest"
        return
    fi
    if command -v tar >/dev/null 2>&1; then
        tar -xf "$archive" -C "$dest"
        return
    fi
    if command -v powershell >/dev/null 2>&1; then
        powershell -NoProfile -NonInteractive -Command "Expand-Archive -Path '$archive' -DestinationPath '$dest' -Force"
        return
    fi

    echo -e "${RED}❌ Не найден архиватор для $archive${NC}"
    echo "   Нужен один из инструментов: unzip, bsdtar, tar или powershell."
    exit 1
}

hash_file_sha256() {
    local file="$1"

    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$file" | awk '{print $1}'
        return
    fi
    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$file" | awk '{print $1}'
        return
    fi
    if command -v powershell >/dev/null 2>&1; then
        powershell -NoProfile -NonInteractive -Command "(Get-FileHash -Algorithm SHA256 -LiteralPath '$file').Hash.ToLowerInvariant()"
        return
    fi

    echo "No SHA-256 tool available for $file" >&2
    return 1
}

build_cache_fingerprint() {
    local path=""
    local file=""
    local hash=""

    printf "BUILD_VARIANT=%s\n" "$BUILD_VARIANT"
    printf "BUILD_LDFLAGS=%s\n" "$BUILD_LDFLAGS"
    printf "GOOS=%s\n" "$GOOS"
    printf "GOARCH=%s\n" "$GOARCH"
    printf "CGO_ENABLED=%s\n" "${CGO_ENABLED:-}"
    printf "PKG_CONFIG=%s\n" "${PKG_CONFIG:-}"
    printf "DEBUG_CONSOLE=%s\n" "${DEBUG_CONSOLE:-0}"

    for path in "$@"; do
        if [ -d "$path" ]; then
            while IFS= read -r file; do
                [ -f "$file" ] || continue
                hash="$(hash_file_sha256 "$file")" || return 1
                printf "%s %s\n" "${file#$REPO_ROOT/}" "$hash"
            done < <(find "$path" -type f | LC_ALL=C sort)
        elif [ -f "$path" ]; then
            hash="$(hash_file_sha256 "$path")" || return 1
            printf "%s %s\n" "${path#$REPO_ROOT/}" "$hash"
        fi
    done
}

dist_keep_entry() {
    local name="$1"
    local kept=""
    for kept in "${DIST_KEEP_ENTRIES[@]}"; do
        if [ "$kept" = "$name" ]; then
            return 0
        fi
    done
    return 1
}

dist_manifest_entries_valid() {
    local manifest="$1"
    local entry=""

    [ -f "$manifest" ] || return 1

    while IFS= read -r entry; do
        [ -z "$entry" ] && continue
        [ -e "$DIST_WIN/$entry" ] || return 1
    done < "$manifest"

    return 0
}

write_gst_runtime_manifest() {
    local manifest="$1"

    {
        [ -e "$DIST_WIN/lib" ] && printf "%s\n" "lib"
        [ -e "$DIST_WIN/libexec" ] && printf "%s\n" "libexec"
        [ -e "$DIST_WIN/gstreamer-plugins.txt" ] && printf "%s\n" "gstreamer-plugins.txt"
    } | sort -u > "$manifest"
}

echo -e "${GREEN}🪟 Building USBridge Client for Windows${NC}"

# 1. Проверка Go
find_dist_windows_processes() {
    local dist_dir="$1"

    if ! command -v powershell >/dev/null 2>&1; then
        return 0
    fi

    powershell -NoProfile -NonInteractive -Command "
        \$distDir = [System.IO.Path]::GetFullPath('$dist_dir').ToLowerInvariant()
        Get-CimInstance Win32_Process -Filter \"Name='USBridge_Client.exe'\" -ErrorAction SilentlyContinue |
            ForEach-Object {
                \$path = \$_.ExecutablePath
                if (-not \$path) { return }
                try {
                    \$resolved = [System.IO.Path]::GetFullPath(\$path).ToLowerInvariant()
                } catch {
                    \$resolved = \$path.ToLowerInvariant()
                }
                if (\$resolved.StartsWith(\$distDir)) {
                    '{0}|{1}' -f \$_.ProcessId, \$path
                }
            }
    "
}

if ! command -v go &> /dev/null; then
    echo -e "${RED}❌ Go не найден! Установите: https://golang.org/dl/${NC}"
    exit 1
fi
echo -e "${GREEN}✓${NC} Go: $(go version)"

# 2. Проверка mingw-w64
echo -e "\n${YELLOW}🛠️ Проверка mingw-w64...${NC}"
if ! command -v x86_64-w64-mingw32-gcc &> /dev/null; then
    echo -e "${RED}❌ mingw-w64 не найден${NC}"
    echo "   Для кросс-сборки Windows на Linux нужен компилятор:"
    echo "   sudo apt-get install -y mingw-w64"
    exit 1
else
    echo -e "${GREEN}✓${NC} mingw-w64 найден"
    export CGO_ENABLED=1
    # Use native gcc for host tools, MinGW only for Windows target.
    export CC="${CC:-gcc}"
    export CXX="${CXX:-g++}"
    export CC_FOR_BUILD="${CC_FOR_BUILD:-gcc}"
    export CXX_FOR_BUILD="${CXX_FOR_BUILD:-g++}"
    export CC_FOR_TARGET=x86_64-w64-mingw32-gcc
    export CXX_FOR_TARGET=x86_64-w64-mingw32-g++
    export CC_FOR_windows_amd64=x86_64-w64-mingw32-gcc
    export CXX_FOR_windows_amd64=x86_64-w64-mingw32-g++
fi

# 2.5. pkg-config (target: Windows)
echo -e "\n${YELLOW}🧩 Проверка pkg-config (Windows target)...${NC}"
PKG_CONFIG_BIN=""
if command -v x86_64-w64-mingw32-pkg-config &>/dev/null; then
    PKG_CONFIG_BIN="x86_64-w64-mingw32-pkg-config"
elif command -v pkg-config &>/dev/null; then
    PKG_CONFIG_BIN="pkg-config"
fi

if [ -z "$PKG_CONFIG_BIN" ]; then
    echo -e "${RED}❌ pkg-config не найден${NC}"
    echo "   Установите: sudo apt-get install -y pkg-config"
    exit 1
fi

# Configure pkg-config paths from FFMPEG_ROOT and/or GSTREAMER_ROOT.
PKG_CONFIG_EXTRA_DIRS=()

if [ -n "${FFMPEG_ROOT:-}" ] && [ -d "$FFMPEG_ROOT" ]; then
    if [ -d "$FFMPEG_ROOT/lib/pkgconfig" ]; then
        PKG_CONFIG_EXTRA_DIRS+=("$FFMPEG_ROOT/lib/pkgconfig")
        echo -e "${GREEN}✓${NC} FFMPEG_ROOT: $FFMPEG_ROOT"
    else
        echo -e "${YELLOW}⚠${NC} FFMPEG_ROOT задан, но нет $FFMPEG_ROOT/lib/pkgconfig"
    fi
fi

if [ -n "${GSTREAMER_ROOT:-}" ] && [ -d "$GSTREAMER_ROOT" ]; then
    if [ -d "$GSTREAMER_ROOT/lib/pkgconfig" ]; then
        PKG_CONFIG_EXTRA_DIRS+=("$GSTREAMER_ROOT/lib/pkgconfig")
        echo -e "${GREEN}✓${NC} GSTREAMER_ROOT: $GSTREAMER_ROOT"
    else
        echo -e "${YELLOW}⚠${NC} GSTREAMER_ROOT задан, но нет $GSTREAMER_ROOT/lib/pkgconfig"
    fi
fi

if [ "${#PKG_CONFIG_EXTRA_DIRS[@]}" -gt 0 ]; then
    IFS=':' eval 'EXTRA_PC="${PKG_CONFIG_EXTRA_DIRS[*]}"'
    export PKG_CONFIG_LIBDIR="$EXTRA_PC"
    export PKG_CONFIG_PATH="$EXTRA_PC:${PKG_CONFIG_PATH:-}"
fi

if [ "$PKG_CONFIG_BIN" = "pkg-config" ] && [ -z "${PKG_CONFIG_LIBDIR:-}" ]; then
    echo -e "${RED}❌ Для кросс-сборки Windows нужен PKG_CONFIG_LIBDIR или FFMPEG_ROOT/GSTREAMER_ROOT${NC}"
    exit 1
fi

export PKG_CONFIG="$PKG_CONFIG_BIN"
echo -e "${GREEN}✓${NC} PKG_CONFIG: $PKG_CONFIG"

# Moonlight requires FFmpeg (libavcodec/libavutil/libswscale) + opus + openssl.
# GStreamer is optional — only used by legacy RTP video mode.
HAS_FFMPEG=0
HAS_GST=0

if "$PKG_CONFIG" --exists libavcodec libavutil libswscale 2>/dev/null; then
    HAS_FFMPEG=1
    echo -e "${GREEN}✓${NC} FFmpeg libs found (Moonlight HW decode enabled)"
else
    echo -e "${YELLOW}⚠${NC} FFmpeg libs not found via pkg-config — Moonlight will use software decode fallback"
    echo "   Set FFMPEG_ROOT to enable HW decode: export FFMPEG_ROOT=/path/to/ffmpeg-mingw"
fi

if "$PKG_CONFIG" --exists gstreamer-1.0 2>/dev/null; then
    HAS_GST=1
    echo -e "${GREEN}✓${NC} GStreamer found (RTP video mode enabled)"
else
    echo -e "${YELLOW}⚠${NC} GStreamer not found — RTP video mode disabled (Moonlight still works)"
fi

if [ "$HAS_FFMPEG" = "0" ] && [ "$HAS_GST" = "0" ]; then
    echo -e "${YELLOW}⚠${NC} Neither FFmpeg nor GStreamer found — building without video decode libraries"
    echo "   Moonlight will fall back to software decode using bundled opus/openssl only"
fi

# 3. Проверка fyne
echo -e "\n${YELLOW}📦 Проверка fyne...${NC}"
FYNE_BIN=""
# In MSYS2 (UCRT64/MinGW64) go env GOPATH returns a Windows-style path.
# Normalise it to a POSIX path so bash [ -x ] checks work correctly.
_raw_gopath="$(go env GOPATH)"
if command -v cygpath >/dev/null 2>&1; then
    GOPATH_BIN="$(cygpath -u "$_raw_gopath")/bin"
else
    # Fallback: convert C:\foo\bar → /c/foo/bar manually
    GOPATH_BIN="$(printf '%s' "$_raw_gopath" | sed -e 's|\\|/|g' -e 's|^\([A-Za-z]\):|/\L\1|')/bin"
fi
for name in fyne fyne.exe; do
    if command -v "$name" &> /dev/null; then
        FYNE_BIN="$name"
        break
    fi
    if [ -x "$GOPATH_BIN/$name" ]; then
        FYNE_BIN="$GOPATH_BIN/$name"
        break
    fi
done
if [ -z "$FYNE_BIN" ]; then
    echo -e "${YELLOW}⚠${NC} fyne не найден, устанавливаю..."
    go install fyne.io/tools/cmd/fyne@latest
    for name in fyne.exe fyne; do
        if [ -x "$GOPATH_BIN/$name" ]; then
            FYNE_BIN="$GOPATH_BIN/$name"
            break
        fi
    done
    [ -z "$FYNE_BIN" ] && FYNE_BIN="$GOPATH_BIN/fyne"
fi
echo -e "${GREEN}✓${NC} fyne: $FYNE_BIN"

# 4. Иконка
ICON_PATH="$REPO_ROOT/Icon.png"
if [ ! -f "$ICON_PATH" ]; then
    echo -e "${RED}❌ Иконка не найдена: $ICON_PATH${NC}"
    exit 1
fi
echo -e "${GREEN}✓${NC} Иконка: $ICON_PATH"

# 5. Компиляция
echo -e "\n${YELLOW}🔨 Компиляция...${NC}"
cd "$REPO_ROOT/cmd"
export GOOS=windows
export GOARCH=amd64
export GOMAXPROCS=12
export GOCACHE="${GOCACHE:-$REPO_ROOT/.cache/go-build/windows-amd64}"
export GOMODCACHE="${GOMODCACHE:-$REPO_ROOT/.cache/go-mod}"
mkdir -p "$GOCACHE" "$GOMODCACHE"
export GOFLAGS="${GOFLAGS:-} -buildvcs=false"
BUILD_LDFLAGS="-H=windowsgui"
BUILD_VARIANT="release"
if [ "${DEBUG_CONSOLE:-0}" = "1" ]; then
    BUILD_LDFLAGS="-H=console"
    BUILD_VARIANT="console"
    echo -e "${YELLOW}⚠${NC} DEBUG_CONSOLE=1: собираем консольную версию"
fi
BUILD_CACHE_DIR="$BUILD_CACHE_ROOT/$BUILD_VARIANT"
BUILD_CACHE_APP_EXE="$BUILD_CACHE_DIR/$APP_EXE_NAME"
BUILD_CACHE_LAUNCHER_RAW="$BUILD_CACHE_DIR/USBridge_Client_launcher_raw.exe"
BUILD_CACHE_LAUNCHER_EXE="$BUILD_CACHE_DIR/$LAUNCHER_EXE_NAME"
BUILD_CACHE_FINGERPRINT="$BUILD_CACHE_DIR/.build-inputs.sha256"
BUILD_CACHE_FINGERPRINT_TMP="$BUILD_CACHE_DIR/.build-inputs.current"
mkdir -p "$BUILD_CACHE_DIR"
export PATH="$GOPATH_BIN:$PATH"

REBUILD_WINDOWS_EXE=0
REBUILD_WINDOWS_REASON=""
if ! build_cache_fingerprint \
    "$REPO_ROOT/cmd" \
    "$REPO_ROOT/internal" \
    "$REPO_ROOT/go.mod" \
    "$REPO_ROOT/go.sum" \
    "$REPO_ROOT/FyneApp.toml" \
    "$ICON_PATH" > "$BUILD_CACHE_FINGERPRINT_TMP"; then
    echo -e "${RED}Fingerprint generation for build cache failed${NC}"
    exit 1
fi

if [ "${FORCE_REBUILD:-0}" = "1" ]; then
    REBUILD_WINDOWS_EXE=1
    REBUILD_WINDOWS_REASON="FORCE_REBUILD=1"
elif [ ! -f "$BUILD_CACHE_APP_EXE" ]; then
    REBUILD_WINDOWS_EXE=1
    REBUILD_WINDOWS_REASON="cached exe is missing"
elif [ ! -f "$BUILD_CACHE_FINGERPRINT" ]; then
    REBUILD_WINDOWS_EXE=1
    REBUILD_WINDOWS_REASON="build fingerprint is missing"
elif command -v cmp >/dev/null 2>&1; then
    if ! cmp -s "$BUILD_CACHE_FINGERPRINT_TMP" "$BUILD_CACHE_FINGERPRINT"; then
        REBUILD_WINDOWS_EXE=1
        REBUILD_WINDOWS_REASON="build inputs changed"
    fi
elif ! diff -q "$BUILD_CACHE_FINGERPRINT_TMP" "$BUILD_CACHE_FINGERPRINT" >/dev/null 2>&1; then
    REBUILD_WINDOWS_EXE=1
    REBUILD_WINDOWS_REASON="build inputs changed"
fi

# Найти или установить go-winres (встраивает иконку без ImageMagick и fyne)
GOWINRES_BIN=""
for _n in go-winres go-winres.exe; do
    _full="$(command -v "$_n" 2>/dev/null)"
    if [ -n "$_full" ]; then GOWINRES_BIN="$_full"; break; fi
    if [ -x "$GOPATH_BIN/$_n" ]; then GOWINRES_BIN="$GOPATH_BIN/$_n"; break; fi
done
if [ -z "$GOWINRES_BIN" ]; then
    echo -e "${YELLOW}⚠${NC} go-winres не найден, устанавливаю..."
    GOOS="" GOARCH="" go install github.com/tc-hib/go-winres@latest
    for _n in go-winres.exe go-winres; do
        if [ -x "$GOPATH_BIN/$_n" ]; then GOWINRES_BIN="$GOPATH_BIN/$_n"; break; fi
        _full="$(command -v "$_n" 2>/dev/null)"
        if [ -n "$_full" ]; then GOWINRES_BIN="$_full"; break; fi
    done
fi

# 5a. Основное приложение (CGo + Fyne UI, встраиваем иконку через go-winres)
APP_SYSO="$REPO_ROOT/cmd/rsrc_windows_amd64.syso"
if [ "$REBUILD_WINDOWS_EXE" = "1" ]; then
    echo -e "${YELLOW}🧱 Сборка основного приложения (Go cache: $GOCACHE)...${NC}"
    echo "   Reason: $REBUILD_WINDOWS_REASON"
    if [ -n "$GOWINRES_BIN" ] && [ -x "$GOWINRES_BIN" ]; then
        rm -f "$APP_SYSO"
        if (cd "$REPO_ROOT/cmd" && \
            GOOS=windows GOARCH=amd64 \
            "$GOWINRES_BIN" simply --icon "$ICON_PATH" 2>&1); then
            [ -f "$APP_SYSO" ] && echo -e "${GREEN}✓${NC} Иконка встроена в основное приложение: $APP_SYSO"
        fi
    else
        echo -e "${YELLOW}⚠${NC} go-winres недоступен — основное приложение будет без иконки"
    fi
    go build -trimpath -ldflags="$BUILD_LDFLAGS" -o "$BUILD_CACHE_APP_EXE" .
    rm -f "$APP_SYSO"
    mv "$BUILD_CACHE_FINGERPRINT_TMP" "$BUILD_CACHE_FINGERPRINT"
else
    rm -f "$BUILD_CACHE_FINGERPRINT_TMP"
    echo -e "${GREEN}✓${NC} Используем готовый app exe из кэша: $BUILD_CACHE_APP_EXE"
fi

# 5b. Лаунчер (чистый Go, встраиваем иконку через go-winres → .syso → go build)
echo -e "${YELLOW}🧱 Сборка лаунчера с иконкой...${NC}"
LAUNCHER_SYSO="$REPO_ROOT/cmd/launcher/rsrc_windows_amd64.syso"

ICON_EMBEDDED=0
if [ -n "$GOWINRES_BIN" ] && [ -x "$GOWINRES_BIN" ]; then
    rm -f "$LAUNCHER_SYSO"
    if (cd "$REPO_ROOT/cmd/launcher" && \
        GOOS=windows GOARCH=amd64 \
        "$GOWINRES_BIN" simply --icon "$ICON_PATH" 2>&1); then
        if [ -f "$LAUNCHER_SYSO" ]; then
            ICON_EMBEDDED=1
            echo -e "${GREEN}✓${NC} Иконка встроена через go-winres: $LAUNCHER_SYSO"
        fi
    fi
fi
[ "$ICON_EMBEDDED" = "0" ] && echo -e "${YELLOW}⚠${NC} go-winres недоступен — лаунчер будет без иконки"

# Собираем лаунчер обычным go build (.syso подхватывается автоматически)
go build -trimpath -ldflags="-H=windowsgui" \
    -o "$BUILD_CACHE_LAUNCHER_EXE" \
    "$REPO_ROOT/cmd/launcher"
rm -f "$LAUNCHER_SYSO"   # убираем из дерева исходников после сборки
echo -e "${GREEN}✓${NC} Launcher: $BUILD_CACHE_LAUNCHER_EXE"

cd "$REPO_ROOT/cmd"

# 6. Создание dist
echo -e "\n${YELLOW}📁 Создание папки dist...${NC}"
cd "$REPO_ROOT"
mkdir -p "$DIST_WIN"

running_dist_processes=()
while IFS= read -r proc; do
    [ -n "$proc" ] && running_dist_processes+=("$proc")
done < <(find_dist_windows_processes "$DIST_WIN")

if [ "${#running_dist_processes[@]}" -gt 0 ]; then
    echo -e "${RED}❌ Cannot clean $DIST_WIN while USBridge is running from that folder${NC}"
    exit 1
fi

if [ -z "$GSTREAMER_ROOT" ]; then
    GSTREAMER_ROOT="C:/gstreamer/1.0/mingw_x86_64"
fi

GST_CANDIDATES=(
    "$GSTREAMER_ROOT"
    "${GSTREAMER_ROOT//\\/\/}"
    "/c/gstreamer/1.0/mingw_x86_64"
    "C:/gstreamer/1.0/mingw_x86_64"
)

if pkg-config --exists gstreamer-1.0 2>/dev/null; then
    GST_PKG_PREFIX=$(pkg-config --variable=prefix gstreamer-1.0 2>/dev/null)
    [ -n "$GST_PKG_PREFIX" ] && GST_CANDIDATES+=("$GST_PKG_PREFIX")
fi

GST_ROOT=""
for cand in "${GST_CANDIDATES[@]}"; do
    [ -z "$cand" ] && continue
    if [ -d "$cand/bin" ] && [ -d "$cand/lib/gstreamer-1.0" ]; then
        GST_ROOT="$cand"
        break
    fi
    if [ -d "$cand/bin" ]; then
        dll_count=$(find "$cand/bin" -maxdepth 1 -name "*.dll" 2>/dev/null | wc -l)
        [ "$dll_count" -gt 0 ] && GST_ROOT="$cand" && break
    fi
done

GST_RUNTIME_STAMP_PATH="$DIST_WIN/$GST_RUNTIME_STAMP_NAME"
GST_RUNTIME_MANIFEST_PATH="$DIST_WIN/$GST_RUNTIME_MANIFEST_NAME"
SKIP_GST_COPY=0
DIST_KEEP_ENTRIES=()
CURRENT_GST_RUNTIME_STAMP=""

if [ -n "$GST_ROOT" ]; then
    GST_VERSION="$("$PKG_CONFIG" --modversion gstreamer-1.0 2>/dev/null || echo unknown)"
    CURRENT_GST_RUNTIME_STAMP="$(cat <<EOF
bundle_version=1
gst_root=$GST_ROOT
gstreamer_version=$GST_VERSION
minimal_gst=$MINIMAL_GST
gst_plugin_allowlist=${GST_PLUGIN_ALLOWLIST:-}
goarch=$GOARCH
EOF
)"

    if [ "${FORCE_GST_REFRESH:-0}" != "1" ] && [ -f "$GST_RUNTIME_STAMP_PATH" ] && [ -f "$GST_RUNTIME_MANIFEST_PATH" ]; then
        EXISTING_GST_RUNTIME_STAMP="$(cat "$GST_RUNTIME_STAMP_PATH" 2>/dev/null || true)"
        if [ "$EXISTING_GST_RUNTIME_STAMP" = "$CURRENT_GST_RUNTIME_STAMP" ] && dist_manifest_entries_valid "$GST_RUNTIME_MANIFEST_PATH"; then
            SKIP_GST_COPY=1
            while IFS= read -r entry; do
                [ -n "$entry" ] && DIST_KEEP_ENTRIES+=("$entry")
            done < "$GST_RUNTIME_MANIFEST_PATH"
            DIST_KEEP_ENTRIES+=("$GST_RUNTIME_STAMP_NAME" "$GST_RUNTIME_MANIFEST_NAME")
        fi
    fi
fi

cleanup_err="${TMPDIR:-/tmp}/usbridge_dist_cleanup.err"
cleanup_failed=0
while IFS= read -r existing; do
    base_name="$(basename "$existing")"
    if [ "$SKIP_GST_COPY" = "1" ] && dist_keep_entry "$base_name"; then
        continue
    fi
    if ! rm -rf -- "$existing" 2>>"$cleanup_err"; then
        cleanup_failed=1
    fi
done < <(find "$DIST_WIN" -mindepth 1 -maxdepth 1 -print 2>>"$cleanup_err")

if [ "$cleanup_failed" != "0" ]; then
    echo -e "${RED}❌ Failed to clean $DIST_WIN${NC}"
    rm -f "$cleanup_err"
    exit 1
fi
rm -f "$cleanup_err"
mkdir -p "$DIST_WIN_DLLS" "$DIST_WIN_BIN"

cp "$BUILD_CACHE_APP_EXE" "$DIST_WIN_BIN/$APP_EXE_NAME"
echo -e "${GREEN}✓${NC} bin/$APP_EXE_NAME (основное приложение)"
cp "$BUILD_CACHE_LAUNCHER_EXE" "$DIST_WIN/$LAUNCHER_EXE_NAME"
echo -e "${GREEN}✓${NC} $LAUNCHER_EXE_NAME (лаунчер с иконкой)"
[ -f config.yaml ] && cp config.yaml "$DIST_WIN/" && echo -e "${GREEN}✓${NC} config.yaml"

# 7a. Копирование FFmpeg DLLs (для Moonlight HW decode)
echo -e "\n${YELLOW}📚 Копирование FFmpeg DLLs (Moonlight HW decode)...${NC}"

# ── DLL utility functions (used by 7a, 7b, 7c) ────────────────────────────────
OBJDUMP_BIN="${OBJDUMP_BIN:-x86_64-w64-mingw32-objdump}"
if ! command -v "$OBJDUMP_BIN" >/dev/null 2>&1; then
    if command -v objdump >/dev/null 2>&1; then
        OBJDUMP_BIN="objdump"
    elif [ -n "$FFMPEG_BIN_DIR" ] && [ -f "$FFMPEG_BIN_DIR/objdump.exe" ]; then
        OBJDUMP_BIN="$FFMPEG_BIN_DIR/objdump.exe"
    elif [ -f "/ucrt64/bin/objdump.exe" ]; then
        OBJDUMP_BIN="/ucrt64/bin/objdump.exe"
    elif [ -f "/mingw64/bin/objdump.exe" ]; then
        OBJDUMP_BIN="/mingw64/bin/objdump.exe"
    fi
fi
echo "=> Using objdump: $OBJDUMP_BIN"

# MSYS2 prefix dirs
DLL_EXTRA_DIRS=()
for _pfx in "/ucrt64" "/mingw64" "/clang64" "/c/msys64/ucrt64" "/c/msys64/mingw64" "C:/msys64/ucrt64" "C:/msys64/mingw64"; do
    if [ -d "$_pfx/bin" ]; then
        DLL_EXTRA_DIRS+=("$_pfx/bin")
    elif [ -d "$_pfx" ] && ls "$_pfx"/*.dll &>/dev/null 2>&1; then
        DLL_EXTRA_DIRS+=("$_pfx")
    fi
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
        d3dcompiler_47.dll|opengl32.dll|mf.dll|mfplat.dll|mfuuid.dll|\
        uuid.dll|wininet.dll|netapi32.dll|iphlpapi.dll|\
        msimg32.dll|userenv.dll|bcryptprimitives.dll|ncrypt.dll|\
        wsock32.dll|wldap32.dll|gdiplus.dll|dnsapi.dll|\
        dwrite.dll|usp10.dll|cfgmgr32.dll|\
        d3d12.dll|d2d1.dll|ksuser.dll|mfreadwrite.dll|\
        mfmediaengine.dll|mfcore.dll|mfsensorgroup.dll|\
        d3d9.dll|d3d10.dll|dxcore.dll|dcomp.dll|\
        dbghelp.dll|psapi.dll|pdh.dll|wtsapi32.dll|\
        authz.dll|wintrust.dll|aclui.dll|cabinet.dll|\
        ndfapi.dll|devobj.dll|hid.dll|hidparse.dll|\
        ksproxy.ax|avrt.dll|wmcodecdspuuid.dll)
            return 0 ;;
    esac
    return 1
}

_collect_dll_deps() {
    local file="$1"
    [ -f "$file" ] || return
    "$OBJDUMP_BIN" -p "$file" \
        | grep -i 'DLL Name:' \
        | awk '{print $NF}' \
        | tr -d '\r'
}

_resolve_dll() {
    local name="$1"
    # Search: FFMPEG_BIN_DIR → FFMPEG_ROOT → GST_ROOT → DLL_EXTRA_DIRS
    local _d _found
    
    # Pre-process search paths to ensure they are MSYS2-friendly if possible
    local _search_paths=(
        "${FFMPEG_BIN_DIR:-}"
        "${FFMPEG_ROOT:-}/bin"
        "${GST_ROOT:-}/bin"
        "${GST_ROOT:-}/lib"
        "${DLL_EXTRA_DIRS[@]}"
    )

    for _d in "${_search_paths[@]}"; do
        [ -z "$_d" ] || [ ! -d "$_d" ] && continue
        
        # Try direct match
        if [ -f "$_d/$name" ]; then 
            printf "%s\n" "$_d/$name"
            return
        fi
        
        # Try bash wildcard expansion
        for _found in "$_d"/$name; do
            if [ -f "$_found" ]; then
                printf "%s\n" "$_found"
                return
            fi
        done

        # Try case-insensitive find
        if command -v find >/dev/null 2>&1; then
            _found="$(find "$_d" -maxdepth 1 -iname "$name" 2>/dev/null | head -1)"
            if [ -n "$_found" ] && [ -f "$_found" ]; then
                printf "%s\n" "$_found"
                return
            fi
        fi
    done
}

_copy_dll() {
    local name="$1" target_dir="$2"
    local resolved
    resolved="$(_resolve_dll "$name")"
    [ -n "$resolved" ] && [ -f "$resolved" ] || return
    cp -L "$resolved" "$target_dir/" || true
    printf "%s\n" "$(basename "$resolved")"
}

_walk_deps() {
    # collect_recursive_deps_into <target_dir> <file>...
    local target_dir="$1"; shift
    local queue=("$@") 
    local idx=0
    local visited_deps=()
    
    # Pre-populate visited with what's already in queue (basenames)
    for f in "${queue[@]}"; do
        visited_deps+=("$(basename "$f" | tr '[:upper:]' '[:lower:]')")
    done

    echo "=> Starting recursive dependency walk in $target_dir"
    while [ $idx -lt ${#queue[@]} ]; do
        local file="${queue[$idx]}"
        idx=$((idx+1))
        
        [ -f "$file" ] || continue
        echo "   ... walking deps of $(basename "$file")"
        
        local deps_tmp
        deps_tmp="$(mktemp)"
        _collect_dll_deps "$file" > "$deps_tmp" 2>/dev/null || { rm -f "$deps_tmp"; continue; }
        
        while IFS= read -r dep; do
            [ -z "$dep" ] && continue
            is_system_dll "$dep" && continue
            
            local dep_lower="${dep,,}"
            local already_visited=0
            for v in "${visited_deps[@]}"; do
                if [ "$v" = "$dep_lower" ]; then already_visited=1; break; fi
            done
            [ "$already_visited" = "1" ] && continue
            
            # Even if it exists, we add it to visited and queue to walk its own deps
            # but we only copy if it doesn't exist
            local _existing
            _existing="$(find "$target_dir" -maxdepth 1 -iname "$dep" 2>/dev/null | head -1)"
            
            if [ -z "$_existing" ]; then
                local copied
                copied="$(_copy_dll "$dep" "$target_dir")" || true
                if [ -n "$copied" ]; then
                    echo -e "   ${GREEN}✓${NC} $copied (dep of $(basename "$file"))" >&2
                    visited_deps+=("${copied,,}")
                    queue+=("$target_dir/$copied")
                else
                    echo -e "   ${RED}❌ ОШИБКА: Не найдена зависимость $dep (для $(basename "$file"))${NC}" >&2
                    MISSING_DLLS_FOUND=1
                    # Add to visited anyway to avoid spamming the same error
                    visited_deps+=("$dep_lower")
                fi
            else
                # Already exists, but we haven't "walked" it yet in this loop 
                # (otherwise it would be in visited_deps already)
                visited_deps+=("$dep_lower")
                queue+=("$_existing")
            fi
        done < "$deps_tmp"
        rm -f "$deps_tmp"
    done
}
# ── end DLL utilities ──────────────────────────────────────────────────────────

# Auto-detect FFMPEG_ROOT from pkg-config when not explicitly set
if [ -z "${FFMPEG_ROOT:-}" ] && [ "$HAS_FFMPEG" = "1" ]; then
    _pc_prefix="$("$PKG_CONFIG" --variable=prefix libavcodec 2>/dev/null || true)"
    _pc_libdir="$("$PKG_CONFIG" --variable=libdir  libavcodec 2>/dev/null || true)"
    for _d in "$_pc_prefix/bin" "$_pc_libdir/../bin" "$_pc_libdir" "$_pc_prefix"; do
        [ -z "$_d" ] && continue
        if ls "$_d"/avutil-*.dll &>/dev/null 2>&1 || ls "$_d"/avcodec-*.dll &>/dev/null 2>&1; then
            FFMPEG_ROOT="$(realpath "$(dirname "$_d")" 2>/dev/null || echo "$(dirname "$_d")")"
            echo -e "${GREEN}✓${NC} FFMPEG_ROOT auto-detected from pkg-config: $FFMPEG_ROOT"
            break
        fi
    done
fi

FFMPEG_DLLS=(
    "avcodec-*.dll"    # H.264 decoder
    "avutil-*.dll"     # utilities
    "swscale-*.dll"    # pixel format conversion
    "swresample-*.dll" # audio resampling (avcodec dep)
    "avformat-*.dll"   # format/container (avcodec dep on some builds)
    "postproc-*.dll"   # postprocessing (dep on some GPL builds)
    "libjxl*.dll"      # JPEG XL (needed for some FFmpeg builds)
    "libhwy*.dll"      # Highway (jxl dep)
    "libbrotli*.dll"   # Brotli (jxl dep)
    "liblcms2-*.dll"   # Little CMS (jxl dep)
    "libogg-*.dll"     # Ogg (vorbis dep)
    "libvorbis*.dll"   # Vorbis
    "libopus-*.dll"    # Opus
    "libsoxr*.dll"     # SoX resampler (swresample dep)
    "libsharpyuv-*.dll" # Sharp YUV (webp dep)
    "libshaderc*.dll"  # Shaderc (swscale/vulkan dep)
    "libgomp-*.dll"    # OpenMP runtime (aom/dav1d dep)
)
# Moonlight runtime DLLs: openssl + MinGW C runtime + additional deps
MOONLIGHT_RUNTIME_DLLS=(
    "libcrypto-*.dll"
    "libssl-*.dll"
    "libgcc_s_*.dll"
    "libwinpthread-*.dll"
    "libstdc++-*.dll"
    "libzstd.dll"
    "libdeflate.dll"
)
FFMPEG_COPIED=0
FFMPEG_BIN_DIR=""

if [ -n "${FFMPEG_ROOT:-}" ] && [ -d "$FFMPEG_ROOT" ]; then
    for d in "$FFMPEG_ROOT/bin" "$FFMPEG_ROOT"; do
        if ls "$d"/*.dll &>/dev/null 2>&1; then FFMPEG_BIN_DIR="$d"; break; fi
    done
    # Add to DLL_EXTRA_DIRS so the GStreamer section and final walk can find FFmpeg deps
    [ -n "$FFMPEG_BIN_DIR" ] && DLL_EXTRA_DIRS=("$FFMPEG_BIN_DIR" "${DLL_EXTRA_DIRS[@]}")

    if [ -n "$FFMPEG_BIN_DIR" ]; then
        echo "=> Copying libraries from $FFMPEG_BIN_DIR..."
        for pattern in "${FFMPEG_DLLS[@]}"; do
            while IFS= read -r dll; do
                [ -f "$dll" ] || continue
                base_dll="$(basename "$dll")"
                if [ ! -f "$DIST_WIN_DLLS/$base_dll" ]; then
                    cp -L "$dll" "$DIST_WIN_DLLS/"
                    echo -e "   ${GREEN}✓${NC} $base_dll"
                    FFMPEG_COPIED=$((FFMPEG_COPIED + 1))
                fi
            done < <(find "$FFMPEG_BIN_DIR" -maxdepth 1 -iname "$pattern" 2>/dev/null)
        done
        echo -e "${GREEN}✓${NC} FFmpeg/Dependencies: $FFMPEG_COPIED base DLLs copied"

        # Walk recursive deps of all copied DLLs in lib/
        mapfile -t _all_copied < <(find "$DIST_WIN_DLLS" -maxdepth 1 -name "*.dll" 2>/dev/null)
        [ "${#_all_copied[@]}" -gt 0 ] && _walk_deps "$DIST_WIN_DLLS" "${_all_copied[@]}"
    else
        echo -e "${YELLOW}⚠${NC} FFMPEG_ROOT задан, но DLLs не найдены в $FFMPEG_ROOT"
    fi
else
    echo -e "${YELLOW}⚠${NC} FFMPEG_ROOT не задан и не обнаружен автоматически — FFmpeg DLLs не будут в дистрибутиве"
    echo "   Скачайте: https://github.com/BtbN/FFmpeg-Builds/releases (ffmpeg-master-latest-win64-gpl-shared.zip)"
    echo "   Затем: export FFMPEG_ROOT=/path/to/ffmpeg-win64-gpl-shared && ./scripts/build_windows.sh"
fi

# Copy Moonlight-specific runtime DLLs (opus, openssl, MinGW runtime)
echo -e "\n${YELLOW}📚 Копирование Moonlight runtime DLLs (opus, openssl, MinGW)...${NC}"
MOONLIGHT_COPIED=0
for _dll_name in "${MOONLIGHT_RUNTIME_DLLS[@]}"; do
    _copied="$(_copy_dll "$_dll_name" "$DIST_WIN_DLLS")"
    if [ -n "$_copied" ]; then
        echo -e "   ${GREEN}✓${NC} $_copied"
        MOONLIGHT_COPIED=$((MOONLIGHT_COPIED + 1))
    else
        # We don't fail for wildcard patterns that might legitimately match 0 files,
        # but for explicit exact DLL names, we print an error.
        if [[ "$_dll_name" != *"*"* ]]; then
            echo -e "   ${RED}❌ ОШИБКА: Не найдена базовая библиотека $_dll_name${NC}" >&2
            MISSING_DLLS_FOUND=1
        fi
    fi
done
if [ "$MOONLIGHT_COPIED" -gt 0 ]; then
    echo -e "${GREEN}✓${NC} Moonlight runtime: $MOONLIGHT_COPIED DLLs copied"
else
    echo -e "${YELLOW}⚠${NC} Moonlight runtime DLLs not found (opus/openssl may be statically linked or missing)"
fi

# 7b. Копирование GStreamer (для RTP видео-режима)
echo -e "\n${YELLOW}📚 Копирование GStreamer (RTP видео-режим)...${NC}"

if [ -n "$GST_ROOT" ]; then
    echo -e "   Найден: $GST_ROOT"

    if [ "$SKIP_GST_COPY" = "1" ]; then
        echo -e "${GREEN}✓${NC} GStreamer runtime уже подготовлен, переиспользую без перекопирования"
    else
        # GST-specific: copy all DLLs to DIST_WIN_DLLS (lib/)
        copy_dll_by_name() {
            local name="$1"
            [ -z "$name" ] && return
            local found=""
            if [ -f "$GST_ROOT/bin/$name" ]; then
                found="$GST_ROOT/bin/$name"
            else
                found="$(find "$GST_ROOT/bin" "$GST_ROOT/lib" -maxdepth 4 -type f -iname "$name" 2>/dev/null | head -1)"
            fi
            if [ -z "$found" ]; then
                local _extra
                for _extra in "${DLL_EXTRA_DIRS[@]}"; do
                    if [ -f "$_extra/$name" ]; then found="$_extra/$name"; break; fi
                done
            fi
            [ -z "$found" ] && return
            cp -L "$found" "$DIST_WIN_DLLS/" 2>/dev/null || true
        }

        # Reuse global DLL utilities (_collect_dll_deps, _resolve_dll, _copy_dll, _walk_deps)
        # but wrap resolve for GST: also search GST_ROOT explicitly
        _gst_resolve_dll() {
            local name="$1"
            [ -z "$name" ] && return
            local found=""
            for _d in "$GST_ROOT/bin" "$GST_ROOT/lib"; do
                if [ -f "$_d/$name" ]; then found="$_d/$name"; break; fi
                local _fi
                _fi="$(find "$_d" -maxdepth 4 -type f -iname "$name" 2>/dev/null | head -1)"
                [ -n "$_fi" ] && { found="$_fi"; break; }
            done
            if [ -z "$found" ]; then found="$(_resolve_dll "$name")"; fi
            [ -n "$found" ] && printf "%s\n" "$found"
        }

        copy_dll_to_dir() {
            local name="$1" target_dir="$2"
            local resolved
            resolved="$(_gst_resolve_dll "$name")"
            [ -n "$resolved" ] && [ -f "$resolved" ] || return
            cp -L "$resolved" "$target_dir/" 2>/dev/null || true
            printf "%s\n" "$(basename "$resolved")"
        }

        collect_deps() { _collect_dll_deps "$@"; }
        collect_recursive_deps_into() { _walk_deps "$@"; }

        if [ "$MINIMAL_GST" = "1" ]; then
            mkdir -p "$DIST_WIN_DLLS/gstreamer-1.0"
            PLUGINS=(
                "libgstcoreelements.dll" "libgstapp.dll" "libgstrtp.dll" "libgstrtpmanager.dll"
                "libgstudp.dll" "libgstvideoconvert.dll" "libgstvideoconvertscale.dll"
                "libgstvideoscale.dll" "libgstplayback.dll" "libgsttypefindfunctions.dll"
                "libgstd3d11.dll" "libgstjpeg.dll" "libgstjpegformat.dll" "libgstlibav.dll"
                "libgstautodetect.dll" "libgstwic.dll" "libgstqsv.dll" "libgstmsdk.dll"
                "libgstnvcodec.dll" "libgstamfcodec.dll" "libgstvideoparsersbad.dll"
                "libgstvideoparsers.dll" "libgstwinks.dll" "libgstmf.dll"
                "libgstmediafoundation.dll"
            )
            CORE_DLLS=(
                "libgobject-2.0-0.dll" "libglib-2.0-0.dll" "libgio-2.0-0.dll" "libgmodule-2.0-0.dll"
                "libgstreamer-1.0-0.dll" "libgstbase-1.0-0.dll" "libgstvideo-1.0-0.dll"
                "libgstpbutils-1.0-0.dll" "libgstapp-1.0-0.dll" "libintl-*.dll" "libffi-*.dll"
                "libpcre2-8-0.dll" "libgcc_s_seh-1.dll" "libwinpthread-1.dll" "libz-1.dll"
                "libcrypto-3-x64.dll" "libssl-3-x64.dll"
                "libcrypto-1_1-x64.dll" "libssl-1_1-x64.dll"
            )
            needed_dlls=("${CORE_DLLS[@]}")
            while IFS= read -r dep; do needed_dlls+=("$dep"); done < <(collect_deps "$DIST_WIN/$APP_EXE_NAME")

            for plugin in "${PLUGINS[@]}"; do
                if [ -f "$GST_ROOT/lib/gstreamer-1.0/$plugin" ]; then
                    cp -L "$GST_ROOT/lib/gstreamer-1.0/$plugin" "$DIST_WIN_DLLS/gstreamer-1.0/" 2>/dev/null || true
                    while IFS= read -r dep; do needed_dlls+=("$dep"); done < <(collect_deps "$GST_ROOT/lib/gstreamer-1.0/$plugin")
                fi
            done

            printf "%s\n" "${needed_dlls[@]}" | sort -u | while read -r dll; do
                copy_dll_by_name "$dll"
            done

            collect_recursive_deps_into "$DIST_WIN_DLLS" "$DIST_WIN/$APP_EXE_NAME"
            collect_recursive_deps_into "$DIST_WIN_DLLS" $(find "$DIST_WIN_DLLS/gstreamer-1.0" -name "*.dll" 2>/dev/null)
        fi

        for tool in gst-launch-1.0.exe gst-inspect-1.0.exe; do
            [ -f "$GST_ROOT/bin/$tool" ] && cp -L "$GST_ROOT/bin/$tool" "$DIST_WIN_BIN/"
        done
        find "$DIST_WIN_DLLS/gstreamer-1.0" -maxdepth 1 -type f -iname "*.dll" -printf "%f\n" 2>/dev/null | sort > "$DIST_WIN/gstreamer-plugins.txt"
        printf "%s\n" "$CURRENT_GST_RUNTIME_STAMP" > "$GST_RUNTIME_STAMP_PATH"
        write_gst_runtime_manifest "$GST_RUNTIME_MANIFEST_PATH"
    fi
fi

# 7c. Копирование QEMU (qemu-nbd / qemu-img для работы с VMDK/QCOW2/VDI образами)
echo -e "\n${YELLOW}📀 Копирование QEMU (qemu-nbd, qemu-img)...${NC}"

QEMU_BIN_DIR=""
for _qd in \
    "/ucrt64/bin" \
    "/mingw64/bin" \
    "/c/msys64/ucrt64/bin" \
    "/c/msys64/mingw64/bin" \
    "C:/msys64/ucrt64/bin" \
    "C:/msys64/mingw64/bin" \
    "${QEMU_ROOT:-}/bin" \
    "${QEMU_ROOT:-}"
do
    [ -z "$_qd" ] && continue
    if [ -f "$_qd/qemu-nbd.exe" ]; then
        QEMU_BIN_DIR="$_qd"
        break
    fi
done

if [ -n "$QEMU_BIN_DIR" ]; then
    echo "=> QEMU found: $QEMU_BIN_DIR"
    QEMU_COPIED=0
    for _qtool in qemu-nbd.exe qemu-img.exe; do
        if [ -f "$QEMU_BIN_DIR/$_qtool" ]; then
            cp -L "$QEMU_BIN_DIR/$_qtool" "$DIST_WIN_BIN/"
            echo -e "   ${GREEN}✓${NC} bin/$_qtool"
            QEMU_COPIED=$((QEMU_COPIED + 1))
        else
            echo -e "   ${YELLOW}⚠${NC} $_qtool не найден в $QEMU_BIN_DIR"
        fi
    done
    # Добавляем QEMU_BIN_DIR в пул поиска DLL для _walk_deps
    DLL_EXTRA_DIRS=("$QEMU_BIN_DIR" "${DLL_EXTRA_DIRS[@]}")
    # Обходим зависимости скопированных QEMU-бинарников (DLL идут в lib/)
    mapfile -t _qemu_bins < <(find "$DIST_WIN_BIN" -maxdepth 1 \( -name "qemu-nbd.exe" -o -name "qemu-img.exe" \) 2>/dev/null)
    [ "${#_qemu_bins[@]}" -gt 0 ] && _walk_deps "$DIST_WIN_DLLS" "${_qemu_bins[@]}"
    echo -e "${GREEN}✓${NC} QEMU: $QEMU_COPIED бинарников скопировано"
else
    echo -e "${YELLOW}⚠${NC} QEMU не найден — qemu-nbd/qemu-img недоступны (VMDK/QCOW2/VDI образы работать не будут)"
    echo "   Установите: pacman -S mingw-w64-ucrt-x86_64-qemu"
    echo "   Или задайте QEMU_ROOT=/path/to/qemu"
fi

# 7d. Финальный dep walk — обходим все зависимости exe и всех DLL, чтобы не пропустить ни одну транзитивную зависимость
echo -e "\n${YELLOW}🔍 Финальная проверка зависимостей...${NC}"
if command -v "$OBJDUMP_BIN" >/dev/null 2>&1; then
    # Начинаем с exe-файлов в корне и bin/, всех DLL в lib/
    mapfile -t _initial_queue < <(
        find "$DIST_WIN" -maxdepth 1 -name "*.exe" 2>/dev/null
        find "$DIST_WIN_BIN" -maxdepth 1 -name "*.exe" 2>/dev/null
        find "$DIST_WIN_DLLS" -maxdepth 1 -name "*.dll" 2>/dev/null
    )
    if [ "${#_initial_queue[@]}" -gt 0 ]; then
        _walk_deps "$DIST_WIN_DLLS" "${_initial_queue[@]}"
    fi
    echo -e "${GREEN}✓${NC} Финальная проверка завершена"
else
    echo -e "${YELLOW}⚠${NC} objdump не найден — пропускаем финальную проверку зависимостей"
fi

# 7e. Sanity check for known problematic DLLs
echo -e "\n${YELLOW}🛠️ Проверка обязательных библиотек...${NC}"
FORCE_DLLS=(
    "libhwy.dll" "libogg-0.dll" "libbrotlienc.dll" "libjxl_cms.dll"
    "libsoxr.dll" "libsharpyuv-0.dll" "libshaderc_shared.dll" "liblcms2-2.dll"
    "libgomp-1.dll" "libstdc++-6.dll" "libpng16-16.dll"
)
for _fdll in "${FORCE_DLLS[@]}"; do
    _existing="$(find "$DIST_WIN_DLLS" -maxdepth 1 -iname "$_fdll" 2>/dev/null | head -1)"
    if [ -z "$_existing" ]; then
        echo -e "   ${YELLOW}⚠ $_fdll отсутствует, пытаюсь найти принудительно...${NC}"
        _copied="$(_copy_dll "$_fdll" "$DIST_WIN_DLLS")"
        if [ -n "$_copied" ]; then
            echo -e "   ${GREEN}✓${NC} $_copied скопирована принудительно"
            _walk_deps "$DIST_WIN_DLLS" "$DIST_WIN_DLLS/$_copied"
        else
            echo -e "   ${RED}❌ ОШИБКА: Не удалось найти $_fdll${NC}"
            MISSING_DLLS_FOUND=1
        fi
    else
        echo -e "   ${GREEN}✓${NC} $_fdll на месте"
    fi
done

# 8. README
echo -e "\n${YELLOW}📝 Создание README...${NC}"

cat > "$DIST_WIN/README.txt" << 'README'
USBridge Client for Windows
===========================
Double-click USBridge_Client.exe to launch.

Folder structure:
  USBridge_Client.exe          — запускаемый файл (лаунчер с иконкой)
  bin\USBridge_Client_app.exe  — основное приложение (запускается автоматически)
  bin\qemu-nbd.exe             — QEMU NBD (для работы с VMDK/QCOW2/VDI образами)
  bin\qemu-img.exe             — QEMU image tool
  lib\                         — runtime DLLs (FFmpeg, OpenSSL, MinGW runtime, etc.)
  lib\gstreamer-1.0\           — GStreamer plugins (if bundled)

Video modes:
  Moonlight streaming — libavcodec (D3D11VA hardware decode) + WASAPI audio.
    Requires: lib\avcodec-*.dll, lib\avutil-*.dll, lib\swscale-*.dll
    (bundled if FFMPEG_ROOT was set at build time).
    Falls back to software decode if DLLs are missing.

  Legacy RTP mode — requires GStreamer.
    Bundled if GSTREAMER_ROOT was set at build time, otherwise install GStreamer
    from https://gstreamer.freedesktop.org/download/ (MinGW x86_64).

Hardware acceleration:
  D3D11VA: automatic on all modern Windows (DirectX 11 capable GPU).
  Software fallback: used when D3D11VA is unavailable.

Configuration:
  config.yaml next to the exe, or %APPDATA%\usbridge-client\
README

echo -e "\n${YELLOW}📦 Создание архива...${NC}"
cd "$DIST_WIN"
if command -v zip >/dev/null 2>&1; then
    zip -rq "../USBridgeClient-Windows.zip" ./*
elif command -v powershell >/dev/null 2>&1; then
    powershell -NoProfile -NonInteractive -Command "Compress-Archive -Path '.\*' -DestinationPath '..\USBridgeClient-Windows.zip' -Force"
else
    echo -e "${YELLOW}⚠ Zip утилита не найдена. Архив не создан.${NC}"
fi
cd "$REPO_ROOT"

if [ "$MISSING_DLLS_FOUND" = "1" ]; then
    echo -e "\n${RED}❌ Сборка завершена с предупреждениями!${NC}"
    echo -e "${RED}Некоторые необходимые DLL не были найдены.${NC}"
    echo -e "Если вы используете MSYS2 UCRT64, попробуйте установить недостающие пакеты, например:"
    echo -e "   pacman -S mingw-w64-ucrt-x86_64-brotli mingw-w64-ucrt-x86_64-libjxl mingw-w64-ucrt-x86_64-libogg"
    echo -e "Проверьте вывод логов выше, чтобы узнать точные имена пропущенных зависимостей."
    exit 1
fi

echo -e "\n${GREEN}✅ Сборка завершена!${NC}"
echo -e "   Результат: $DIST_WIN/"
echo -e "   Архив:     dist/USBridgeClient-Windows.zip"
