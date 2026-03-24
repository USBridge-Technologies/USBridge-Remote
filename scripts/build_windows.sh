#!/bin/bash
# Build USBridge Client for Windows: binary + dist folder with libraries
# Требования: Go, mingw-w64, Fyne, GStreamer (MinGW x86_64)
#
# Для портативного пакета с GStreamer:
#   export GSTREAMER_ROOT="C:/gstreamer/1.0/mingw_x86_64"
#   scripts/build_windows.sh
#
# Без GSTREAMER_ROOT — создаётся только exe + config (GStreamer должен быть установлен на целевой машине)

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
DIST_WIN="dist/windows"
EXE_NAME="USBridge_Client.exe"
WINTUN_VERSION="0.14.1"
WINTUN_URL="https://www.wintun.net/builds/wintun-${WINTUN_VERSION}.zip"

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

resolve_wintun_arch() {
    case "$1" in
        amd64) echo "amd64" ;;
        386) echo "x86" ;;
        arm64) echo "arm64" ;;
        *)
            echo -e "${RED}❌ Неподдерживаемая архитектура для wintun: $1${NC}" >&2
            return 1
            ;;
    esac
}

echo -e "${GREEN}🪟 Building USBridge Client for Windows${NC}"

# 1. Проверка Go
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
# Важно: нельзя использовать host pkg-config (он отдаст /usr/include и сборка будет ломаться).
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

# Если задан GSTREAMER_ROOT (Linux path), подключаем его pkgconfig.
# Примечание: для Windows SDK важно, чтобы *.pc содержали пути, существующие на Linux.
if [ -n "$GSTREAMER_ROOT" ] && [ -d "$GSTREAMER_ROOT" ]; then
    if [ -d "$GSTREAMER_ROOT/lib/pkgconfig" ]; then
        export PKG_CONFIG_LIBDIR="$GSTREAMER_ROOT/lib/pkgconfig"
        export PKG_CONFIG_PATH="$GSTREAMER_ROOT/lib/pkgconfig:${PKG_CONFIG_PATH:-}"
        echo -e "${GREEN}✓${NC} GSTREAMER_ROOT: $GSTREAMER_ROOT"
        echo -e "${GREEN}✓${NC} PKG_CONFIG_LIBDIR: $PKG_CONFIG_LIBDIR"
    else
        echo -e "${YELLOW}⚠${NC} GSTREAMER_ROOT задан, но нет $GSTREAMER_ROOT/lib/pkgconfig"
    fi
fi

# Защита от ситуации, когда pkg-config видит только host glib/gstreamer.
if [ "$PKG_CONFIG_BIN" = "pkg-config" ] && [ -z "${PKG_CONFIG_LIBDIR:-}" ]; then
    echo -e "${RED}❌ Для кросс-сборки Windows нужен pkg-config для target или PKG_CONFIG_LIBDIR${NC}"
    echo "   Сейчас найден только host pkg-config, он будет тянуть /usr/include и сборка упадёт."
    echo "   Варианты:"
    echo "   1) Установить x86_64-w64-mingw32-pkg-config (пакет обычно: pkg-config-mingw-w64-x86-64)"
    echo "   2) Скачать/подготовить Windows (MinGW) GStreamer SDK и указать GSTREAMER_ROOT (Linux path),"
    echo "      чтобы там был lib/pkgconfig с корректными путями."
    exit 1
fi

# Явно задаём PKG_CONFIG для CGO.
export PKG_CONFIG="$PKG_CONFIG_BIN"
echo -e "${GREEN}✓${NC} PKG_CONFIG: $PKG_CONFIG"

# Быстрая проверка: glib/gstreamer должны находиться в target окружении.
if ! "$PKG_CONFIG" --exists glib-2.0 2>/dev/null; then
    echo -e "${RED}❌ pkg-config не находит glib-2.0 для Windows target${NC}"
    echo "   Это означает, что вы кросс-компилируете, но dev-зависимости под Windows не подключены."
    echo "   Решение: установите/подготовьте Windows (MinGW) GStreamer/GLib SDK и задайте GSTREAMER_ROOT."
    exit 1
fi

if ! "$PKG_CONFIG" --exists gstreamer-1.0 2>/dev/null; then
    echo -e "${RED}❌ pkg-config не находит gstreamer-1.0 для Windows target${NC}"
    echo "   Решение: Windows (MinGW) GStreamer SDK + GSTREAMER_ROOT."
    exit 1
fi

# 3. Проверка fyne
echo -e "\n${YELLOW}📦 Проверка fyne...${NC}"
FYNE_BIN=""
GOPATH_BIN="$(go env GOPATH)/bin"
# На Windows/MSYS2 исполняемый файл — fyne.exe
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
# Disable VCS stamping (common failure inside containers)
export GOFLAGS="${GOFLAGS:-} -buildvcs=false"
if [ "${DEBUG_CONSOLE:-0}" = "1" ]; then
    export GOFLAGS="${GOFLAGS} -ldflags=-H=console"
    echo -e "${YELLOW}⚠${NC} DEBUG_CONSOLE=1: собираем консольную версию"
fi
# Добавляем GOPATH/bin в PATH для корректного запуска fyne (MSYS2/Windows)
export PATH="$GOPATH_BIN:$PATH"

echo "--- Вывод fyne package ---"
FYNE_CC="x86_64-w64-mingw32-gcc"
FYNE_CXX="x86_64-w64-mingw32-g++"
FYNE_PKG_CONFIG="${PKG_CONFIG:-x86_64-w64-mingw32-pkg-config}"
if ! env \
    CC="$FYNE_CC" \
    CXX="$FYNE_CXX" \
    PKG_CONFIG="$FYNE_PKG_CONFIG" \
    CGO_ENABLED=1 \
    "$FYNE_BIN" package \
    --target windows \
    --app-id "com.usbridge.client" \
    --name "USBridge Client" \
    --app-version "1.0.0" \
    --icon "$ICON_PATH" \
    --release \
    -- -j 12 2>&1; then
    echo -e "\n${RED}❌ fyne package завершился с ошибкой${NC}"
    echo "Содержимое $(pwd):"
    ls -la
    exit 1
fi
echo "--- Конец вывода fyne ---"

# Ищем созданный exe
EXE_SRC=""
for n in "USBridge_Client.exe" "USBridge Client.exe"; do
    if [ -f "$n" ]; then
        EXE_SRC="$n"
        break
    fi
done

if [ -z "$EXE_SRC" ]; then
    echo -e "${RED}❌ exe не создан${NC}"
    ls -la
    exit 1
fi

# 6. Создание dist
echo -e "\n${YELLOW}📁 Создание папки dist...${NC}"
cd "$REPO_ROOT"
mkdir -p "$DIST_WIN"
cleanup_err="${TMPDIR:-/tmp}/usbridge_dist_cleanup.err"
if ! find "$DIST_WIN" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + 2>"$cleanup_err"; then
    echo -e "${RED}❌ Failed to clean $DIST_WIN${NC}"
    if [ -s "$cleanup_err" ]; then
        sed 's/^/   /' "$cleanup_err"
    fi
    rm -f "$cleanup_err"
    echo "   This usually means a file in dist/windows is open or locked."
    echo "   Check the following:"
    echo "   - USBridge_Client.exe is not running from dist/windows"
    echo "   - no other terminal is currently inside dist/windows"
    echo "   - Explorer, archiver, or antivirus is not locking files there"
    exit 1
fi
rm -f "$cleanup_err"

# Копируем exe
cp "$REPO_ROOT/cmd/$EXE_SRC" "$DIST_WIN/$EXE_NAME"
echo -e "${GREEN}✓${NC} $EXE_NAME"

# Копируем config
[ -f config.yaml ] && cp config.yaml "$DIST_WIN/" && echo -e "${GREEN}✓${NC} config.yaml"

# Копируем wintun.dll рядом с exe, потому что рантайм WireGuard ищет её
# только в каталоге приложения или в System32.
echo -e "\n${YELLOW}🔐 Подготовка WireGuard runtime...${NC}"
WINTUN_TARGET_ARCH="$(resolve_wintun_arch "$GOARCH")"
WINTUN_CANDIDATES=()
if [ -n "${WINTUN_DLL:-}" ]; then
    WINTUN_CANDIDATES+=("$WINTUN_DLL")
fi
if [ -n "${WINTUN_ROOT:-}" ]; then
    WINTUN_CANDIDATES+=(
        "$WINTUN_ROOT/wintun.dll"
        "$WINTUN_ROOT/bin/wintun.dll"
    )
fi
WINTUN_CANDIDATES+=(
    "$REPO_ROOT/wintun.dll"
    "$REPO_ROOT/third_party/wintun/wintun.dll"
    "/c/Program Files/WireGuard/wintun.dll"
    "/c/Windows/System32/wintun.dll"
    "C:/Program Files/WireGuard/wintun.dll"
    "C:/Windows/System32/wintun.dll"
)

WINTUN_DLL_SRC=""
for cand in "${WINTUN_CANDIDATES[@]}"; do
    [ -z "$cand" ] && continue
    if [ -f "$cand" ]; then
        WINTUN_DLL_SRC="$cand"
        break
    fi
done

if [ -n "$WINTUN_DLL_SRC" ]; then
    cp -L "$WINTUN_DLL_SRC" "$DIST_WIN/wintun.dll"
    echo -e "${GREEN}✓${NC} wintun.dll ($(basename "$WINTUN_DLL_SRC"))"
else
    echo -e "${YELLOW}⚠${NC} wintun.dll не найден локально, скачиваю ${WINTUN_URL}"

    WINTUN_CACHE_DIR="${REPO_ROOT}/.cache/wintun/${WINTUN_VERSION}"
    WINTUN_ZIP_PATH="${WINTUN_CACHE_DIR}/wintun-${WINTUN_VERSION}.zip"
    WINTUN_EXTRACT_DIR="${WINTUN_CACHE_DIR}/unzipped"
    mkdir -p "$WINTUN_CACHE_DIR"

    if [ ! -f "$WINTUN_ZIP_PATH" ]; then
        download_file "$WINTUN_URL" "$WINTUN_ZIP_PATH"
    fi

    rm -rf "$WINTUN_EXTRACT_DIR"
    extract_zip "$WINTUN_ZIP_PATH" "$WINTUN_EXTRACT_DIR"

    for cand in \
        "$WINTUN_EXTRACT_DIR/wintun/bin/$WINTUN_TARGET_ARCH/wintun.dll" \
        "$WINTUN_EXTRACT_DIR/bin/$WINTUN_TARGET_ARCH/wintun.dll" \
        "$WINTUN_EXTRACT_DIR/$WINTUN_TARGET_ARCH/wintun.dll"; do
        if [ -f "$cand" ]; then
            WINTUN_DLL_SRC="$cand"
            break
        fi
    done

    if [ -z "$WINTUN_DLL_SRC" ]; then
        WINTUN_DLL_SRC="$(find "$WINTUN_EXTRACT_DIR" -type f -path "*/$WINTUN_TARGET_ARCH/wintun.dll" 2>/dev/null | head -1)"
    fi

    if [ -n "$WINTUN_DLL_SRC" ] && [ -f "$WINTUN_DLL_SRC" ]; then
        cp -L "$WINTUN_DLL_SRC" "$DIST_WIN/wintun.dll"
        echo -e "${GREEN}✓${NC} wintun.dll скачан и добавлен для $WINTUN_TARGET_ARCH"
    else
        echo -e "${RED}❌ Не удалось подготовить wintun.dll для архитектуры $WINTUN_TARGET_ARCH${NC}"
        echo "   Проверялся архив: $WINTUN_URL"
        echo "   Можно задать WINTUN_DLL=/path/to/wintun.dll вручную."
        exit 1
    fi
fi

# 7. Копирование GStreamer
# По умолчанию — стандартный путь установки; можно переопределить: export GSTREAMER_ROOT="..."
if [ -z "$GSTREAMER_ROOT" ]; then
    GSTREAMER_ROOT="C:/gstreamer/1.0/mingw_x86_64"
fi

# Список путей для поиска (GSTREAMER_ROOT и типичные форматы)
GST_CANDIDATES=(
    "$GSTREAMER_ROOT"
    "${GSTREAMER_ROOT//\\/\/}"
    "/c/gstreamer/1.0/mingw_x86_64"
    "C:/gstreamer/1.0/mingw_x86_64"
)

# Если pkg-config знает GStreamer (MSYS2 pacman)
if pkg-config --exists gstreamer-1.0 2>/dev/null; then
    GST_PKG_PREFIX=$(pkg-config --variable=prefix gstreamer-1.0 2>/dev/null)
    [ -n "$GST_PKG_PREFIX" ] && GST_CANDIDATES+=("$GST_PKG_PREFIX")
fi

GST_ROOT=""
for cand in "${GST_CANDIDATES[@]}"; do
    [ -z "$cand" ] && continue
    # GStreamer: bin/ с DLL и lib/gstreamer-1.0/ с плагинами
    if [ -d "$cand/bin" ] && [ -d "$cand/lib/gstreamer-1.0" ]; then
        GST_ROOT="$cand"
        break
    fi
    # Только bin с DLL (standalone installer)
    if [ -d "$cand/bin" ]; then
        dll_count=$(find "$cand/bin" -maxdepth 1 -name "*.dll" 2>/dev/null | wc -l)
        [ "$dll_count" -gt 0 ] && GST_ROOT="$cand" && break
    fi
done

echo -e "\n${YELLOW}📚 Копирование GStreamer...${NC}"

if [ -n "$GST_ROOT" ]; then
    echo -e "   Найден: $GST_ROOT"

    mkdir -p "$DIST_WIN/bin"

    OBJDUMP_BIN="${OBJDUMP_BIN:-x86_64-w64-mingw32-objdump}"
    if ! command -v "$OBJDUMP_BIN" >/dev/null 2>&1; then
        OBJDUMP_BIN="objdump"
    fi

    is_core_dll() {
        local name="$1"
        for core in "${CORE_DLLS[@]}"; do
            case "${name,,}" in
                ${core,,})
                    return 0
                    ;;
            esac
            if [ "${core,,}" = "${name,,}" ]; then
                return 0
            fi
        done
        return 1
    }

    copy_dll_by_name() {
        local name="$1"
        if [ -z "$name" ]; then
            return
        fi
        if [ -f "$GST_ROOT/bin/$name" ]; then
            if is_core_dll "$name"; then
                cp -L "$GST_ROOT/bin/$name" "$DIST_WIN/" 2>/dev/null || true
            else
                cp -L "$GST_ROOT/bin/$name" "$DIST_WIN/bin/" 2>/dev/null || true
            fi
            return
        fi
        if [ -f "$GST_ROOT/lib/$name" ]; then
            if is_core_dll "$name"; then
                cp -L "$GST_ROOT/lib/$name" "$DIST_WIN/" 2>/dev/null || true
            else
                cp -L "$GST_ROOT/lib/$name" "$DIST_WIN/bin/" 2>/dev/null || true
            fi
            return
        fi
        local found
        found="$(find "$GST_ROOT/bin" "$GST_ROOT/lib" -maxdepth 4 -type f -iname "$name" 2>/dev/null | head -1)"
        if [ -n "$found" ]; then
            if is_core_dll "$name"; then
                cp -L "$found" "$DIST_WIN/" 2>/dev/null || true
            else
                cp -L "$found" "$DIST_WIN/bin/" 2>/dev/null || true
            fi
        fi
    }

    collect_deps() {
        local file="$1"
        [ -f "$file" ] || return
        "$OBJDUMP_BIN" -p "$file" 2>/dev/null | awk -F': ' '/DLL Name:/ {gsub(/\\r/,"",$2); print $2}'
    }

    is_system_dll() {
        local name="${1,,}"
        case "$name" in
            api-ms-win-*.dll|ext-ms-win-*.dll|kernel32.dll|user32.dll|gdi32.dll|advapi32.dll|shell32.dll|ole32.dll|oleaut32.dll|comdlg32.dll|comctl32.dll|imm32.dll|setupapi.dll|version.dll|winmm.dll|ws2_32.dll|secur32.dll|rpcrt4.dll|crypt32.dll|bcrypt.dll|ntdll.dll|shlwapi.dll|msvcrt.dll|ucrtbase.dll|dwmapi.dll|dxgi.dll|d3d11.dll|d3dcompiler_47.dll|opengl32.dll)
                return 0
                ;;
        esac
        return 1
    }

    resolve_dll_path() {
        local name="$1"
        [ -z "$name" ] && return

        if [ -f "$GST_ROOT/bin/$name" ]; then
            printf "%s\n" "$GST_ROOT/bin/$name"
            return
        fi
        if [ -f "$GST_ROOT/lib/$name" ]; then
            printf "%s\n" "$GST_ROOT/lib/$name"
            return
        fi

        local found=""
        found="$(find "$GST_ROOT/bin" "$GST_ROOT/lib" -maxdepth 4 -type f -iname "$name" 2>/dev/null | head -1)"
        if [ -n "$found" ]; then
            printf "%s\n" "$found"
            return
        fi

        for extra in /ucrt64/bin /ucrt64/lib /mingw64/bin /mingw64/lib; do
            if [ -f "$extra/$name" ]; then
                printf "%s\n" "$extra/$name"
                return
            fi
        done
    }

    copy_dll_to_dir() {
        local name="$1"
        local target_dir="$2"
        local resolved=""
        [ -z "$name" ] && return
        [ -z "$target_dir" ] && return

        resolved="$(resolve_dll_path "$name")"
        if [ -n "$resolved" ] && [ -f "$resolved" ]; then
            cp -L "$resolved" "$target_dir/" 2>/dev/null || true
            printf "%s\n" "$(basename "$resolved")"
        fi
    }

    expand_dll_pattern() {
        local pattern="$1"
        [ -z "$pattern" ] && return

        {
            find "$GST_ROOT/bin" "$GST_ROOT/lib" -maxdepth 4 -type f -iname "$pattern" -printf "%f\n" 2>/dev/null
            for extra in /ucrt64/bin /ucrt64/lib /mingw64/bin /mingw64/lib; do
                find "$extra" -maxdepth 1 -type f -iname "$pattern" -printf "%f\n" 2>/dev/null
            done
        } | sort -u
    }

    collect_recursive_deps_into() {
        local target_dir="$1"
        shift
        local queue=("$@")
        local idx=0

        while [ "$idx" -lt "${#queue[@]}" ]; do
            local file="${queue[$idx]}"
            idx=$((idx + 1))
            [ -f "$file" ] || continue

            while IFS= read -r dep; do
                [ -z "$dep" ] && continue
                if is_system_dll "$dep"; then
                    continue
                fi

                local dep_name
                dep_name="$(basename "$dep")"
                if [ -f "$target_dir/$dep_name" ]; then
                    continue
                fi

                local copied_name
                copied_name="$(copy_dll_to_dir "$dep_name" "$target_dir")"
                if [ -n "$copied_name" ] && [ -f "$target_dir/$copied_name" ]; then
                    queue+=("$target_dir/$copied_name")
                fi
            done < <(collect_deps "$file")
        done
    }

    MINIMAL_GST="${MINIMAL_GST:-1}"
    if [ "$MINIMAL_GST" = "1" ]; then
        mkdir -p "$DIST_WIN/lib/gstreamer-1.0"
        PLUGINS_DEFAULT=(
            "libgstcoreelements.dll"
            "libgstapp.dll"
            "libgstrtp.dll"
            "libgstrtpmanager.dll"
            "libgstudp.dll"
            "libgstvideoconvert.dll"
            "libgstvideoconvertscale.dll"
            "libgstvideoscale.dll"
            "libgstplayback.dll"
            "libgsttypefindfunctions.dll"
            "libgstd3d11.dll"
            "libgstjpeg.dll"
            "libgstjpegformat.dll"
            "libgstlibav.dll"
            "libgstautodetect.dll"
            "libgstvideoparsersbad.dll"
            "libgstvideoparsers.dll"
            "libgstwinks.dll" # contains ksvideosrc for Windows QR camera capture
        )
        PLUGINS=("${PLUGINS_DEFAULT[@]}")
        if [ -n "${GST_PLUGIN_ALLOWLIST:-}" ]; then
            IFS=',' read -r -a PLUGINS <<< "$GST_PLUGIN_ALLOWLIST"
        fi
        REQUIRED_PLUGINS=(
            "libgstrtp.dll"
            "libgstrtpmanager.dll"
            "libgstplayback.dll"
            "libgsttypefindfunctions.dll"
            "libgstjpeg.dll"
            "libgstjpegformat.dll"
            "libgstlibav.dll"
            "libgstwinks.dll"
        )

        needed_dlls=()
        while IFS= read -r dep; do
            needed_dlls+=("$dep")
        done < <(collect_deps "$DIST_WIN/$EXE_NAME")

        # Core runtime DLLs (safe minimal baseline)
        CORE_DLLS=(
            "libgobject-2.0-0.dll"
            "libglib-2.0-0.dll"
            "libgio-2.0-0.dll"
            "libgmodule-2.0-0.dll"
            "libgstreamer-1.0-0.dll"
            "libgstbase-1.0-0.dll"
            "libgstvideo-1.0-0.dll"
            "libgstpbutils-1.0-0.dll"
            "libgstapp-1.0-0.dll"
            "libintl-*.dll"
            "libffi-*.dll"
            "libpcre2-8-0.dll"
            "libgcc_s_seh-1.dll"
            "libwinpthread-1.dll"
            "libz-1.dll"
        )
        needed_dlls+=("${CORE_DLLS[@]}")

        missing_required_plugins=()
        for plugin in "${PLUGINS[@]}"; do
            [ -z "$plugin" ] && continue
            if [ -f "$GST_ROOT/lib/gstreamer-1.0/$plugin" ]; then
                cp -L "$GST_ROOT/lib/gstreamer-1.0/$plugin" "$DIST_WIN/lib/gstreamer-1.0/" 2>/dev/null || true
                while IFS= read -r dep; do
                    needed_dlls+=("$dep")
                done < <(collect_deps "$GST_ROOT/lib/gstreamer-1.0/$plugin")
            else
                case " ${REQUIRED_PLUGINS[*]} " in
                    *" $plugin "*) missing_required_plugins+=("$plugin") ;;
                esac
            fi
        done

        if [ "${#missing_required_plugins[@]}" -gt 0 ]; then
            echo -e "${RED}❌ Missing required GStreamer plugin(s) in $GST_ROOT/lib/gstreamer-1.0:${NC}"
            printf '   - %s\n' "${missing_required_plugins[@]}"
            echo "   Windows camera capture uses ksvideosrc, which is provided by libgstwinks.dll."
            echo "   Install a GStreamer build with gst-plugins-bad for Windows, then rebuild."
            exit 1
        fi

        # unique + copy deps
        if [ "${#needed_dlls[@]}" -gt 0 ]; then
            printf "%s\n" "${needed_dlls[@]}" | sort -u | while read -r dll; do
                case "$dll" in
                    *'*'*|*'?'*)
                        while IFS= read -r matched; do
                            [ -n "$matched" ] && copy_dll_by_name "$matched"
                        done < <(expand_dll_pattern "$dll")
                        ;;
                    *)
                        copy_dll_by_name "$dll"
                        ;;
                esac
            done
        fi

        root_dep_seeds=("$DIST_WIN/$EXE_NAME")
        while IFS= read -r root_dll; do
            root_dep_seeds+=("$root_dll")
        done < <(find "$DIST_WIN" -maxdepth 1 -type f -iname "*.dll" 2>/dev/null | sort)
        collect_recursive_deps_into "$DIST_WIN" "${root_dep_seeds[@]}"

        bin_dep_seeds=()
        while IFS= read -r bin_dll; do
            bin_dep_seeds+=("$bin_dll")
        done < <(find "$DIST_WIN/bin" "$DIST_WIN/lib/gstreamer-1.0" -maxdepth 1 -type f -iname "*.dll" 2>/dev/null | sort)
        if [ "${#bin_dep_seeds[@]}" -gt 0 ]; then
            collect_recursive_deps_into "$DIST_WIN/bin" "${bin_dep_seeds[@]}"
        fi

        GST_PLUGIN_SCANNER_SRC=""
        for scanner in \
            "$GST_ROOT/libexec/gstreamer-1.0/gst-plugin-scanner.exe" \
            "$GST_ROOT/libexec/gstreamer-1.0/gst-plugin-scanner" \
            "$GST_ROOT/bin/gst-plugin-scanner.exe" \
            "$GST_ROOT/bin/gst-plugin-scanner"; do
            if [ -f "$scanner" ]; then
                GST_PLUGIN_SCANNER_SRC="$scanner"
                break
            fi
        done

        if [ -n "$GST_PLUGIN_SCANNER_SRC" ]; then
            mkdir -p "$DIST_WIN/libexec/gstreamer-1.0"
            cp -L "$GST_PLUGIN_SCANNER_SRC" "$DIST_WIN/libexec/gstreamer-1.0/"
            echo -e "${GREEN}✓${NC} gst-plugin-scanner ($(basename "$GST_PLUGIN_SCANNER_SRC"))"
        else
            echo -e "${YELLOW}⚠${NC} gst-plugin-scanner не найден в $GST_ROOT"
            echo "   Портативный пакет всё ещё может работать, но часть плагинов может не обнаруживаться без scanner."
        fi

        find "$DIST_WIN/lib/gstreamer-1.0" -maxdepth 1 -type f -iname "*.dll" -printf "%f\n" 2>/dev/null | sort > "$DIST_WIN/gstreamer-plugins.txt"
        echo -e "${GREEN}✓${NC} gstreamer-plugins.txt"
        echo -e "${GREEN}✓${NC} GStreamer минимальный набор (MINIMAL_GST=1)"
    else
        if [ -d "$GST_ROOT/bin" ]; then
            cp -L "$GST_ROOT"/bin/*.dll "$DIST_WIN/bin/" 2>/dev/null || true
            echo -e "${GREEN}✓${NC} bin/*.dll"
        fi

        if [ -d "$GST_ROOT/lib/gstreamer-1.0" ]; then
            mkdir -p "$DIST_WIN/lib/gstreamer-1.0"
            cp -L "$GST_ROOT/lib/gstreamer-1.0"/*.dll "$DIST_WIN/lib/gstreamer-1.0/" 2>/dev/null || true
            echo -e "${GREEN}✓${NC} lib/gstreamer-1.0/*.dll"
        fi

        if [ -d "$GST_ROOT/libexec/gstreamer-1.0" ]; then
            mkdir -p "$DIST_WIN/libexec/gstreamer-1.0"
            cp -L "$GST_ROOT/libexec/gstreamer-1.0"/gst-plugin-scanner* "$DIST_WIN/libexec/gstreamer-1.0/" 2>/dev/null || true
            echo -e "${GREEN}✓${NC} libexec/gstreamer-1.0/gst-plugin-scanner*"
        fi
    fi

    # libintl_setlocale — явная проверка (gettext), критично для glib/GStreamer
    for libintl in "$GST_ROOT/bin/libintl"*.dll "$GST_ROOT/lib/libintl"*.dll /ucrt64/bin/libintl*.dll /mingw64/bin/libintl*.dll; do
        if [ -f "$libintl" ]; then
            cp -L "$libintl" "$DIST_WIN/bin/" 2>/dev/null && cp -L "$libintl" "$DIST_WIN/" 2>/dev/null
            echo -e "${GREEN}✓${NC} $(basename "$libintl") (libintl_setlocale)"
            break
        fi
    done
else
    echo -e "${YELLOW}⚠ GStreamer не найден. Проверка путей:${NC}"
    for cand in "${GST_CANDIDATES[@]}"; do
        [ -z "$cand" ] && continue
        if [ ! -d "$cand" ]; then
            echo "   - $cand — папки нет"
        elif [ ! -d "$cand/bin" ]; then
            echo "   - $cand — нет bin/"
        elif [ ! -d "$cand/lib/gstreamer-1.0" ]; then
            echo "   - $cand — нет lib/gstreamer-1.0/"
        else
            echo "   - $cand — структура есть, но *.dll не найдены"
        fi
    done
    echo ""
    echo "   Установите GStreamer (MinGW x86_64):"
    echo "   https://gstreamer.freedesktop.org/download/#windows"
    echo "   Путь по умолчанию: C:\\gstreamer\\1.0\\mingw_x86_64"
fi

# 8. README
cat > "$DIST_WIN/README.txt" << 'README'
USBridge Client for Windows
===========================

Run:
  USBridge_Client.exe - directly

If the folder contains bin/ and lib/gstreamer-1.0/, the package is portable.
It uses a minimal GStreamer plugin set.

If GStreamer is not bundled, install GStreamer (MinGW x86_64):
  https://gstreamer.freedesktop.org/download/#windows

Configuration: config.yaml (in this folder or %APPDATA%\usbridge-client\)
README

echo -e "${GREEN}✓${NC} README.txt"

# 9. Итог
echo -e "\n${GREEN}✅ Сборка завершена!${NC}"
echo -e "   Результат: $DIST_WIN/"
echo -e "   Содержимое:"
ls -la "$DIST_WIN/"
echo ""
echo -e "   Для запуска на другой машине — скопируйте папку $DIST_WIN целиком."
echo ""
