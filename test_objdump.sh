#!/bin/bash
export PATH=/usr/bin:/c/msys64/ucrt64/bin:$PATH
OBJDUMP_BIN='x86_64-w64-mingw32-objdump'
if ! command -v $OBJDUMP_BIN >/dev/null; then
    echo "NO OBJDUMP: $OBJDUMP_BIN"
    OBJDUMP_BIN=objdump
fi
$OBJDUMP_BIN -p dist/windows/libjxl.dll | grep -i 'DLL Name:'
