#!/usr/bin/env bash
# Builds dist/USBridgeAgent-Windows-x86_64-Setup-<version>.exe from
# dist/windows (run scripts/build_windows.sh first). Needs Inno Setup 6.3+
# (ISCC.exe); set ISCC to point at it if it isn't in a standard location.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VERSION="$(tr -d ' \t\n\r' < "$REPO_ROOT/VERSION" 2>/dev/null || echo "0.0.0")"

if [[ ! -f "$REPO_ROOT/dist/windows/USBridgeAgent.exe" ]]; then
    echo "dist/windows/USBridgeAgent.exe not found -- run scripts/build_windows.sh first" >&2
    exit 1
fi

ISCC_BIN="${ISCC:-}"
if [[ -z "$ISCC_BIN" ]]; then
    for c in "/c/Program Files (x86)/Inno Setup 6/ISCC.exe" "/c/Program Files/Inno Setup 6/ISCC.exe" \
             "$LOCALAPPDATA/Programs/Inno Setup 6/ISCC.exe"; do
        [[ -x "$c" ]] && { ISCC_BIN="$c"; break; }
    done
fi
if [[ -z "$ISCC_BIN" ]] && command -v iscc >/dev/null 2>&1; then
    ISCC_BIN="$(command -v iscc)"
fi
if [[ -z "$ISCC_BIN" ]]; then
    echo "ISCC.exe (Inno Setup 6) not found; install it or set ISCC=" >&2
    exit 1
fi

rm -f "$REPO_ROOT"/dist/USBridgeAgent-Windows-x86_64-Setup-*.exe
# MSYS2 would otherwise rewrite the /D... switches as paths.
MSYS2_ARG_CONV_EXCL='*' "$ISCC_BIN" /Q "/DAppVersion=$VERSION" \
    "$(cygpath -w "$SCRIPT_DIR/windows-installer/USBridgeAgent.iss")"
ls -la "$REPO_ROOT"/dist/USBridgeAgent-Windows-x86_64-Setup-*.exe
