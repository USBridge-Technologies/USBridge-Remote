#!/bin/bash
# Sets up the local ui.parse offload (see internal/localui's package doc
# comment and internal/api/local_ui_intercept.go): installs the ONNX
# Runtime shared library (+ OpenVINO execution provider, for Intel iGPU
# acceleration) into ~/.usbridge/localui/runtime (the default path
# AppConfig.LocalUIParseORTLib resolves to when left empty) and the three
# ONNX models (icon_detect, dbnet, svtr) into both
# internal/localui/models/ (committed to git -- see that directory's
# README for why) and ~/.usbridge/localui/models, so a plain checkout is
# already runnable and re-running this script after an upstream model
# update refreshes the committed copy too.
#
# The runtime lib (~30MB of .so files) stays fetch-on-demand -- unlike the
# models, it's an OS/arch-specific native binary blob, not something a
# git checkout on a different platform could use anyway.
#
# This script is the reproducible record of where the models come from --
# see usbridge/models/ui_parser/README.md's "Provenance" section for how
# the .rknn device-side versions of the same three models were produced;
# these ONNX exports are the same upstream sources (PP-OCRv3 multilingual
# det / cyrillic rec, OmniParser-v2 icon_detect) taken one step earlier in
# that pipeline, before RKNN compilation.
#
# Usage: ./scripts/setup_localui.sh
# Requires: docker (for the Paddle->ONNX conversion stage, reusing
# ../usbridge/tools/ui-parser/docker's cached export image), python3+pip
# (for fetching prebuilt onnxruntime-openvino wheels -- pip packages are
# used purely as a convenient way to obtain the prebuilt .so files, no
# Python runtime is needed afterward).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
USBRIDGE_REPO="${USBRIDGE_REPO:-$REPO_ROOT/../../usbridge}"
OUT_DIR="${LOCALUI_DIR:-$HOME/.usbridge/localui}"
MODELS_DIR="$OUT_DIR/models"
RUNTIME_DIR="$OUT_DIR/runtime"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> Output: $OUT_DIR"
mkdir -p "$MODELS_DIR" "$RUNTIME_DIR"

# ---- 1. ONNX Runtime + OpenVINO EP shared libraries ----------------------
echo "==> Fetching onnxruntime-openvino (prebuilt .so files, Intel CPU+GPU EPs)..."
python3 -m venv "$WORK/venv"
"$WORK/venv/bin/pip" install -q --disable-pip-version-check onnxruntime-openvino
CAPI=$(find "$WORK/venv/lib" -type d -path "*/onnxruntime/capi")
cp "$CAPI"/libonnxruntime.so.* "$RUNTIME_DIR/libonnxruntime.so"
cp "$CAPI"/libonnxruntime_providers_shared.so "$RUNTIME_DIR/"
cp "$CAPI"/libonnxruntime_providers_openvino.so "$RUNTIME_DIR/"
cp "$CAPI"/libopenvino*.so* "$RUNTIME_DIR/" 2>/dev/null || true
echo "    -> $RUNTIME_DIR ($(du -sh "$RUNTIME_DIR" | cut -f1))"

# ---- 2. Models: Paddle (det/rec) -> ONNX via paddle2onnx ------------------
if [ ! -f "$USBRIDGE_REPO/tools/ui-parser/docker/Dockerfile.export" ]; then
    echo "!! Can't find ../usbridge/tools/ui-parser/docker (set USBRIDGE_REPO=...); skipping model export." >&2
    exit 1
fi

echo "==> Fetching PP-OCRv3 multilingual det + cyrillic rec (Paddle inference format)..."
curl -sL -o "$WORK/det.tar" "https://paddleocr.bj.bcebos.com/PP-OCRv3/multilingual/Multilingual_PP-OCRv3_det_infer.tar"
curl -sL -o "$WORK/rec.tar" "https://paddleocr.bj.bcebos.com/PP-OCRv3/multilingual/cyrillic_PP-OCRv3_rec_infer.tar"
tar xf "$WORK/det.tar" -C "$WORK"
tar xf "$WORK/rec.tar" -C "$WORK"

echo "==> Building the export docker image (reused from ../usbridge/tools/ui-parser, cached after first run)..."
# Dockerfile.export's build needs no context files (no COPY) -- point the
# build context at an empty dir instead of tools/ui-parser/docker itself,
# which also holds calibration datasets (hundreds of MB) that would
# otherwise get hashed/sent on every build for no reason.
mkdir -p "$WORK/empty_ctx"
docker build -q -t usbridge-ui-parser-export -f "$USBRIDGE_REPO/tools/ui-parser/docker/Dockerfile.export" "$WORK/empty_ctx" >/dev/null

echo "==> Converting DBNet (det) + SVTR (rec) to ONNX via paddle2onnx..."
docker run --rm -v "$WORK:/work" -w /work -e DEBIAN_FRONTEND=noninteractive usbridge-ui-parser-export \
    bash -c "apt-get update -qq && apt-get install -y -qq libgomp1 >/dev/null 2>&1 && \
    pip install -q --no-cache-dir paddlepaddle==2.6.2 paddle2onnx==1.2.6 >/dev/null 2>&1 && \
    paddle2onnx --model_dir Multilingual_PP-OCRv3_det_infer --model_filename inference.pdmodel --params_filename inference.pdiparams --save_file dbnet.onnx --opset_version 11 --enable_onnx_checker True && \
    paddle2onnx --model_dir cyrillic_PP-OCRv3_rec_infer --model_filename inference.pdmodel --params_filename inference.pdiparams --save_file svtr.onnx --opset_version 11 --enable_onnx_checker True"
cp "$WORK/dbnet.onnx" "$WORK/svtr.onnx" "$MODELS_DIR/"

echo "==> Exporting icon_detect (YOLOv8, OmniParser-v2) to ONNX..."
if [ ! -f "$USBRIDGE_REPO/tools/ui-parser/weights/model.pt" ]; then
    echo "!! ../usbridge/tools/ui-parser/weights/model.pt not found -- run" \
         "'cd $USBRIDGE_REPO/tools/ui-parser && python download_weights.py' first." >&2
    exit 1
fi
cp "$USBRIDGE_REPO/tools/ui-parser/weights/model.pt" "$WORK/model.pt"
cp "$USBRIDGE_REPO/tools/ui-parser/docker/convert_yolo_to_onnx.py" "$WORK/"
docker run --rm -v "$WORK:/work" -w /work usbridge-ui-parser-export \
    python /work/convert_yolo_to_onnx.py --weights /work/model.pt --out /work/icon_detect.onnx --imgsz 640 --opset 12 >/dev/null
cp "$WORK/icon_detect.onnx" "$MODELS_DIR/"

echo "    -> $MODELS_DIR ($(du -sh "$MODELS_DIR" | cut -f1))"

REPO_MODELS_DIR="$REPO_ROOT/internal/localui/models"
cp "$MODELS_DIR/icon_detect.onnx" "$MODELS_DIR/dbnet.onnx" "$MODELS_DIR/svtr.onnx" "$REPO_MODELS_DIR/"
echo "    -> also refreshed committed copy at $REPO_MODELS_DIR (review/commit that diff if this was an upstream model update)"

echo "==> Done. Set local_ui_parse_enabled: true in your config (or AppConfig.LocalUIParseEnabled) to use it."
