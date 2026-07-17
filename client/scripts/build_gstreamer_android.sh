#!/bin/bash

# Script that prepares GStreamer for Android:
# - Build from source (if gstreamer/ exists)
# - Or check/install prebuilt libraries
#
# NOTE: GStreamer is NOT required for Moonlight streaming on Android.
#   Moonlight uses AMediaCodec (NDK system API) for hardware H.264 decode
#   and AAudio (NDK system API) for audio output — both are built into Android NDK.
#   This script is only needed if you use the legacy RTP/GStreamer video mode.

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "🔧 Preparing GStreamer for Android..."

# Paths
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

extract_prebuilt_arm64_shared_libs() {
    local archive_path="$1"
    local extract_dir="$2"
    local entry=""
    local entries=()

    mkdir -p "$extract_dir"

    while IFS= read -r entry; do
        [ -n "$entry" ] && entries+=("$entry")
    done < <(tar -tf "$archive_path" | grep -E '^arm64/lib/[^/]+\.so(\..*)?$' || true)

    if [ ${#entries[@]} -eq 0 ]; then
        while IFS= read -r entry; do
            [ -n "$entry" ] && entries+=("$entry")
        done < <(tar -tf "$archive_path" | grep -E '(^|.*/)arm64-v8a/lib/[^/]+\.so(\..*)?$' || true)
    fi

    if [ ${#entries[@]} -eq 0 ]; then
        return 1
    fi

    tar -xf "$archive_path" -C "$extract_dir" "${entries[@]}"
}

archive_contains_arm64_shared_libs() {
    local archive_path="$1"

    tar -tf "$archive_path" | grep -Eq '^arm64/lib/[^/]+\.so(\..*)?$|(^|.*/)arm64-v8a/lib/[^/]+\.so(\..*)?$'
}

export_android_env
NDK_PATH="$(resolve_android_ndk 2>/dev/null || true)"
if [ -z "$NDK_PATH" ]; then
    echo -e "${RED}❌ Android NDK not found. Install it via Android Studio: SDK Manager → SDK Tools → NDK${NC}"
    exit 1
fi

if [ ! -d "$NDK_PATH" ]; then
    printf "%b❌ Android NDK not found: %s%b\n" "$RED" "$NDK_PATH" "$NC"
    exit 1
fi

printf "%b✓%b Android NDK found: %s\n" "$GREEN" "$NC" "$NDK_PATH"

if ! setup_android_ndk_toolchain_env "$NDK_PATH" 28; then
    printf "%b❌ Failed to prepare the toolchain from NDK: %s%b\n" "$RED" "$NDK_PATH" "$NC"
    exit 1
fi

# flex is required to build GStreamer's parsers
if ! command -v flex >/dev/null 2>&1; then
    echo -e "${RED}❌ flex not found${NC}"
    print_flex_install_hint
    echo "   Or use the prebuilt: USE_PREBUILT_GSTREAMER=1 scripts/build_android.sh"
    exit 1
fi

# Check whether prebuilt GStreamer libraries are already in jniLibs
ensure_jnilibs_ready() {
    mkdir -p "$JNILIBS_DIR"

    # Copy libc++_shared.so from the NDK (required for Android)
    NDK_PREBUILT="$NDK_PATH/toolchains/llvm/prebuilt"
    if [ -d "$NDK_PREBUILT" ]; then
        NDK_CPP_SHARED=$(find "$NDK_PREBUILT" -path "*/lib/aarch64-linux-android/libc++_shared.so" 2>/dev/null | head -1)
        if [ -n "$NDK_CPP_SHARED" ] && [ -f "$NDK_CPP_SHARED" ]; then
            cp -f "$NDK_CPP_SHARED" "$JNILIBS_DIR/"
            echo -e "${GREEN}✓${NC} libc++_shared.so copied from NDK"
        else
            echo -e "${YELLOW}⚠${NC} libc++_shared.so not found in NDK"
        fi
    else
        echo -e "${YELLOW}⚠${NC} NDK prebuilt not found"
    fi

    SO_COUNT=$(find "$JNILIBS_DIR" -maxdepth 1 -name "*.so" 2>/dev/null | wc -l)
    A_COUNT=$(find "$JNILIBS_DIR" -maxdepth 1 -name "*.a" 2>/dev/null | wc -l)
    echo -e "${GREEN}✓${NC} In android/jniLibs/arm64-v8a: $SO_COUNT .so, $A_COUNT .a"

    ensure_dist_copy
}

# Mode 1: Build from source (if gstreamer/ exists and prebuilt wasn't requested)
# USE_PREBUILT_GSTREAMER=1 — skip building from source, download prebuilt instead
if [ -d "$GSTREAMER_DIR" ] && [ "${USE_PREBUILT_GSTREAMER:-0}" != "1" ]; then
echo "📦 Building GStreamer from source..."
    cd "$GSTREAMER_DIR"

if meson_builddir_needs_reset "$BUILD_DIR"; then
    echo -e "${YELLOW}⚠${NC} Found a stale Meson cache from another platform. Recreating $BUILD_DIR"
    rm -rf "$BUILD_DIR"
fi

# Generate the cross-file based on the NDK (overwritten so it doesn't carry over darwin paths)
NDK_PREBUILT=$(find "$NDK_PATH/toolchains/llvm/prebuilt" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | head -1)
if [ -z "$NDK_PREBUILT" ] || [ ! -d "$NDK_PREBUILT" ]; then
    echo -e "${RED}❌ prebuilt not found in NDK: $NDK_PATH${NC}"
    exit 1
fi
NDK_BIN="$NDK_PREBUILT/bin"
NDK_SYSROOT="$NDK_PREBUILT/sysroot"
CROSS_FILE="$REPO_ROOT/android-arm64.txt"
echo "📝 Updating cross-file: $CROSS_FILE"
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

echo "📦 Configuring Meson for Android arm64-v8a..."
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

# Fix permissions on glib-mkenums if Meson created it without +x
if [ -d "$BUILD_DIR" ]; then
        :
    find "$BUILD_DIR" -type f -name "glib-mkenums" -exec chmod +x {} + 2>/dev/null || true
fi

echo "🔨 Compiling GStreamer..."
meson compile -C "$BUILD_DIR"

    echo "📦 Installing libraries..."
    meson install -C "$BUILD_DIR"

    # Copy .so files into jniLibs for use on Android
    if [ -d "$INSTALL_DIR/lib" ]; then
        mkdir -p "$JNILIBS_DIR"
        cp -f "$INSTALL_DIR/lib"/*.so "$JNILIBS_DIR/" 2>/dev/null || true
    fi

    ensure_jnilibs_ready

    echo ""
    echo -e "${GREEN}🎉 GStreamer compiled successfully!${NC}"
    echo "   Libraries: $INSTALL_DIR/lib"
    echo "   For the APK: $JNILIBS_DIR"
    exit 0
fi

# Mode 2: Check for prebuilt libraries (shared .so needed for Go/Fyne, static .a is not)
# With USE_PREBUILT_GSTREAMER=1 we skip static libs — the app needs .so files
if [ "${USE_PREBUILT_GSTREAMER:-0}" != "1" ] && { [ -f "$JNILIBS_DIR/libgstreamer-full-1.0.so" ] || [ -f "$JNILIBS_DIR/libgstreamer-1.0.so" ]; }; then
    echo -e "${GREEN}✓${NC} GStreamer shared libraries (.so) are already in android/jniLibs/arm64-v8a"
    ensure_jnilibs_ready
    echo ""
    echo -e "${GREEN}🎉 GStreamer is ready to use!${NC}"
    echo "   Run scripts/build_android.sh to build the APK"
    exit 0
fi

# Mode 2b: Copy .so from gstreamer-android-static (if a shared build exists)
GST_STATIC_LIB="$REPO_ROOT/gstreamer-android-static/lib/libgstreamer-full-1.0.so"
if [ -f "$GST_STATIC_LIB" ]; then
    echo "📦 Copying libgstreamer-full-1.0.so from gstreamer-android-static..."
    mkdir -p "$JNILIBS_DIR"
    cp -f "$GST_STATIC_LIB" "$JNILIBS_DIR/"
    # Dependencies
    for dep in libdssim-lib.so libintl.so; do
        if [ -f "$REPO_ROOT/gstreamer-android-static/lib/$dep" ]; then
            cp -f "$REPO_ROOT/gstreamer-android-static/lib/$dep" "$JNILIBS_DIR/"
        fi
    done
    ensure_jnilibs_ready
    echo ""
    echo -e "${GREEN}🎉 GStreamer .so copied from gstreamer-android-static!${NC}"
    echo "   Run scripts/build_android.sh to build the APK"
    exit 0
fi

# Mode 3: Download prebuilt GStreamer for Android
if [ "${USE_PREBUILT_GSTREAMER:-0}" = "1" ]; then
    echo -e "${YELLOW}📥 USE_PREBUILT_GSTREAMER=1: downloading prebuilt GStreamer...${NC}"
else
    echo -e "${YELLOW}📥 No source or static libraries found. Downloading prebuilt GStreamer...${NC}"
fi

GST_VERSION="1.22.12"
GST_URL="https://gstreamer.freedesktop.org/data/pkg/android/$GST_VERSION/gstreamer-1.0-android-universal-$GST_VERSION.tar.xz"
GST_ARCHIVE="$REPO_ROOT/gstreamer-android-$GST_VERSION.tar.xz"
GST_EXTRACT="$REPO_ROOT/gstreamer-android-tmp"

if [ ! -f "$GST_ARCHIVE" ]; then
    echo "   Downloading $GST_URL ..."
    if command -v curl &>/dev/null; then
        curl -L -o "$GST_ARCHIVE" "$GST_URL" || {
            echo -e "${RED}❌ Download error${NC}"
            rm -f "$GST_ARCHIVE"
            exit 1
        }
    elif command -v wget &>/dev/null; then
        wget -O "$GST_ARCHIVE" "$GST_URL" || {
            echo -e "${RED}❌ Download error${NC}"
            rm -f "$GST_ARCHIVE"
            exit 1
        }
    else
        echo -e "${RED}❌ Install curl or wget to download${NC}"
        exit 1
    fi
else
    echo "   Using already-downloaded archive: $GST_ARCHIVE"
fi

if ! archive_contains_arm64_shared_libs "$GST_ARCHIVE"; then
    echo -e "${RED}❌ The prebuilt archive does not contain the arm64 shared libraries (.so) this project needs${NC}"
    echo "   The archive is kept locally and will not be re-downloaded: $GST_ARCHIVE"
    echo "   Building requires the gst-build repository at: $GSTREAMER_DIR"
    echo "   Then run: scripts/build_gstreamer_dynamic_android.sh"
    exit 1
fi

echo "📦 Extracting..."
mkdir -p "$GST_EXTRACT"

if ! extract_prebuilt_arm64_shared_libs "$GST_ARCHIVE" "$GST_EXTRACT"; then
    echo -e "${RED}❌ Failed to extract arm64 shared libraries from the GStreamer archive${NC}"
    echo "   The archive is kept locally and will not be re-downloaded: $GST_ARCHIVE"
    exit 1
fi

# Prebuilt: GStreamer archives usually contain arm64/lib, but keep a fallback
ARM64_LIB=""
for candidate in \
    "$GST_EXTRACT/arm64/lib" \
    "$(find "$GST_EXTRACT" -type d -path "*/arm64-v8a/lib" 2>/dev/null | head -1)"; do
    if [ -n "$candidate" ] && [ -d "$candidate" ]; then
        ARM64_LIB="$candidate"
        break
    fi
done

if [ -n "$ARM64_LIB" ] && [ -d "$ARM64_LIB" ]; then
    mkdir -p "$JNILIBS_DIR"
    cp -f "$ARM64_LIB"/*.so* "$JNILIBS_DIR/" 2>/dev/null || true
    echo -e "${GREEN}✓${NC} Copied $(find "$JNILIBS_DIR" -maxdepth 1 -type f -name '*.so*' 2>/dev/null | wc -l) .so* files"
else
    echo -e "${RED}❌ Could not find an arm64/lib or arm64-v8a/lib directory in the archive${NC}"
fi

# Copy libc++_shared.so from the NDK
ensure_jnilibs_ready

rm -rf "$GST_EXTRACT"

echo ""
echo -e "${GREEN}🎉 GStreamer prebuilt installed!${NC}"
echo "   Libraries: $JNILIBS_DIR"
echo ""
echo -e "${YELLOW}Note: the prebuilt contains .so (shared) libraries. If the project uses static linking (.a),${NC}"
echo -e "${YELLOW}some adaptation may be needed. Run scripts/build_android.sh to verify.${NC}"
