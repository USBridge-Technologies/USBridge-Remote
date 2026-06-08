#!/bin/bash
FFMPEG_BIN_DIR="C:/msys64/ucrt64/bin"
DLL_EXTRA_DIRS=("/c/msys64/ucrt64/bin")

_resolve_dll() {
    local name="$1"
    for _d in "$FFMPEG_BIN_DIR" "${DLL_EXTRA_DIRS[@]}"; do
        [ -d "$_d" ] || continue
        if [ -f "$_d/$name" ]; then
            echo "$_d/$name"
            return
        fi
    done
}

echo "libhwy.dll: $(_resolve_dll libhwy.dll)"
echo "libogg-0.dll: $(_resolve_dll libogg-0.dll)"
echo "libbrotlienc.dll: $(_resolve_dll libbrotlienc.dll)"
echo "libjxl_cms.dll: $(_resolve_dll libjxl_cms.dll)"
