// r2Routes maps a URL path to the R2 object key + response shape for every
// file too big for Cloudflare's 25MiB static-asset limit (everything else
// -- index.html/gui.html/wasm_exec.js/ai_vision.js/vendor/ort's small
// files -- is served straight from the ASSETS binding below instead).
const r2Routes = {
  "/app.wasm": {
    key: "app.wasm",
    contentType: "application/wasm",
    // A stale cached app.wasm silently surviving across redeploys has
    // repeatedly cost real debugging time in this project -- force
    // revalidation instead, same reasoning as build_web.sh's --serve mode.
    cacheControl: "no-cache",
  },
  // icon_detect.onnx (the in-browser AI Vision overlay's YOLO weights, see
  // ai_vision.js) and onnxruntime-web's WebGPU-provider WASM glue are both
  // over the limit too, and unlike app.wasm essentially never change
  // between deploys (same model weights / same pinned onnxruntime-web
  // version, see internal/localui/models/README.md and
  // web/vendor/ort/README.md) -- long-lived immutable caching instead of
  // no-cache, so a returning visitor's browser (and this app's own Cache
  // API layer in ai_vision.js) never re-fetches either one just because
  // app.wasm's own no-cache header forced a revalidation round trip.
  "/models/icon_detect.onnx": {
    key: "models/icon_detect.onnx",
    contentType: "application/octet-stream",
    cacheControl: "public, max-age=31536000, immutable",
  },
  "/vendor/ort/ort-wasm-simd-threaded.jsep.wasm": {
    key: "vendor/ort/ort-wasm-simd-threaded.jsep.wasm",
    contentType: "application/wasm",
    cacheControl: "public, max-age=31536000, immutable",
  },
  // asyncify is the fallback ai_vision.js's webgpu-first executionProviders
  // list falls through to when webgpu itself isn't available -- also over
  // the 25MiB limit (25.5MiB).
  "/vendor/ort/ort-wasm-simd-threaded.asyncify.wasm": {
    key: "vendor/ort/ort-wasm-simd-threaded.asyncify.wasm",
    contentType: "application/wasm",
    cacheControl: "public, max-age=31536000, immutable",
  },
};

// addCrossOriginIsolationHeaders is required for onnxruntime-web's WASM
// backend to work at all in this dependency version (see web/vendor/ort/
// README.md) -- every wasm-simd binary it ships is thread-based, and
// browsers only grant SharedArrayBuffer on a crossOriginIsolated page.
// "credentialless" (not "require-corp") and "same-origin-allow-popups"
// (not plain "same-origin") deliberately: neither requires this app's own
// cross-origin WebRTC signaling/device requests or the MCP bridge
// download's window.open() to carry a Cross-Origin-Resource-Policy header
// of their own -- COEP doesn't gate WebSocket/RTCPeerConnection traffic at
// all, and credentialless only strips credentials from cross-origin
// subresource loads rather than blocking them outright. Mirrored in
// build_web.sh's --serve mode for local dev. Applied to every response
// (R2-routed and ASSETS-routed alike) since crossOriginIsolated is a
// property of the top-level document -- it doesn't matter which response
// carries index.html/gui.html, but whichever does needs both headers, and
// setting them everywhere is simpler than special-casing just those two.
function addCrossOriginIsolationHeaders(response) {
  const headers = new Headers(response.headers);
  headers.set("Cross-Origin-Opener-Policy", "same-origin-allow-popups");
  headers.set("Cross-Origin-Embedder-Policy", "credentialless");
  return new Response(response.body, { status: response.status, statusText: response.statusText, headers });
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const route = r2Routes[url.pathname];
    if (route) {
      const obj = await env.WASM_BUCKET.get(route.key);
      if (!obj) {
        return new Response(`${route.key} not found in R2`, { status: 404 });
      }
      const headers = new Headers();
      obj.writeHttpMetadata(headers);
      headers.set("etag", obj.httpEtag);
      headers.set("Content-Type", route.contentType);
      headers.set("Cache-Control", route.cacheControl);
      return addCrossOriginIsolationHeaders(new Response(obj.body, { headers }));
    }

    return addCrossOriginIsolationHeaders(await env.ASSETS.fetch(request));
  },
};
