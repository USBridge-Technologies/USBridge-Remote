#!/bin/bash
set -e

# Moonlight-common-c build script for USBridge Client.
# Host build: always.
# Android arm64 build: when MOONLIGHT_ANDROID_TARGET=1 and ANDROID_NDK_HOME is set.

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${PROJECT_ROOT}/moonlight-common-c"
NCPU="$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 2)"

echo "==============================================="
echo " Building Moonlight Core (moonlight-common-c)  "
echo "==============================================="

if [ ! -d "${BUILD_DIR}/src" ]; then
    echo "⬇️ Initialising moonlight-common-c submodule (pinned commit)..."
    git -C "${PROJECT_ROOT}" submodule update --init --recursive moonlight-common-c
else
    echo "✅ moonlight-common-c already present ($(git -C "${BUILD_DIR}" rev-parse --short HEAD 2>/dev/null || echo 'unknown'))."
fi

# Upstream's CMakeLists.txt calls CHECK_FUNCTION_EXISTS() without including
# the CMake module that defines it, relying on CHECK_LIBRARY_EXISTS(rt ...)
# succeeding first so that branch never runs. Android's bionic libc has no
# librt, so that check always fails there and this branch does run,
# breaking configure with "Unknown CMake command CHECK_FUNCTION_EXISTS" on a
# fresh checkout. One-line local patch, idempotent.
if ! grep -q "include(CheckFunctionExists)" "${BUILD_DIR}/CMakeLists.txt"; then
    sed -i.bak 's/set(CMAKE_EXTRA_INCLUDE_FILES time.h)/include(CheckFunctionExists)\n    set(CMAKE_EXTRA_INCLUDE_FILES time.h)/' "${BUILD_DIR}/CMakeLists.txt"
    rm -f "${BUILD_DIR}/CMakeLists.txt.bak"
fi

# ────────────────────────────────────────────────────────────────────────────
# Android arm64-v8a / API 26
# ────────────────────────────────────────────────────────────────────────────
if [ "${MOONLIGHT_ANDROID_TARGET:-0}" = "1" ] && [ -n "${ANDROID_NDK_HOME:-}" ] && [ -d "${ANDROID_NDK_HOME}" ]; then
    ANDROID_OUT="${BUILD_DIR}/build/android"
    OPENSSL_OUT="${ANDROID_OUT}/openssl"
    NDK_PREBUILT="$(find "${ANDROID_NDK_HOME}/toolchains/llvm/prebuilt" \
        -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort | head -1)"

    mkdir -p "${ANDROID_OUT}"

    echo ""
    echo "=== Android arm64-v8a (API 26) ==="

    # ── Resolve OpenSSL for Android ──────────────────────────────────────────
    # Priority:
    #   1. Already built in build/android/openssl/
    #   2. Pre-built in sibling project (usbridge_client_new_2/gstreamer-android/arm64)
    #   3. Download and build from source

    if [ -f "${OPENSSL_OUT}/lib/libssl.a" ]; then
        echo "⚡ OpenSSL already present at ${OPENSSL_OUT}"
    else
        # Check well-known pre-built locations (sibling project, etc.)
        OPENSSL_PREBUILT=""
#        for candidate in \
#            "${OPENSSL_PREBUILT_DIR:-}" \
#            "${PROJECT_ROOT}/../usbridge_client_new_2/gstreamer-android/arm64" \
#            "${HOME}/Projects/usbridge_client_new_2/gstreamer-android/arm64"
#        do
#            [ -z "$candidate" ] && continue
#            if [ -f "${candidate}/lib/libssl.a" ] && [ -f "${candidate}/include/openssl/ssl.h" ]; then
#                OPENSSL_PREBUILT="$candidate"
#                break
#            fi
#        done

        if [ -n "${OPENSSL_PREBUILT}" ]; then
            echo "📦 Using pre-built OpenSSL from ${OPENSSL_PREBUILT}"
            mkdir -p "${OPENSSL_OUT}/lib" "${OPENSSL_OUT}/include"
            cp "${OPENSSL_PREBUILT}/lib/libssl.a"    "${OPENSSL_OUT}/lib/"
            cp "${OPENSSL_PREBUILT}/lib/libcrypto.a" "${OPENSSL_OUT}/lib/"
            # Symlink or copy include directory (headers are platform-independent)
            if [ ! -d "${OPENSSL_OUT}/include/openssl" ]; then
                cp -r "${OPENSSL_PREBUILT}/include/openssl" "${OPENSSL_OUT}/include/"
            fi
            echo "✅ OpenSSL libs + headers copied"
        else
            # ── Build OpenSSL from source ──────────────────────────────────
            OPENSSL_VERSION="3.3.2"
            OPENSSL_SRC="${BUILD_DIR}/openssl-${OPENSSL_VERSION}"
            echo "📦 Building OpenSSL ${OPENSSL_VERSION} for Android arm64..."

            if [ ! -d "${OPENSSL_SRC}" ]; then
                TARBALL="${BUILD_DIR}/openssl-${OPENSSL_VERSION}.tar.gz"
                if [ ! -f "${TARBALL}" ]; then
                    echo "  Downloading OpenSSL ${OPENSSL_VERSION}..."
                    wget -q "https://www.openssl.org/source/openssl-${OPENSSL_VERSION}.tar.gz" \
                        -O "${TARBALL}" \
                        || curl -fsSL "https://www.openssl.org/source/openssl-${OPENSSL_VERSION}.tar.gz" \
                            -o "${TARBALL}"
                fi
                tar xzf "${TARBALL}" -C "${BUILD_DIR}"
            fi

            cd "${OPENSSL_SRC}"
            ANDROID_NDK_ROOT="${ANDROID_NDK_HOME}" \
            PATH="${NDK_PREBUILT}/bin:${PATH}" \
                ./Configure android-arm64 \
                    -D__ANDROID_API__=26 \
                    --prefix="${OPENSSL_OUT}" \
                    --openssldir="${OPENSSL_OUT}" \
                    no-shared no-tests no-apps \
                    -fPIC
            ANDROID_NDK_ROOT="${ANDROID_NDK_HOME}" \
            PATH="${NDK_PREBUILT}/bin:${PATH}" \
                make clean || true
            ANDROID_NDK_ROOT="${ANDROID_NDK_HOME}" \
            PATH="${NDK_PREBUILT}/bin:${PATH}" \
                make -j"${NCPU}" build_libs
            ANDROID_NDK_ROOT="${ANDROID_NDK_HOME}" \
            PATH="${NDK_PREBUILT}/bin:${PATH}" \
                make install_dev
            cd "${BUILD_DIR}"
            echo "✅ OpenSSL built for Android arm64"
        fi
    fi

    # ── Opus ─────────────────────────────────────────────────────────────────
    OPUS_VERSION="1.5.2"
    OPUS_OUT="${ANDROID_OUT}/opus"

    if [ -f "${OPUS_OUT}/lib/libopus.a" ]; then
        echo "⚡ Opus already present at ${OPUS_OUT}"
    else
        echo "📦 Building Opus ${OPUS_VERSION} for Android arm64..."
        OPUS_SRC="${BUILD_DIR}/opus-${OPUS_VERSION}"

        if [ ! -d "${OPUS_SRC}" ]; then
            OPUS_TARBALL="${BUILD_DIR}/opus-${OPUS_VERSION}.tar.gz"
            if [ ! -f "${OPUS_TARBALL}" ]; then
                echo "  Downloading Opus ${OPUS_VERSION}..."
                wget -q "https://downloads.xiph.org/releases/opus/opus-${OPUS_VERSION}.tar.gz" \
                    -O "${OPUS_TARBALL}" \
                    || curl -fsSL "https://downloads.xiph.org/releases/opus/opus-${OPUS_VERSION}.tar.gz" \
                        -o "${OPUS_TARBALL}"
            fi
            tar xzf "${OPUS_TARBALL}" -C "${BUILD_DIR}"
        fi

        OPUS_CMAKE_BUILD="${BUILD_DIR}/opus-cmake-build"
        mkdir -p "${OPUS_CMAKE_BUILD}"

        cmake "${OPUS_SRC}" \
            -B "${OPUS_CMAKE_BUILD}" \
            -DCMAKE_TOOLCHAIN_FILE="${ANDROID_NDK_HOME}/build/cmake/android.toolchain.cmake" \
            -DANDROID_ABI=arm64-v8a \
            -DANDROID_PLATFORM=android-26 \
            -DBUILD_SHARED_LIBS=OFF \
            -DCMAKE_BUILD_TYPE=Release \
            -DOPUS_BUILD_TESTING=OFF \
            -DOPUS_BUILD_PROGRAMS=OFF \
            -DCMAKE_INSTALL_PREFIX="${OPUS_OUT}"

        cmake --build "${OPUS_CMAKE_BUILD}" -j"${NCPU}"
        cmake --install "${OPUS_CMAKE_BUILD}"

        echo "✅ Opus ${OPUS_VERSION} built for Android arm64"
        echo "   Outputs: ${OPUS_OUT}/"
    fi

    # ── moonlight-common-c ────────────────────────────────────────────────────
    if [ ! -f "${ANDROID_OUT}/libmoonlight-common-c.a" ]; then
        echo "⚙️ Cross-compiling moonlight-common-c for Android arm64..."
        ANDROID_CMAKE_BUILD="${ANDROID_OUT}/cmake-build"
        mkdir -p "${ANDROID_CMAKE_BUILD}"

        # MOONLIGHT_CMAKE_BUILD_TYPE lets a one-off debug build opt into
        # moonlight-common-c's own BUILD_TYPE=XDEBUG (see its CMakeLists.txt
        # -- it derives BUILD_TYPE from CMAKE_BUILD_TYPE via
        # `string(TOUPPER "x${CMAKE_BUILD_TYPE}" BUILD_TYPE)`, so passing
        # CMAKE_BUILD_TYPE=Debug is what's actually needed, not a
        # standalone -DBUILD_TYPE=). CMAKE_BUILD_TYPE=Debug is what compiles
        # in LC_DEBUG and, with it, RtpVideoQueue.c's FEC_VALIDATION_MODE
        # (synthetic per-frame drop + recovery, for exercising real
        # FEC-recovery smoothness without touching the actual network).
        # Defaults to Release, this project's normal shipping configuration.
        cmake "${BUILD_DIR}" \
            -B "${ANDROID_CMAKE_BUILD}" \
            -DCMAKE_TOOLCHAIN_FILE="${ANDROID_NDK_HOME}/build/cmake/android.toolchain.cmake" \
            -DANDROID_ABI=arm64-v8a \
            -DANDROID_PLATFORM=android-26 \
            -DBUILD_SHARED_LIBS=OFF \
            -DCMAKE_BUILD_TYPE="${MOONLIGHT_CMAKE_BUILD_TYPE:-Release}" \
            -DOPENSSL_ROOT_DIR="${OPENSSL_OUT}" \
            -DOPENSSL_INCLUDE_DIR="${OPENSSL_OUT}/include" \
            -DOPENSSL_CRYPTO_LIBRARY="${OPENSSL_OUT}/lib/libcrypto.a" \
            -DOPENSSL_SSL_LIBRARY="${OPENSSL_OUT}/lib/libssl.a"

        cmake --build "${ANDROID_CMAKE_BUILD}" -j"${NCPU}"

        cp "${ANDROID_CMAKE_BUILD}/libmoonlight-common-c.a" "${ANDROID_OUT}/"
        cp "${ANDROID_CMAKE_BUILD}/enet/libenet.a"           "${ANDROID_OUT}/"
        cp "${OPENSSL_OUT}/lib/libssl.a"                     "${ANDROID_OUT}/"
        cp "${OPENSSL_OUT}/lib/libcrypto.a"                  "${ANDROID_OUT}/"

        echo "✅ moonlight-common-c built for Android arm64"
        echo "   Outputs: ${ANDROID_OUT}/"
    else
        echo "⚡ moonlight-common-c already built for Android"
    fi
fi

# ────────────────────────────────────────────────────────────────────────────
# iOS arm64 build (MOONLIGHT_IOS_TARGET=1)
# ────────────────────────────────────────────────────────────────────────────
if [ "${MOONLIGHT_IOS_TARGET:-0}" = "1" ]; then
    IOS_DEPLOY_TARGET="16.0"
    IOS_OUT="${BUILD_DIR}/build/ios"
    IOS_SDK="$(xcrun --sdk iphoneos --show-sdk-path)"
    IOS_CLANG="$(xcrun --sdk iphoneos -f clang)"
    IOS_AR="$(xcrun --sdk iphoneos -f ar)"
    IOS_RANLIB="$(xcrun --sdk iphoneos -f ranlib)"

    mkdir -p "${IOS_OUT}"

    echo ""
    echo "=== iOS arm64 (deployment target ${IOS_DEPLOY_TARGET}) ==="

    IOS_OPENSSL_OUT="${IOS_OUT}/openssl"
    if [ -f "${IOS_OPENSSL_OUT}/lib/libssl.a" ]; then
        :
    else
        OPENSSL_VERSION="3.3.2"
        OPENSSL_SRC="${BUILD_DIR}/openssl-${OPENSSL_VERSION}"
        if [ ! -d "${OPENSSL_SRC}" ]; then
            TARBALL="${BUILD_DIR}/openssl-${OPENSSL_VERSION}.tar.gz"
            if [ ! -f "${TARBALL}" ]; then
                echo "  Downloading OpenSSL ${OPENSSL_VERSION}..."
                curl -fsSL "https://www.openssl.org/source/openssl-${OPENSSL_VERSION}.tar.gz" -o "${TARBALL}"
            fi
            tar xzf "${TARBALL}" -C "${BUILD_DIR}"
        fi

        echo "📦 Building OpenSSL ${OPENSSL_VERSION} for iOS arm64..."
        mkdir -p "${IOS_OPENSSL_OUT}"
        cd "${OPENSSL_SRC}"
        IPHONEOS_DEPLOYMENT_TARGET="${IOS_DEPLOY_TARGET}" \
            ./Configure ios64-xcrun \
                --prefix="${IOS_OPENSSL_OUT}" \
                --openssldir="${IOS_OPENSSL_OUT}" \
                no-shared no-tests no-apps \
                -fPIC
        IPHONEOS_DEPLOYMENT_TARGET="${IOS_DEPLOY_TARGET}" \
            make -j"${NCPU}"
        IPHONEOS_DEPLOYMENT_TARGET="${IOS_DEPLOY_TARGET}" \
            make install_dev
        cd "${BUILD_DIR}"
        echo "✅ OpenSSL built for iOS"
    fi

    IOS_OPUS_OUT="${IOS_OUT}/opus"
    if [ -f "${IOS_OPUS_OUT}/lib/libopus.a" ]; then
        :
    else
        OPUS_VERSION="1.5.2"
        OPUS_SRC="${BUILD_DIR}/opus-${OPUS_VERSION}"
        if [ ! -d "${OPUS_SRC}" ]; then
            OPUS_TARBALL="${BUILD_DIR}/opus-${OPUS_VERSION}.tar.gz"
            if [ ! -f "${OPUS_TARBALL}" ]; then
                echo "  Downloading Opus ${OPUS_VERSION}..."
                curl -fsSL "https://downloads.xiph.org/releases/opus/opus-${OPUS_VERSION}.tar.gz" -o "${OPUS_TARBALL}"
            fi
            tar xzf "${OPUS_TARBALL}" -C "${BUILD_DIR}"
        fi

        echo "📦 Building Opus ${OPUS_VERSION} for iOS arm64..."
        IOS_OPUS_BUILD="${BUILD_DIR}/opus-ios-build"
        mkdir -p "${IOS_OPUS_BUILD}"
        cmake "${OPUS_SRC}" \
            -B "${IOS_OPUS_BUILD}" \
            -DCMAKE_SYSTEM_NAME=iOS \
            -DCMAKE_OSX_SYSROOT="${IOS_SDK}" \
            -DCMAKE_OSX_ARCHITECTURES=arm64 \
            -DCMAKE_OSX_DEPLOYMENT_TARGET="${IOS_DEPLOY_TARGET}" \
            -DCMAKE_C_COMPILER="${IOS_CLANG}" \
            -DCMAKE_AR="${IOS_AR}" \
            -DCMAKE_RANLIB="${IOS_RANLIB}" \
            -DBUILD_SHARED_LIBS=OFF \
            -DCMAKE_BUILD_TYPE=Release \
            -DOPUS_BUILD_TESTING=OFF \
            -DOPUS_BUILD_PROGRAMS=OFF \
            -DCMAKE_INSTALL_PREFIX="${IOS_OPUS_OUT}" \
            -DCMAKE_C_COMPILER_WORKS=TRUE
        cmake --build "${IOS_OPUS_BUILD}" -j"${NCPU}"
        cmake --install "${IOS_OPUS_BUILD}"
        echo "✅ Opus built for iOS"
    fi

    if [ -f "${IOS_OUT}/libmoonlight-common-c.a" ]; then
        :
    else
        echo "⚙️ Cross-compiling moonlight-common-c for iOS arm64..."
        IOS_MC_BUILD="${IOS_OUT}/cmake-build"
        mkdir -p "${IOS_MC_BUILD}"
        cmake "${BUILD_DIR}" \
            -B "${IOS_MC_BUILD}" \
            -DCMAKE_SYSTEM_NAME=iOS \
            -DCMAKE_OSX_SYSROOT="${IOS_SDK}" \
            -DCMAKE_OSX_ARCHITECTURES=arm64 \
            -DCMAKE_OSX_DEPLOYMENT_TARGET="${IOS_DEPLOY_TARGET}" \
            -DCMAKE_C_COMPILER="${IOS_CLANG}" \
            -DCMAKE_AR="${IOS_AR}" \
            -DCMAKE_RANLIB="${IOS_RANLIB}" \
            -DBUILD_SHARED_LIBS=OFF \
            -DCMAKE_BUILD_TYPE=Release \
            -DCMAKE_C_COMPILER_WORKS=TRUE \
            -DOPENSSL_ROOT_DIR="${IOS_OPENSSL_OUT}" \
            -DOPENSSL_INCLUDE_DIR="${IOS_OPENSSL_OUT}/include" \
            -DOPENSSL_CRYPTO_LIBRARY="${IOS_OPENSSL_OUT}/lib/libcrypto.a" \
            -DOPENSSL_SSL_LIBRARY="${IOS_OPENSSL_OUT}/lib/libssl.a"
        cmake --build "${IOS_MC_BUILD}" -j"${NCPU}"

        cp "${IOS_MC_BUILD}/libmoonlight-common-c.a" "${IOS_OUT}/"
        cp "${IOS_MC_BUILD}/enet/libenet.a"           "${IOS_OUT}/"
        cp "${IOS_OPENSSL_OUT}/lib/libssl.a"          "${IOS_OUT}/"
        cp "${IOS_OPENSSL_OUT}/lib/libcrypto.a"       "${IOS_OUT}/"
        cp "${IOS_OPUS_OUT}/lib/libopus.a"            "${IOS_OUT}/"
        echo "✅ moonlight-common-c built for iOS"
        echo "   Outputs: ${IOS_OUT}/"
    fi
fi

# ────────────────────────────────────────────────────────────────────────────
# ────────────────────────────────────────────────────────────────────────────
if [ "${MOONLIGHT_SKIP_HOST:-0}" = "1" ]; then
    echo ""
    exit 0
fi

OS="$(uname -s)"
case "${OS}" in
    Linux*)   PLATFORM=Linux;;
    Darwin*)  PLATFORM=Mac;;
    CYGWIN*|MINGW*|MSYS*) PLATFORM=Windows;;
    *)        PLATFORM="UNKNOWN:${OS}";;
esac

echo ""
echo "🛠️ Host platform: ${PLATFORM}"

HOST_BUILD="${BUILD_DIR}/build"
mkdir -p "${HOST_BUILD}"
cd "${HOST_BUILD}"

rm -f ./*.dylib ./*.so ./*.dll

echo "⚙️ Configuring CMake (host)..."
# Detect Homebrew OpenSSL so cmake can find the include directory
OPENSSL_ROOT=""
for _d in /opt/homebrew/opt/openssl@3 /usr/local/opt/openssl@3 \
           /opt/homebrew/opt/openssl   /usr/local/opt/openssl; do
    [ -d "$_d" ] && OPENSSL_ROOT="$_d" && break
done
cmake "${BUILD_DIR}" -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
    ${OPENSSL_ROOT:+-DOPENSSL_ROOT_DIR="$OPENSSL_ROOT"}

echo "🔨 Compiling moonlight-common-c (host)..."
cmake --build . --config Release -j"${NCPU}"

echo "✅ Moonlight Core (host) built successfully."
echo "   Libraries: ${HOST_BUILD}/"
