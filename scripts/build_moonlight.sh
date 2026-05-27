#!/bin/bash
set -e

# Moonlight-common-c build script for USBridge Client
# This script downloads and compiles the Moonlight core library as a static library.
# It requires CMake and basic C build tools (GCC/Clang/MSVC) to be installed.

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${PROJECT_ROOT}/moonlight-common-c"

echo "==============================================="
echo " Building Moonlight Core (moonlight-common-c)  "
echo "==============================================="

if [ ! -d "${BUILD_DIR}/src" ]; then
    echo "⬇️ Cloning moonlight-common-c..."
    git clone --recursive https://github.com/moonlight-stream/moonlight-common-c.git "${BUILD_DIR}"
else
    echo "✅ moonlight-common-c already cloned."
fi

cd "${BUILD_DIR}"

# Determine OS
OS="$(uname -s)"
case "${OS}" in
    Linux*)     PLATFORM=Linux;;
    Darwin*)    PLATFORM=Mac;;
    CYGWIN*|MINGW*|MSYS*) PLATFORM=Windows;;
    *)          PLATFORM="UNKNOWN:${OS}"
esac

echo "🛠️ Platform detected: ${PLATFORM}"

# Prepare CMake build directory
mkdir -p build
cd build

# Force remove any previously built shared libraries to ensure static linking
rm -f *.dylib *.so *.dll

echo "⚙️ Configuring CMake..."
# We want a static library to embed it directly without requiring users to install .dll/.so/.dylib
cmake .. -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF

echo "🔨 Compiling moonlight-common-c..."
cmake --build . --config Release -j$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 2)

echo "✅ Moonlight Core built successfully."
echo "The static libraries are located in ${BUILD_DIR}/build"
