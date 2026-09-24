#!/bin/bash
# Builds the browser/WASM web client (client/cmd/wasm) into client/web/,
# ready to be served by any static file server -- no CGO, no native
# toolchain, unlike every other client platform.
#
# Usage:
#   ./scripts/build_web.sh            # build only
#   ./scripts/build_web.sh --serve     # build, then serve web/ on :8765
#   ./scripts/build_web.sh --serve 9000 # ...on a custom port
#
# What gets built:
#   web/app.wasm       -- the actual client (GOOS=js GOARCH=wasm build of
#                          ./cmd/wasm), which boots the same Fyne GUI
#                          (internal/gui) every other platform runs, plus
#                          the webrtcweb signaling/DataChannel plumbing.
#   web/wasm_exec.js   -- Go's wasm bootstrap shim, copied from this
#                          module's own toolchain (see below on why that
#                          matters) -- NOT downloaded or hand-maintained.
#   web/index.html      -- committed, untouched by this script.
#   web/gui.html        -- NOT committed (build artifact, gitignored): a
#                          plain mirror of index.html this script
#                          regenerates on every run, loads app.wasm the
#                          same way. Two filenames exist so either can be
#                          used as the served entry point without one
#                          clobbering the other.
#   web/models/*.onnx   -- NOT committed (build artifact, gitignored):
#                          icon_detect.onnx/dbnet.onnx/svtr.onnx copied from
#                          internal/localui/models/ on every run so the
#                          in-browser AI Vision overlay + local ui.parse
#                          offload (client/web/ai_vision.js) have the same
#                          weights every other platform's cgo/onnxruntime_go
#                          build already ships, without a second copy of
#                          ~90MiB of models living in this repo -- see that
#                          directory's own README for provenance.
#   web/vendor/ort/*     -- committed (onnxruntime-web runtime, see that
#                          directory's own README) -- untouched by this
#                          script, same as index.html.
#
# wasm_exec.js MUST come from the exact Go toolchain that built app.wasm --
# the wire format between the two changes across Go versions, and a
# mismatched pair fails at runtime (WebAssembly.instantiate throwing, or a
# blank page with no error) rather than at build time. This repo's
# client/go.mod pins a `go` directive, so with the default GOTOOLCHAIN=auto,
# plain `go build`/`go env` from inside client/ already resolve to that
# exact pinned toolchain (auto-downloaded under GOPATH/pkg/mod if not
# already cached) -- reading GOROOT *after* that resolution, as this script
# does, is what keeps the copied wasm_exec.js correct without hand-tracking
# a Go version here.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLIENT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
WEB_DIR="$CLIENT_DIR/web"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

cd "$CLIENT_DIR"

CLIENT_VERSION="$(tr -d ' \t\n\r' < "$CLIENT_DIR/VERSION" 2>/dev/null || echo "0.0.0")"

echo -e "${YELLOW}==> Building app.wasm (GOOS=js GOARCH=wasm, version=$CLIENT_VERSION)...${NC}"
# -X main.version=... patches cmd/wasm/main.go's `version` var, same
# ldflags-injection mechanism every other platform's build_*.sh uses (see
# client/cmd/main.go's own doc comment on it) -- without this the version
# corner label falls back to cmd/wasm/main.go's literal "web" placeholder,
# rendering as the nonsensical "vweb".
#
# CGO_ENABLED=0 is NOT optional here even though js/wasm can't use cgo
# anyway: `go env CGO_ENABLED` defaults to 1 whenever a C compiler is on
# PATH (true on most dev machines, e.g. Xcode's cc on macOS), and that
# default leaking into this cross-build makes the package loader refuse
# internal/service's plain .c files (h264_nal.c/h264_sei.c/h264_stream.c --
# no cgo build tag needed on every OS-suffixed platform file, since GOOS
# alone already excludes those) outright ("C source files not allowed when
# not using cgo or SWIG") instead of quietly excluding them the way it does
# when CGO_ENABLED=0 is explicit. Confirmed live: this build broke on a
# machine with a C toolchain installed and silently worked on one without.
CGO_ENABLED=0 GOOS=js GOARCH=wasm go build -ldflags "-X main.version=$CLIENT_VERSION" -o "$WEB_DIR/app.wasm" ./cmd/wasm
ls -lh "$WEB_DIR/app.wasm"

# Resolve wasm_exec.js from whatever toolchain `go build` above actually
# used (see this script's top doc comment) -- Go >=1.24 ships it at
# lib/wasm/wasm_exec.js; older toolchains shipped it at
# misc/wasm/wasm_exec.js, so fall back to that if the new path isn't there.
GOROOT="$(go env GOROOT)"
WASM_EXEC_SRC="$GOROOT/lib/wasm/wasm_exec.js"
[ -f "$WASM_EXEC_SRC" ] || WASM_EXEC_SRC="$GOROOT/misc/wasm/wasm_exec.js"
if [ ! -f "$WASM_EXEC_SRC" ]; then
    echo "error: wasm_exec.js not found under GOROOT ($GOROOT) at either lib/wasm/ or misc/wasm/" >&2
    exit 1
fi
rm -f "$WEB_DIR/wasm_exec.js"
cp "$WASM_EXEC_SRC" "$WEB_DIR/wasm_exec.js"
echo -e "${GREEN}✓${NC} wasm_exec.js <- $WASM_EXEC_SRC"

# gui.html is just index.html under a second name (see this script's top
# doc comment) -- regenerated here rather than committed so there's only
# ever one real source file to keep in sync. Confirmed missing in CI
# (client-web-deploy's own "Deploy Worker" step failing to `cp` it) before
# this line existed -- it only ever got created by hand, locally, during
# this project's own dev-server testing.
cp "$WEB_DIR/index.html" "$WEB_DIR/gui.html"
echo -e "${GREEN}✓${NC} gui.html <- index.html"

mkdir -p "$WEB_DIR/models"
for model in icon_detect dbnet svtr; do
    cp "$CLIENT_DIR/internal/localui/models/$model.onnx" "$WEB_DIR/models/$model.onnx"
    echo -e "${GREEN}✓${NC} models/$model.onnx <- internal/localui/models/$model.onnx"
done

echo -e "${GREEN}✓${NC} Web client built: $WEB_DIR"
echo "  Serve $WEB_DIR with any static file server and open index.html (or gui.html)."
echo "  NOTE: browsers aggressively cache app.wasm across reloads -- if you don't see a"
echo "  rebuild take effect, hard-reload (Ctrl+Shift+R / disable cache in devtools) or"
echo "  use this script's --serve mode below, which sends no-cache headers."

# ── Optional: serve web/ locally with no-cache headers ──────────────────────
# A plain `python3 -m http.server` works too, but browsers can reuse a
# previously cached app.wasm across reloads even once the server starts
# sending fresh bytes for a new build -- confirmed repeatedly hunting a
# "phone still shows the old build" report that was actually this, not a
# stale server. Sending explicit no-cache headers here closes that gap
# server-side; client/web/index.html's own `fetch(..., {cache:"no-store"})`
# closes the other half (a plain page reload not revalidating at all).
if [ "${1:-}" = "--serve" ]; then
    PORT="${2:-8765}"
    echo -e "${YELLOW}==> Serving $WEB_DIR on http://0.0.0.0:$PORT (no-cache) -- Ctrl+C to stop${NC}"
    python3 - "$WEB_DIR" "$PORT" <<'PYEOF'
import http.server
import sys

directory, port = sys.argv[1], int(sys.argv[2])

class NoCacheHandler(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
        self.send_header("Pragma", "no-cache")
        # Cross-origin isolation, required for onnxruntime-web's WASM
        # backend to work at all in this dependency version (see
        # web/vendor/ort/README.md) -- every wasm-simd binary it ships is
        # thread-based, and browsers only grant SharedArrayBuffer on a
        # crossOriginIsolated page. "credentialless" (not "require-corp")
        # and "same-origin-allow-popups" (not plain "same-origin")
        # deliberately: neither requires this app's own cross-origin
        # WebRTC signaling/device requests or the MCP bridge download's
        # window.open() to carry a Cross-Origin-Resource-Policy header of
        # their own -- COEP doesn't gate WebSocket/RTCPeerConnection
        # traffic at all, and credentialless only strips credentials from
        # cross-origin subresource loads rather than blocking them
        # outright. Mirrored in deploy/cloudflare-web/worker.js for
        # production.
        self.send_header("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
        self.send_header("Cross-Origin-Embedder-Policy", "credentialless")
        super().end_headers()

handler = lambda *args, **kwargs: NoCacheHandler(*args, directory=directory, **kwargs)
http.server.ThreadingHTTPServer(("0.0.0.0", port), handler).serve_forever()
PYEOF
fi
