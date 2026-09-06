#!/bin/bash
# fetch_onnxruntime.sh <output_dir>
#
# Downloads a redistributable ONNX Runtime shared library (CPU execution
# provider only) for the *build host's* platform, via the official PyPI
# wheel, and drops it as <output_dir>/<internal/localui.DefaultRuntimeLibName()>
# -- ready to be bundled directly into an app package (build_macos.sh's
# Contents/Frameworks/, an AppImage's lib dir, next to a Windows .exe, ...).
#
# Why the PyPI wheel and not Homebrew/apt/a system package: those link
# against whatever version of protobuf/abseil/etc. happens to be installed
# on the *build* machine at that moment, which is exactly the trap this
# script exists to avoid -- `brew upgrade protobuf` on a dev box silently
# broke a Homebrew-linked libonnxruntime.dylib that used to work, because
# it was never protobuf-version-pinned in the first place (see git log for
# the incident this script was added to fix). PyPI's manylinux/macOS wheels
# are built to be self-contained redistributables: `otool -L`/`ldd` on the
# extracted library shows only OS-provided system libraries as
# dependencies, nothing under /opt/homebrew or a distro package -- verified
# for the macOS arm64 wheel before wiring this into build_macos.sh.
#
# This intentionally does NOT install the OpenVINO execution provider
# (onnxruntime-openvino, Linux/Intel-iGPU only) -- that stays an opt-in
# developer enhancement via setup_localui.sh's existing flow (which needs
# real system access to pick the right Intel driver stack). This script's
# job is narrower: guarantee a bundled app has a *working baseline CPU*
# ONNX Runtime with zero assumptions about what's installed on the machine
# it ends up running on, matching the ONNX models already committed at
# internal/localui/models/ (see that directory's README).
#
# Usage: ./scripts/fetch_onnxruntime.sh <output_dir>
# Requires: python3 (pip) -- no docker, no network access beyond PyPI, no
# access to the private usbridge (device) repo.
set -euo pipefail

OUT_DIR="${1:?usage: fetch_onnxruntime.sh <output_dir>}"
mkdir -p "$OUT_DIR"

case "$(uname -s)" in
    Darwin) LIB_NAME="libonnxruntime.dylib" ;;
    Linux)  LIB_NAME="libonnxruntime.so" ;;
    *)
        echo "!! fetch_onnxruntime.sh: unsupported build host $(uname -s) -- add a case here" >&2
        exit 1
        ;;
esac

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> Fetching onnxruntime (PyPI, CPU EP, self-contained) for $(uname -s)/$(uname -m)..."
python3 -m venv "$WORK/venv"
"$WORK/venv/bin/pip" install -q --disable-pip-version-check --target "$WORK/pkg" onnxruntime

SRC="$(find "$WORK/pkg/onnxruntime/capi" -maxdepth 1 -name "libonnxruntime.*.${LIB_NAME##*.}" -print -quit)"
if [ -z "$SRC" ]; then
    echo "!! fetch_onnxruntime.sh: no libonnxruntime.*.${LIB_NAME##*.} found under the onnxruntime wheel's capi/ dir" >&2
    exit 1
fi

cp "$SRC" "$OUT_DIR/$LIB_NAME"
chmod 755 "$OUT_DIR/$LIB_NAME"
echo "    -> $OUT_DIR/$LIB_NAME ($(du -h "$OUT_DIR/$LIB_NAME" | cut -f1), from $(basename "$SRC"))"
