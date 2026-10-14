#!/bin/bash
# Build PyroWave's native Metal decoder (third_party/pyrowave/metal, a copy of rust-shine's
# crates/pyrowave-sys/metal) as a static archive for the macOS client's decode path
# (internal/service/pyrowave_decode_darwin.m).
#
# No Vulkan and no MoltenVK: the decoder is PyroWave's own, ported from Granite/Vulkan to the
# Metal API, running the same compute shaders translated to Metal Shading Language (compiled at
# runtime by Metal itself). It decodes straight into the IOSurface the renderer shows. The
# bitstream is the one every host encodes (rust-shine on Linux, Windows and macOS, Punktfunk):
# same shaders, block layout and packet parsing.
#
# Build deps: Xcode Command Line Tools (clang++, libtool) only.
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC_DIR="${PROJECT_ROOT}/third_party/pyrowave/metal"
BUILD_DIR="${PROJECT_ROOT}/third_party/pyrowave/build"

echo "==============================================="
echo " Building PyroWave (Metal) for macOS"
echo "==============================================="

OUT_LIB="${BUILD_DIR}/libusbridge-pyrowave.a"
if [ -f "${OUT_LIB}" ] && [ -z "${PYROWAVE_FORCE_REBUILD:-}" ] && \
   [ -z "$(find "${SRC_DIR}" "$0" -newer "${OUT_LIB}" -print -quit)" ]; then
    echo "✅ PyroWave already built."
    exit 0
fi

mkdir -p "${BUILD_DIR}"
# The client's deployment target, so the archive links into the app without warnings.
DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-10.15}"
clang++ -std=c++17 -O2 -fobjc-arc -fPIC -mmacosx-version-min="${DEPLOYMENT_TARGET}" \
    -I"${SRC_DIR}" -c "${SRC_DIR}/pyrowave_metal.mm" -o "${BUILD_DIR}/pyrowave_metal.o"
rm -f "${OUT_LIB}"
libtool -static -o "${OUT_LIB}" "${BUILD_DIR}/pyrowave_metal.o"
echo "✅ PyroWave built: ${OUT_LIB}"
