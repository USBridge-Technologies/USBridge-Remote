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
    echo "⬇️ Cloning moonlight-common-c..."
    git clone --recursive https://github.com/moonlight-stream/moonlight-common-c.git "${BUILD_DIR}"
else
    echo "✅ moonlight-common-c already cloned."
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
        for candidate in \
            "${OPENSSL_PREBUILT_DIR:-}" \
            "${PROJECT_ROOT}/../usbridge_client_new_2/gstreamer-android/arm64" \
            "${HOME}/Projects/usbridge_client_new_2/gstreamer-android/arm64"
        do
            [ -z "$candidate" ] && continue
            if [ -f "${candidate}/lib/libssl.a" ] && [ -f "${candidate}/include/openssl/ssl.h" ]; then
                OPENSSL_PREBUILT="$candidate"
                break
            fi
        done

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
                make -j"${NCPU}" build_libs
            ANDROID_NDK_ROOT="${ANDROID_NDK_HOME}" \
            PATH="${NDK_PREBUILT}/bin:${PATH}" \
                make install_dev
            cd "${BUILD_DIR}"
            echo "✅ OpenSSL built for Android arm64"
        fi
    fi

    # ── moonlight-common-c ────────────────────────────────────────────────────
    if [ ! -f "${ANDROID_OUT}/libmoonlight-common-c.a" ]; then
        echo "⚙️ Cross-compiling moonlight-common-c for Android arm64..."
        ANDROID_CMAKE_BUILD="${ANDROID_OUT}/cmake-build"
        mkdir -p "${ANDROID_CMAKE_BUILD}"

        cmake "${BUILD_DIR}" \
            -B "${ANDROID_CMAKE_BUILD}" \
            -DCMAKE_TOOLCHAIN_FILE="${ANDROID_NDK_HOME}/build/cmake/android.toolchain.cmake" \
            -DANDROID_ABI=arm64-v8a \
            -DANDROID_PLATFORM=android-26 \
            -DBUILD_SHARED_LIBS=OFF \
            -DCMAKE_BUILD_TYPE=Release \
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
# Host build
# ────────────────────────────────────────────────────────────────────────────
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
cmake "${BUILD_DIR}" -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF

echo "🔨 Compiling moonlight-common-c (host)..."
cmake --build . --config Release -j"${NCPU}"

echo "✅ Moonlight Core (host) built successfully."
echo "   Libraries: ${HOST_BUILD}/"
