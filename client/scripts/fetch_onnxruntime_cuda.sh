#!/bin/bash
# fetch_onnxruntime_cuda.sh [output_dir]
#
# Installs the onnxruntime-gpu build (core + CUDA execution provider) for
# local ui.parse on Linux into ~/.usbridge/localui/runtime-cuda. No sudo.
# The CUDA 12 toolkit libs (cudart/cublas/cufft/curand) and cuDNN 9 are NOT
# downloaded here: install them from the OS packages
# (sudo apt install nvidia-cuda-toolkit + a cuDNN 9 for CUDA 12 package) and
# the dynamic loader finds them system-wide.
#
# internal/api/local_ui_init.go prefers this directory over the default
# (OpenVINO) runtime when an NVIDIA driver is loaded, and
# internal/localui/onnx.go's acceleratorEP then picks the CUDA EP. The
# OpenVINO GPU plugin is Intel-only: on an NVIDIA box it binds NVIDIA's
# OpenCL device, where icon_detect runs but dbnet/svtr fail at inference.
#
# ~250 MB download, ~370 MB on disk.
set -euo pipefail

ORT_VERSION="${ORT_VERSION:-1.24.1}"   # must match the libonnxruntime the app bundles
OUT="${1:-$HOME/.usbridge/localui/runtime-cuda}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$OUT"
python3 -m pip download --no-deps -q -d "$WORK" \
    "onnxruntime-gpu==${ORT_VERSION}"

for whl in "$WORK"/*.whl; do
    unzip -q -o "$whl" -d "$WORK/x"
done

# onnxruntime core + providers
cp -f "$WORK"/x/onnxruntime/capi/libonnxruntime.so."${ORT_VERSION}" "$OUT/"
ln -sf "libonnxruntime.so.${ORT_VERSION}" "$OUT/libonnxruntime.so"
cp -f "$WORK"/x/onnxruntime/capi/libonnxruntime_providers_{shared,cuda}.so "$OUT/"

echo "CUDA pack installed in $OUT ($(du -sh "$OUT" | cut -f1))"
