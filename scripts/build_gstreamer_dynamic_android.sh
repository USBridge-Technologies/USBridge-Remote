#!/bin/bash
#
# Динамическая сборка GStreamer для Android (shared .so).
# Результат:
# - install:   gstreamer-android-dynamic/
# - runtime:   android/jniLibs/arm64-v8a/*.so  (и копия в dist/android/jniLibs/arm64-v8a при наличии dist)
#
# Примечание: cross-file android-arm64.txt перезаписывается под текущий NDK.

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPTS_DIR/.." && pwd)"

if [ -z "${USBRIDGE_LOGGING_ACTIVE:-}" ]; then
    export USBRIDGE_LOGGING_ACTIVE=1
    LOG_DIR="$REPO_ROOT/logs"
    mkdir -p "$LOG_DIR"
    LOG_FILE="$LOG_DIR/$(basename "$0" .sh).log"
    exec > >(tee -a "$LOG_FILE") 2>&1
    echo "=== $(date '+%Y-%m-%d %H:%M:%S') [$0] ==="
fi

echo "🔧 Динамическая компиляция GStreamer для Android (.so)..."

GSTREAMER_DIR="$REPO_ROOT/gstreamer"
BUILD_DIR="$GSTREAMER_DIR/build-android-arm64-shared"
INSTALL_DIR="$REPO_ROOT/gstreamer-android-dynamic"
JNILIBS_DIR="$REPO_ROOT/android/jniLibs/arm64-v8a"
DIST_ANDROID="$REPO_ROOT/dist/android"

ensure_dist_copy() {
    mkdir -p "$DIST_ANDROID" 2>/dev/null || true
    if [ -d "$DIST_ANDROID" ] && [ -w "$DIST_ANDROID" ]; then
        mkdir -p "$DIST_ANDROID/jniLibs/arm64-v8a" 2>/dev/null || true
        cp -f "$JNILIBS_DIR"/*.so "$DIST_ANDROID/jniLibs/arm64-v8a/" 2>/dev/null || true
    fi
}

# Поиск NDK: ANDROID_NDK_HOME -> ANDROID_HOME/ndk -> ~/Library/Android/sdk/ndk -> /usr/lib/android-ndk
if [ -n "${ANDROID_NDK_HOME:-}" ] && [ -d "$ANDROID_NDK_HOME" ]; then
    NDK_PATH="$ANDROID_NDK_HOME"
elif [ -n "${ANDROID_HOME:-}" ] && [ -d "$ANDROID_HOME/ndk" ]; then
    NDK_PATH="$(ls -d "$ANDROID_HOME"/ndk/*/ 2>/dev/null | head -1)"
    NDK_PATH="${NDK_PATH%/}"
elif [ -d "$HOME/Library/Android/sdk/ndk" ]; then
    NDK_PATH="$(ls -d "$HOME/Library/Android/sdk/ndk"/*/ 2>/dev/null | head -1)"
    NDK_PATH="${NDK_PATH%/}"
elif [ -d "/usr/lib/android-ndk" ]; then
    NDK_PATH="/usr/lib/android-ndk"
else
    echo -e "${RED}❌ Android NDK не найден${NC}"
    exit 1
fi

if [ ! -d "$NDK_PATH" ]; then
    echo -e "${RED}❌ Android NDK не найден: $NDK_PATH${NC}"
    exit 1
fi
echo -e "${GREEN}✓${NC} Android NDK: $NDK_PATH"

# Требуется flex для сборки парсеров GStreamer
if ! command -v flex >/dev/null 2>&1; then
    echo -e "${RED}❌ Не найден flex${NC}"
    echo "   Установите flex (например: sudo apt-get install flex)"
    echo "   Или используйте prebuilt: USE_PREBUILT_GSTREAMER=1 scripts/build_android.sh"
    exit 1
fi

if [ ! -d "$GSTREAMER_DIR" ]; then
    echo -e "${RED}❌ Директория gstreamer не найдена: $GSTREAMER_DIR${NC}"
    echo "   Ожидается gst-build репозиторий в корне проекта."
    exit 1
fi

cd "$GSTREAMER_DIR"

# Cross-file (перезаписываем, чтобы не тащить darwin/чужие пути)
CROSS_FILE="$REPO_ROOT/android-arm64.txt"
NDK_PREBUILT=$(find "$NDK_PATH/toolchains/llvm/prebuilt" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | head -1)
if [ -z "$NDK_PREBUILT" ] || [ ! -d "$NDK_PREBUILT" ]; then
    echo -e "${RED}❌ Не найден prebuilt в NDK${NC}"
    exit 1
fi
NDK_BIN="$NDK_PREBUILT/bin"
NDK_SYSROOT="$NDK_PREBUILT/sysroot"
echo "📝 Обновление cross-file: $CROSS_FILE"
cat > "$CROSS_FILE" << EOF
[host_machine]
system = 'android'
cpu_family = 'aarch64'
cpu = 'aarch64'
endian = 'little'

[properties]
sys_root = '$NDK_SYSROOT'
pkg_config_libdir = ''
needs_exe_wrapper = true

[binaries]
c = '$NDK_BIN/aarch64-linux-android28-clang'
cpp = '$NDK_BIN/aarch64-linux-android28-clang++'
ar = '$NDK_BIN/llvm-ar'
strip = '$NDK_BIN/llvm-strip'
pkg-config = 'false'
EOF

export ANDROID_NDK_HOME="$NDK_PATH"

echo "📦 Настройка динамической сборки (shared .so)..."
MESON_EXTRA=""
[ -d "$BUILD_DIR" ] && MESON_EXTRA="--reconfigure"
meson setup $MESON_EXTRA "$BUILD_DIR" \
    --cross-file "$CROSS_FILE" \
    --prefix="$INSTALL_DIR" \
    --buildtype=release \
    --default-library=shared \
    -Dgst-full-plugins='*' \
    -Dbase=enabled \
    -Dgood=enabled \
    -Dbad=enabled \
    -Dlibav=enabled \
    -Dugly=disabled \
    -Dorc=disabled \
    -Ddevtools=disabled \
    -Dglib:sysprof=disabled \
    -Dintrospection=disabled \
    -Dnls=disabled \
    -Dexamples=disabled \
    -Dtests=disabled \
    -Ddoc=disabled \
    -Dgtk_doc=disabled \
    -Dqt5=disabled \
    -Dgst-plugins-base:gl=enabled \
    -Dgst-plugins-base:gl_api=gles2 \
    -Dgst-plugins-base:gl_platform=egl \
    -Dgst-plugins-base:gl_winsys=android \
    -Dgst-plugins-good:qt5=disabled \
    -Dgst-plugins-bad:fdkaac=disabled \
    -Dgst-plugins-bad:androidmedia=enabled

# Исправление прав на glib-mkenums, если Meson создал его без +x
if [ -d "$BUILD_DIR" ]; then
    find "$BUILD_DIR" -type f -name "glib-mkenums" -exec chmod +x {} + 2>/dev/null || true
fi

echo "🔨 Компиляция (~30-60 минут)..."
meson compile -C "$BUILD_DIR"

echo "📦 Установка..."
meson install -C "$BUILD_DIR"

echo "📚 Копирование .so в android/jniLibs/arm64-v8a..."
mkdir -p "$JNILIBS_DIR"

if [ -d "$INSTALL_DIR/lib" ]; then
    # Основная библиотека + зависимости (копируем все .so)
    for f in "$INSTALL_DIR/lib"/*.so; do
        [ -f "$f" ] || continue
        cp -f "$f" "$JNILIBS_DIR/"
        echo -e "${GREEN}✓${NC} $(basename "$f")"
    done
else
    echo -e "${RED}❌ Не найдена директория lib в install prefix: $INSTALL_DIR${NC}"
    exit 1
fi

# libc++_shared.so из NDK (обязательно для Android)
NDK_CPP=$(find "$NDK_PATH/toolchains/llvm/prebuilt" -path "*/lib/aarch64-linux-android/libc++_shared.so" 2>/dev/null | head -1)
if [ -n "$NDK_CPP" ] && [ -f "$NDK_CPP" ]; then
    cp -f "$NDK_CPP" "$JNILIBS_DIR/"
    echo -e "${GREEN}✓${NC} libc++_shared.so"
fi

ensure_dist_copy

SO_COUNT=$(find "$JNILIBS_DIR" -maxdepth 1 -name "*.so" 2>/dev/null | wc -l)
echo ""
echo -e "${GREEN}🎉 GStreamer (dynamic) готов!${NC}"
echo "   Install: $INSTALL_DIR"
echo "   jniLibs:  $JNILIBS_DIR ($SO_COUNT .so)"
