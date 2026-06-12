#!/bin/bash
# Fast incremental rebuild — Go only, no DLL copy, no fyne package.
# Use from UCRT64 shell: ./scripts/fast_rebuild.sh
# Run time: ~15-30s on cached build (only changed packages recompile).
#
# For a full dist rebuild with all DLLs: ./scripts/build_windows.sh
set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXE_OUT="$REPO_ROOT/.cache/build/windows-amd64/release/USBridge_Client.exe"
DIST_EXE="$REPO_ROOT/dist/windows/USBridge_Client.exe"

echo "==> fast_rebuild: go build..."
cd "$REPO_ROOT/cmd"
export CGO_ENABLED=1
export GOOS=windows
export GOARCH=amd64
export GOCACHE="${GOCACHE:-$REPO_ROOT/.cache/go-build/windows-amd64}"
export GOMODCACHE="${GOMODCACHE:-$REPO_ROOT/.cache/go-mod}"
export GOMAXPROCS="${GOMAXPROCS:-12}"

# Vulkan + GStreamer headers (ucrt64 native build)
UCRT64_INC="${UCRT64_INC:-/ucrt64/include}"
UCRT64_LIB="${UCRT64_LIB:-/ucrt64/lib}"
# Fallback to msys64 path when running outside ucrt64 shell
[ -d "$UCRT64_INC" ] || UCRT64_INC="C:/msys64/ucrt64/include"
[ -d "$UCRT64_LIB" ] || UCRT64_LIB="C:/msys64/ucrt64/lib"
export CGO_CFLAGS="-I${UCRT64_INC}"
export CGO_LDFLAGS="-L${UCRT64_LIB} -lvulkan-1 -lgdi32 -luser32"

time go build \
  -trimpath \
  -ldflags="-H=windowsgui -extldflags=-Wl,--stack,8388608" \
  -o "$EXE_OUT" \
  .

echo "==> Copying to dist/windows..."
cp "$EXE_OUT" "$DIST_EXE"
echo "==> Done: $DIST_EXE"
ls -lh "$DIST_EXE"
