#!/usr/bin/env bash
# Builds the FFmpeg the Linux client links against and bundles: shared
# libavcodec/libavutil/libswscale (+ libavformat/libavdevice/libavfilter/
# libswresample, which they pull in) into $PREFIX.
#
# Why not the distro's: release builds run on an older base (Ubuntu 22.04)
# so the AppImage and .deb start on older glibc too, but 22.04's FFmpeg is
# 4.4 -- different decoder behaviour and missing APIs the client uses. This
# pins the same FFmpeg the client was developed and tested against (Ubuntu
# 24.04's 6.1.x), with the parts of 24.04's configuration the client relies
# on: native H.264/HEVC/AV1/MJPEG decoders plus dav1d for software AV1, with VAAPI, NVDEC (CUDA), VDPAU
# and QSV (oneVPL) hwaccels, and the libx264/NVENC/QSV encoders the camera
# uplink tries (internal/platform/uplink_camera_linux.c).
#
# Usage: build_ffmpeg_linux.sh [PREFIX]   (default: dist/linux/ffmpeg)
# Then: PKG_CONFIG_PATH=$PREFIX/lib/pkgconfig
set -euo pipefail

FFMPEG_VERSION="6.1.1"
FFMPEG_SHA256="8684f4b00f94b85461884c3719382f1261f0d9eb3d59640a1f4ac0873616f968"
NVCODEC_TAG="n12.1.14.0"
# oneVPL dispatcher for QSV; 24.04's version. Built here too because 22.04's
# libvpl-dev ships a vpl.pc with a broken relative -L path.
LIBVPL_TAG="v2023.3.0"
# Software AV1: the native av1 decoder is hwaccel-only, so without dav1d an
# AV1 stream on a GPU with no AV1 decode (e.g. NVIDIA Turing) shows nothing.
# 24.04's version.
DAV1D_TAG="1.4.1"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PREFIX="$(realpath -m "${1:-$REPO_ROOT/dist/linux/ffmpeg}")"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

if [[ -f "$PREFIX/.built-$FFMPEG_VERSION" ]]; then
    echo "FFmpeg $FFMPEG_VERSION already built in $PREFIX"
    exit 0
fi
rm -rf "$PREFIX"
mkdir -p "$PREFIX"

# nv-codec-headers: header-only NVDEC/NVENC/CUDA loader (dlopens the driver).
git clone -q --depth 1 --branch "$NVCODEC_TAG" https://github.com/FFmpeg/nv-codec-headers.git "$WORK/nv-codec-headers"
make -C "$WORK/nv-codec-headers" PREFIX="$PREFIX" install >/dev/null

git clone -q --depth 1 --branch "$LIBVPL_TAG" https://github.com/intel/libvpl.git "$WORK/libvpl"
cmake -S "$WORK/libvpl" -B "$WORK/libvpl/build" -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$PREFIX" \
    -DCMAKE_INSTALL_LIBDIR=lib -DBUILD_TESTS=OFF -DBUILD_EXAMPLES=OFF -DBUILD_TOOLS=OFF -DINSTALL_EXAMPLE_CODE=OFF >/dev/null
cmake --build "$WORK/libvpl/build" -j"$(nproc)" >/dev/null
cmake --install "$WORK/libvpl/build" >/dev/null

git clone -q --depth 1 --branch "$DAV1D_TAG" https://code.videolan.org/videolan/dav1d.git "$WORK/dav1d"
meson setup "$WORK/dav1d/build" "$WORK/dav1d" --prefix="$PREFIX" --libdir=lib --buildtype=release \
    -Denable_tools=false -Denable_tests=false >/dev/null
ninja -C "$WORK/dav1d/build" install >/dev/null

curl -fsSL -o "$WORK/ffmpeg.tar.xz" "https://ffmpeg.org/releases/ffmpeg-$FFMPEG_VERSION.tar.xz"
echo "$FFMPEG_SHA256  $WORK/ffmpeg.tar.xz" | sha256sum -c -
tar -C "$WORK" -xJf "$WORK/ffmpeg.tar.xz"

cd "$WORK/ffmpeg-$FFMPEG_VERSION"
PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}" ./configure \
    --prefix="$PREFIX" \
    --enable-shared --disable-static \
    --disable-doc --disable-debug --disable-programs ${FFMPEG_PROGRAMS:+--enable-ffmpeg} \
    --enable-gpl \
    --enable-libx264 --enable-libdav1d \
    --enable-libdrm --enable-vaapi --enable-vdpau \
    --enable-ffnvcodec --enable-nvdec --enable-nvenc --enable-cuvid \
    --enable-libvpl --disable-libmfx \
    --extra-ldflags="-Wl,-rpath,\$\$ORIGIN"
make -j"$(nproc)" >/dev/null
make install >/dev/null
touch "$PREFIX/.built-$FFMPEG_VERSION"
echo "FFmpeg $FFMPEG_VERSION built into $PREFIX"
