#!/bin/bash

# Скрипт для подготовки GStreamer для Android:
# - Сборка из исходников (если есть gstreamer/)
# - Или проверка/установка готовых библиотек

set -e

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "🔧 Подготовка GStreamer для Android..."

# Пути
SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPTS_DIR/.." && pwd)"
source "$SCRIPTS_DIR/android_env.sh"
GSTREAMER_DIR="$REPO_ROOT/gstreamer"
BUILD_DIR="$GSTREAMER_DIR/build-android-arm64"
INSTALL_DIR="$REPO_ROOT/gstreamer-android"
JNILIBS_DIR="$REPO_ROOT/android/jniLibs/arm64-v8a"
DIST_ANDROID="$REPO_ROOT/dist/android"

if [ -z "${USBRIDGE_LOGGING_ACTIVE:-}" ]; then
    export USBRIDGE_LOGGING_ACTIVE=1
    LOG_DIR="$REPO_ROOT/logs"
    mkdir -p "$LOG_DIR"
    LOG_FILE="$LOG_DIR/$(basename "$0" .sh).log"
    exec > >(tee -a "$LOG_FILE") 2>&1
    echo "=== $(date '+%Y-%m-%d %H:%M:%S') [$0] ==="
fi

ensure_dist_copy() {
    mkdir -p "$DIST_ANDROID" 2>/dev/null || true
    if [ -d "$DIST_ANDROID" ] && [ -w "$DIST_ANDROID" ]; then
        mkdir -p "$DIST_ANDROID/jniLibs/arm64-v8a" 2>/dev/null || true
        cp -f "$JNILIBS_DIR"/*.so "$DIST_ANDROID/jniLibs/arm64-v8a/" 2>/dev/null || true
        cp -f "$JNILIBS_DIR"/*.a "$DIST_ANDROID/jniLibs/arm64-v8a/" 2>/dev/null || true
    fi
}

export_android_env
NDK_PATH="$(resolve_android_ndk 2>/dev/null || true)"
if [ -z "$NDK_PATH" ]; then
    echo -e "${RED}❌ Android NDK не найден. Установите через Android Studio: SDK Manager → SDK Tools → NDK${NC}"
    exit 1
fi

if [ ! -d "$NDK_PATH" ]; then
    echo -e "${RED}❌ Android NDK не найден: $NDK_PATH${NC}"
    exit 1
fi

echo -e "${GREEN}✓${NC} Android NDK найден: $NDK_PATH"

if ! setup_android_ndk_toolchain_env "$NDK_PATH" 28; then
    echo -e "${RED}❌ Не удалось подготовить toolchain из NDK: $NDK_PATH${NC}"
    exit 1
fi

# Требуется flex для сборки парсеров GStreamer
if ! command -v flex >/dev/null 2>&1; then
    echo -e "${RED}❌ Не найден flex${NC}"
    echo "   Установите flex (например: sudo apt-get install flex)"
    echo "   Или используйте prebuilt: USE_PREBUILT_GSTREAMER=1 scripts/build_android.sh"
    exit 1
fi

# Проверка: есть ли уже готовые библиотеки GStreamer в jniLibs
ensure_jnilibs_ready() {
    mkdir -p "$JNILIBS_DIR"

    # Копируем libc++_shared.so из NDK (обязательно для Android)
    NDK_PREBUILT="$NDK_PATH/toolchains/llvm/prebuilt"
    if [ -d "$NDK_PREBUILT" ]; then
        NDK_CPP_SHARED=$(find "$NDK_PREBUILT" -path "*/lib/aarch64-linux-android/libc++_shared.so" 2>/dev/null | head -1)
        if [ -n "$NDK_CPP_SHARED" ] && [ -f "$NDK_CPP_SHARED" ]; then
            cp -f "$NDK_CPP_SHARED" "$JNILIBS_DIR/"
            echo -e "${GREEN}✓${NC} libc++_shared.so скопирован из NDK"
        else
            echo -e "${YELLOW}⚠${NC} libc++_shared.so не найден в NDK"
        fi
    else
        echo -e "${YELLOW}⚠${NC} NDK prebuilt не найден"
    fi

    SO_COUNT=$(find "$JNILIBS_DIR" -maxdepth 1 -name "*.so" 2>/dev/null | wc -l)
    A_COUNT=$(find "$JNILIBS_DIR" -maxdepth 1 -name "*.a" 2>/dev/null | wc -l)
    echo -e "${GREEN}✓${NC} В android/jniLibs/arm64-v8a: $SO_COUNT .so, $A_COUNT .a"

    ensure_dist_copy
}

# Режим 1: Сборка из исходников (если есть gstreamer/ и не запрошен prebuilt)
# USE_PREBUILT_GSTREAMER=1 — пропустить сборку из исходников, скачать prebuilt
if [ -d "$GSTREAMER_DIR" ] && [ "${USE_PREBUILT_GSTREAMER:-0}" != "1" ]; then
echo "📦 Сборка GStreamer из исходников..."
    cd "$GSTREAMER_DIR"

if meson_builddir_needs_reset "$BUILD_DIR"; then
    echo -e "${YELLOW}⚠${NC} Найден старый Meson cache от другой платформы. Пересоздаю $BUILD_DIR"
    rm -rf "$BUILD_DIR"
fi

# Генерация cross-file на основе NDK (перезаписываем, чтобы не тащить darwin-пути)
NDK_PREBUILT=$(find "$NDK_PATH/toolchains/llvm/prebuilt" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | head -1)
if [ -z "$NDK_PREBUILT" ] || [ ! -d "$NDK_PREBUILT" ]; then
    echo -e "${RED}❌ Не найден prebuilt в NDK: $NDK_PATH${NC}"
    exit 1
fi
NDK_BIN="$NDK_PREBUILT/bin"
NDK_SYSROOT="$NDK_PREBUILT/sysroot"
CROSS_FILE="$REPO_ROOT/android-arm64.txt"
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

echo "📦 Настройка Meson для Android arm64-v8a..."
MESON_EXTRA=""
[ -d "$BUILD_DIR" ] && MESON_EXTRA="--reconfigure"
meson setup $MESON_EXTRA "$BUILD_DIR" \
        --cross-file "$CROSS_FILE" \
        --prefix="$INSTALL_DIR" \
        --buildtype=release \
        --default-library=shared \
        -Dintrospection=disabled \
        -Dnls=disabled \
        -Dexamples=disabled \
        -Dtests=disabled \
        -Ddoc=disabled \
        -Dgtk_doc=disabled \
        -Dqt5=disabled \
        -Dgst-plugins-base:gl=disabled \
        -Dgst-plugins-good:qt5=disabled \
        -Dgst-plugins-bad:fdkaac=disabled \
        -Dugly=disabled \
        -Dorc=disabled

# Исправление прав на glib-mkenums, если Meson создал его без +x
if [ -d "$BUILD_DIR" ]; then
    find "$BUILD_DIR" -type f -name "glib-mkenums" -exec chmod +x {} + 2>/dev/null || true
fi

echo "🔨 Компиляция GStreamer..."
meson compile -C "$BUILD_DIR"

    echo "📦 Установка библиотек..."
    meson install -C "$BUILD_DIR"

    # Копируем .so в jniLibs для использования в Android
    if [ -d "$INSTALL_DIR/lib" ]; then
        mkdir -p "$JNILIBS_DIR"
        cp -f "$INSTALL_DIR/lib"/*.so "$JNILIBS_DIR/" 2>/dev/null || true
    fi

    ensure_jnilibs_ready

    echo ""
    echo -e "${GREEN}🎉 GStreamer успешно скомпилирован!${NC}"
    echo "   Библиотеки: $INSTALL_DIR/lib"
    echo "   Для APK: $JNILIBS_DIR"
    exit 0
fi

# Режим 2: Проверка готовых библиотек (shared .so нужны для Go/Fyne, static .a — нет)
# При USE_PREBUILT_GSTREAMER=1 пропускаем статику — приложению нужны .so
if [ "${USE_PREBUILT_GSTREAMER:-0}" != "1" ] && { [ -f "$JNILIBS_DIR/libgstreamer-full-1.0.so" ] || [ -f "$JNILIBS_DIR/libgstreamer-1.0.so" ]; }; then
    echo -e "${GREEN}✓${NC} GStreamer shared библиотеки (.so) уже в android/jniLibs/arm64-v8a"
    ensure_jnilibs_ready
    echo ""
    echo -e "${GREEN}🎉 GStreamer готов к использованию!${NC}"
    echo "   Запустите scripts/build_android.sh для сборки APK"
    exit 0
fi

# Режим 2b: Копирование .so из gstreamer-android-static (если есть shared build)
GST_STATIC_LIB="$REPO_ROOT/gstreamer-android-static/lib/libgstreamer-full-1.0.so"
if [ -f "$GST_STATIC_LIB" ]; then
    echo "📦 Копирование libgstreamer-full-1.0.so из gstreamer-android-static..."
    mkdir -p "$JNILIBS_DIR"
    cp -f "$GST_STATIC_LIB" "$JNILIBS_DIR/"
    # Зависимости
    for dep in libdssim-lib.so libintl.so; do
        if [ -f "$REPO_ROOT/gstreamer-android-static/lib/$dep" ]; then
            cp -f "$REPO_ROOT/gstreamer-android-static/lib/$dep" "$JNILIBS_DIR/"
        fi
    done
    ensure_jnilibs_ready
    echo ""
    echo -e "${GREEN}🎉 GStreamer .so скопированы из gstreamer-android-static!${NC}"
    echo "   Запустите scripts/build_android.sh для сборки APK"
    exit 0
fi

# Режим 3: Скачивание prebuilt GStreamer для Android
if [ "${USE_PREBUILT_GSTREAMER:-0}" = "1" ]; then
    echo -e "${YELLOW}📥 USE_PREBUILT_GSTREAMER=1: скачивание prebuilt GStreamer...${NC}"
else
    echo -e "${YELLOW}📥 Исходники и статические библиотеки не найдены. Скачивание prebuilt GStreamer...${NC}"
fi

GST_VERSION="1.22.12"
GST_URL="https://gstreamer.freedesktop.org/data/pkg/android/$GST_VERSION/gstreamer-1.0-android-universal-$GST_VERSION.tar.xz"
GST_ARCHIVE="$REPO_ROOT/gstreamer-android-$GST_VERSION.tar.xz"
GST_EXTRACT="$REPO_ROOT/gstreamer-android-tmp"

if [ ! -f "$GST_ARCHIVE" ]; then
    echo "   Скачивание $GST_URL ..."
    if command -v curl &>/dev/null; then
        curl -L -o "$GST_ARCHIVE" "$GST_URL" || {
            echo -e "${RED}❌ Ошибка скачивания${NC}"
            rm -f "$GST_ARCHIVE"
            exit 1
        }
    elif command -v wget &>/dev/null; then
        wget -O "$GST_ARCHIVE" "$GST_URL" || {
            echo -e "${RED}❌ Ошибка скачивания${NC}"
            rm -f "$GST_ARCHIVE"
            exit 1
        }
    else
        echo -e "${RED}❌ Установите curl или wget для скачивания${NC}"
        exit 1
    fi
fi

echo "📦 Распаковка..."
mkdir -p "$GST_EXTRACT"
tar -xf "$GST_ARCHIVE" -C "$GST_EXTRACT"

# Prebuilt: ищем arm64-v8a/lib (архив может иметь вложенную структуру)
ARM64_LIB=$(find "$GST_EXTRACT" -type d -path "*/arm64-v8a/lib" 2>/dev/null | head -1)
if [ -n "$ARM64_LIB" ] && [ -d "$ARM64_LIB" ]; then
    mkdir -p "$JNILIBS_DIR"
    cp -f "$ARM64_LIB"/*.so "$JNILIBS_DIR/" 2>/dev/null || true
    echo -e "${GREEN}✓${NC} Скопировано $(ls "$JNILIBS_DIR"/*.so 2>/dev/null | wc -l) .so файлов"
else
    echo -e "${RED}❌ Не найдена директория arm64-v8a/lib в архиве${NC}"
fi

# Копируем libc++_shared.so из NDK
ensure_jnilibs_ready

rm -rf "$GST_EXTRACT"

echo ""
echo -e "${GREEN}🎉 GStreamer prebuilt установлен!${NC}"
echo "   Библиотеки: $JNILIBS_DIR"
echo ""
echo -e "${YELLOW}Примечание: Prebuilt содержит .so (shared). Если проект использует статическую линковку (.a),${NC}"
echo -e "${YELLOW}возможно потребуется адаптация. Запустите scripts/build_android.sh для проверки.${NC}"
