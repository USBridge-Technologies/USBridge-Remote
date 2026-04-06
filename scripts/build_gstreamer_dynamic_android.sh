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
source "$SCRIPTS_DIR/android_env.sh"
refresh_bootstrap_paths

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
MESON_PREFIX="/usr/local"
GST_SOURCE_VERSION="${GST_SOURCE_VERSION:-1.19.2}"
GST_SOURCE_REPO_URL="${GST_SOURCE_REPO_URL:-https://gitlab.freedesktop.org/gstreamer/gst-build.git}"
GST_SOURCE_REF="${GST_SOURCE_REF:-$GST_SOURCE_VERSION}"

ensure_dist_copy() {
    mkdir -p "$DIST_ANDROID" 2>/dev/null || true
    if [ -d "$DIST_ANDROID" ] && [ -w "$DIST_ANDROID" ]; then
        mkdir -p "$DIST_ANDROID/jniLibs/arm64-v8a" 2>/dev/null || true
        cp -f "$JNILIBS_DIR"/*.so "$DIST_ANDROID/jniLibs/arm64-v8a/" 2>/dev/null || true
    fi
}

sync_install_prefix_layout() {
    local staged_root="$INSTALL_DIR$MESON_PREFIX"

    if [ ! -d "$staged_root" ]; then
        echo -e "${RED}❌ Не найден staged root после установки: $staged_root${NC}"
        exit 1
    fi

    rm -rf "$INSTALL_DIR/include" "$INSTALL_DIR/lib" "$INSTALL_DIR/share"

    if [ -d "$staged_root/include" ]; then
        cp -R "$staged_root/include" "$INSTALL_DIR/"
    fi
    if [ -d "$staged_root/lib" ]; then
        cp -R "$staged_root/lib" "$INSTALL_DIR/"
    fi
    if [ -d "$staged_root/share" ]; then
        cp -R "$staged_root/share" "$INSTALL_DIR/"
    fi
}

download_wrap_subprojects_serially() {
    local wrap_dir="$GSTREAMER_DIR/subprojects"
    local wrap_file=""
    local wrap_name=""

    [ -d "$wrap_dir" ] || return 0

    shopt -s nullglob
    for wrap_file in "$wrap_dir"/*.wrap; do
        wrap_name="$(basename "$wrap_file" .wrap)"
        meson subprojects download "$wrap_name" >/dev/null
    done
    shopt -u nullglob
}

meson_windows_path() {
    local target_path="$1"

    if command -v cygpath >/dev/null 2>&1; then
        cygpath -m "$target_path"
        return 0
    fi

    printf '%s\n' "$target_path"
}

bootstrap_gstreamer_source() {
    if [ -d "$GSTREAMER_DIR" ]; then
        return 0
    fi

    if ! ensure_command_available git git; then
        echo -e "${RED}❌ Не найден git, а локальный исходный GStreamer отсутствует${NC}"
        echo "   Не удалось установить git автоматически. Положите исходники вручную в: $GSTREAMER_DIR"
        return 1
    fi

    local tmp_dir="$REPO_ROOT/.gstreamer-bootstrap.$$"

    echo "📥 Локальный GStreamer не найден. Клонирую исходники версии $GST_SOURCE_REF..."
    echo "   Репозиторий: $GST_SOURCE_REPO_URL"
    echo "   Назначение:  $GSTREAMER_DIR"

    if ! git clone --depth 1 --branch "$GST_SOURCE_REF" "$GST_SOURCE_REPO_URL" "$tmp_dir"; then
        echo -e "${RED}❌ Не удалось получить исходники GStreamer ($GST_SOURCE_REF)${NC}"
        echo "   Можно указать другой источник через GST_SOURCE_REPO_URL и GST_SOURCE_REF"
        return 1
    fi

    mv "$tmp_dir" "$GSTREAMER_DIR"
    echo -e "${GREEN}✓${NC} Исходники GStreamer подготовлены: $GSTREAMER_DIR"
}

patch_gstreamer_checkout() {
    "$PYTHON" "$SCRIPTS_DIR/patch_gstreamer_android_checkout.py" "$GSTREAMER_DIR"
}

export_android_env
NDK_PATH="$(resolve_android_ndk 2>/dev/null || true)"
if [ -z "$NDK_PATH" ]; then
    echo -e "${RED}❌ Android NDK не найден${NC}"
    exit 1
fi

if [ ! -d "$NDK_PATH" ]; then
    printf "%b❌ Android NDK не найден: %s%b\n" "$RED" "$NDK_PATH" "$NC"
    exit 1
fi
printf "%b✓%b Android NDK: %s\n" "$GREEN" "$NC" "$NDK_PATH"

if ! setup_android_ndk_toolchain_env "$NDK_PATH" 28; then
    printf "%b❌ Не удалось подготовить toolchain из NDK: %s%b\n" "$RED" "$NDK_PATH" "$NC"
    exit 1
fi

# Требуется flex для сборки парсеров GStreamer
if ! ensure_command_available flex flex || ! ensure_command_available bison bison; then
    echo -e "${RED}❌ Не найдены flex/bison${NC}"
    print_flex_install_hint
    echo "   Для Android build используется source build GStreamer, legacy prebuilt fallback отключён"
    exit 1
fi

if ! ensure_command_available meson meson; then
    echo -e "${RED}❌ Не найден meson${NC}"
    exit 1
fi

if ! ensure_command_available ninja ninja; then
    echo -e "${RED}❌ Не найден ninja${NC}"
    exit 1
fi

if ! ensure_command_available nasm nasm; then
    echo -e "${RED}❌ Не найден nasm${NC}"
    exit 1
fi

if ! ensure_command_available python3 python3; then
    echo -e "${RED}❌ Не найден python3${NC}"
    exit 1
fi

export PYTHON="$(resolve_command_path python3)"

if [ ! -d "$GSTREAMER_DIR" ]; then
    bootstrap_gstreamer_source || exit 1
fi

cd "$GSTREAMER_DIR"

echo "📦 Подготовка wrap-subprojects для свежего checkout..."
download_wrap_subprojects_serially

patch_gstreamer_checkout

if meson_builddir_needs_reset "$BUILD_DIR"; then
    echo -e "${YELLOW}⚠${NC} Найден старый Meson cache от другой платформы. Пересоздаю $BUILD_DIR"
    rm -rf "$BUILD_DIR"
fi

# Cross-file (перезаписываем, чтобы не тащить darwin/чужие пути)
CROSS_FILE="$REPO_ROOT/android-arm64.txt"
NDK_PREBUILT=$(find "$NDK_PATH/toolchains/llvm/prebuilt" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | head -1)
if [ -z "$NDK_PREBUILT" ] || [ ! -d "$NDK_PREBUILT" ]; then
    echo -e "${RED}❌ Не найден prebuilt в NDK${NC}"
    exit 1
fi
NDK_BIN="$NDK_PREBUILT/bin"
NDK_SYSROOT="$NDK_PREBUILT/sysroot"
NDK_BIN_MESON="$(meson_windows_path "$NDK_BIN")"
NDK_SYSROOT_MESON="$(meson_windows_path "$NDK_SYSROOT")"
echo "📝 Обновление cross-file: $CROSS_FILE"
cat > "$CROSS_FILE" << EOF
[host_machine]
system = 'android'
cpu_family = 'aarch64'
cpu = 'aarch64'
endian = 'little'

[properties]
sys_root = '$NDK_SYSROOT_MESON'
pkg_config_libdir = ''
needs_exe_wrapper = true

[binaries]
c = '$NDK_BIN_MESON/aarch64-linux-android28-clang.cmd'
cpp = '$NDK_BIN_MESON/aarch64-linux-android28-clang++.cmd'
ar = '$NDK_BIN_MESON/llvm-ar.exe'
strip = '$NDK_BIN_MESON/llvm-strip.exe'
pkg-config = 'false'
EOF

export ANDROID_NDK_HOME="$NDK_PATH"

echo "📦 Настройка динамической сборки (shared .so)..."
MESON_EXTRA=""
[ -d "$BUILD_DIR" ] && MESON_EXTRA="--reconfigure"
MSYS2_ARG_CONV_EXCL='--prefix=' meson setup $MESON_EXTRA "$BUILD_DIR" \
    --cross-file "$CROSS_FILE" \
    --prefix="$MESON_PREFIX" \
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
rm -rf "$INSTALL_DIR"
DESTDIR="$INSTALL_DIR" meson install -C "$BUILD_DIR"
sync_install_prefix_layout

echo "📚 Копирование .so в android/jniLibs/arm64-v8a..."
mkdir -p "$JNILIBS_DIR"

INSTALL_LIB_DIR="$INSTALL_DIR/lib"
if [ -d "$INSTALL_LIB_DIR" ]; then
    # Основная библиотека + зависимости (копируем все .so)
    for f in "$INSTALL_LIB_DIR"/*.so; do
        [ -f "$f" ] || continue
        cp -f "$f" "$JNILIBS_DIR/"
        echo -e "${GREEN}✓${NC} $(basename "$f")"
    done
else
    echo -e "${RED}❌ Не найдена директория lib в staged install: $INSTALL_LIB_DIR${NC}"
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
