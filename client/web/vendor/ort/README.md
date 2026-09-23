# Vendored onnxruntime-web

`ai_vision.js` (client/web/ai_vision.js) runs `icon_detect.onnx` (see
`internal/localui/models/README.md`) directly in the browser via
[onnxruntime-web](https://github.com/microsoft/onnxruntime) -- the WASM/web
build has no cgo, so it can't use the `onnxruntime_go` binding every other
platform's `internal/localui` package does (see `stub_wasm.go`'s doc
comment).

The `web/` build has no bundler/npm pipeline (plain static HTML+JS, see
`scripts/build_web.sh`'s own doc comment), so these files are vendored here
as plain committed assets rather than an `npm install` dependency -- same
convention as `internal/localui/models/`'s own README on why the ONNX
weights are committed rather than fetched on demand.

| File | Source | Purpose |
|---|---|---|
| `ort.webgpu.min.mjs` | `onnxruntime-web@1.30.0` dist | Entry point ESM module (`ort.webgpu.min.mjs`, not the `.bundle.` variant, so `ai_vision.js`'s own relative `wasmPaths`/`import` control which backend file loads). |
| `ort-wasm-simd-threaded.wasm` / `.mjs` | same | Plain WASM/CPU execution provider -- works in any browser, no special HTTP headers required (falls back to single-threaded automatically without cross-origin isolation). |
| `ort-wasm-simd-threaded.jsep.wasm` / `.mjs` | same | WebGPU execution provider's JS-execution-provider glue. Same file whether or not the page is cross-origin-isolated -- ONNX Runtime only uses `SharedArrayBuffer`/multi-threading opportunistically (COOP/COEP `Cross-Origin-*` headers, not currently set anywhere this app is served -- deliberately not added, since `Cross-Origin-Embedder-Policy: require-corp` risks breaking the existing cross-origin WebRTC signaling fetches this client depends on), degrading to single-threaded WASM-side prep with GPU dispatch still happening on the GPU either way. `ai_vision.js` lists `webgpu` before `wasm` in `executionProviders` so ONNX Runtime falls back further, to the plain CPU path, if WebGPU itself isn't available at all (no GPU, browser doesn't support it, etc). |

MIT licensed (Microsoft). Re-vendor by running `npm pack onnxruntime-web@latest`
and copying the same five files out of its `dist/` -- no other files in that
package are used.
