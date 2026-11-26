#!/bin/bash
# Builds the browser client (client/scripts/build_web.sh) and stages the
# files the agent serves into agent/internal/webclient/static/, where
# `go build` embeds them (see agent/internal/webclient/webclient.go). Called
# by build_linux.sh, build_macos.sh and build_windows.sh before their
# `go build`; safe to run by hand.
#
# app.wasm is staged gzip-only (~16 MB): every browser accepts gzip, also
# over plain http, where browsers don't offer brotli. The AI-vision models
# and ONNX runtime (~150 MB) aren't shipped, same as on the KVM; the page
# loads without them.
#
# USBRIDGE_SKIP_WEBCLIENT=1 skips it (the agent then says the web client
# isn't built in).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CLIENT_DIR="$(cd "$AGENT_DIR/../client" && pwd)"
WEB_DIR="$CLIENT_DIR/web"
STATIC_DIR="$AGENT_DIR/internal/webclient/static"

if [ "${USBRIDGE_SKIP_WEBCLIENT:-}" = "1" ]; then
    echo "==> web client: skipped (USBRIDGE_SKIP_WEBCLIENT=1)"
    exit 0
fi

echo "==> web client: building client/web"
"$CLIENT_DIR/scripts/build_web.sh"

find "$STATIC_DIR" -mindepth 1 ! -name README.md -delete
for f in index.html gui.html lite.html join.html wasm_exec.js ai_vision.js favicon.ico; do
    [ -f "$WEB_DIR/$f" ] && cp "$WEB_DIR/$f" "$STATIC_DIR/$f"
done
gzip -9n -c "$WEB_DIR/app.wasm" > "$STATIC_DIR/app.wasm.gz"

for f in index.html lite.html wasm_exec.js app.wasm.gz; do
    if [ ! -s "$STATIC_DIR/$f" ]; then
        echo "error: web client staging: $f missing" >&2
        exit 1
    fi
done
echo "==> web client staged into $STATIC_DIR ($(du -sh "$STATIC_DIR" | cut -f1))"
