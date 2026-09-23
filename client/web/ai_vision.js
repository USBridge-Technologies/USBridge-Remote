// AI Vision's in-browser inference backend. Loaded as an ES module from
// index.html/gui.html, called from Go/wasm (internal/localui/stub_wasm.go's
// real Parser implementation) via syscall/js. Every other platform runs
// icon_detect.onnx through github.com/yalue/onnxruntime_go (cgo -- no wasm
// build at all), so this is the wasm-only equivalent using
// onnxruntime-web (see web/vendor/ort/README.md for which files and why).
//
// Deliberately a thin black box: this file only turns an already-letterboxed
// NCHW float32 tensor into icon_detect's raw output tensor. All the actual
// pre/post-processing -- letterbox, /255 normalize, YOLO decode, NMS,
// coordinate un-mapping -- stays in Go (ported from internal/localui's
// desktop image.go/yolo.go into stub_wasm.go) so both platforms run the
// exact same math instead of two implementations that could quietly drift
// apart. See ai_vision.go's package doc comment for the feature end to end.
//
// Nothing here runs at page load: the model (icon_detect.onnx, ~77MiB) and
// the WebGPU-provider WASM glue (~28MiB, see vendor/ort/README.md) are only
// fetched the first time loadModel() is actually called -- from Go, only
// once AI Vision's checkbox or the "Local models" toggle is turned on this
// session (api.LazyInitLocalUIParse's doc comment has the full reasoning:
// downloading 100MB+ on every page open whether or not the feature gets
// used was exactly the bug report this replaced). Once fetched, the
// Worker's long-lived immutable Cache-Control on both (see
// deploy/cloudflare-web/worker.js's r2Routes) means the browser's own HTTP
// cache -- not a bespoke Cache API/OPFS layer here -- covers "only
// downloaded once" across future page loads too.

const ICON_INPUT_SIZE = 640;

let sessionPromise = null;

async function loadSession(modelUrl) {
  const ort = await import(new URL("./vendor/ort/ort.webgpu.min.mjs", import.meta.url).href);
  // Resolved relative to *this* module's own URL, not document.baseURI --
  // robust regardless of whether index.html is served at the site root or
  // some sub-path (self-hosted dev server, a future non-root Worker route,
  // etc.), see this file's top doc comment on why nothing here can assume
  // where it's deployed.
  const vendorDir = new URL("./vendor/ort/", import.meta.url).href;
  ort.env.wasm.wasmPaths = vendorDir;

  const session = await ort.InferenceSession.create(modelUrl, {
    // webgpu first, wasm (plain CPU) as the automatic fallback if WebGPU
    // isn't available at all -- see vendor/ort/README.md for why both
    // ship and why no cross-origin-isolation headers are required for
    // either to work, just for the fastest multi-threaded case of the
    // wasm-side prep work.
    executionProviders: [{ name: "webgpu" }, "wasm"],
    graphOptimizationLevel: "all",
  });

  // Warm-up run: WebGPU compiles its WGSL shaders lazily on the first real
  // session.run() (a 1-3s freeze, see vendor/ort/README.md) -- paying that
  // cost right after load instead of on the first live-overlay pass keeps
  // the very first detection after ticking the checkbox from stalling.
  const dummy = new ort.Tensor(
    "float32",
    new Float32Array(3 * ICON_INPUT_SIZE * ICON_INPUT_SIZE),
    [1, 3, ICON_INPUT_SIZE, ICON_INPUT_SIZE],
  );
  await session.run({ [session.inputNames[0]]: dummy });

  return { ort, session };
}

function ensureSession(modelUrl) {
  if (!sessionPromise) {
    sessionPromise = loadSession(modelUrl).catch((err) => {
      // Let the next call retry instead of permanently caching a failed
      // load (a transient network error fetching the 77MiB model
      // shouldn't need a full page reload to recover from).
      sessionPromise = null;
      throw err;
    });
  }
  return sessionPromise;
}

window.usbridgeAIVision = {
  // WebAssembly is a proxy for "onnxruntime-web can run here at all" --
  // the wasm execution provider works in effectively every browser this
  // client already requires (it's the Go client binary's own runtime
  // too); WebGPU's own absence just means loadSession's executionProviders
  // list falls through to that CPU path instead of failing.
  isSupported() {
    return typeof WebAssembly !== "undefined";
  },

  // Idempotent and lazy: only actually fetches+compiles the model on the
  // first call (see ensureSession/this file's top doc comment). Go awaits
  // this once, before the first runInference.
  loadModel(modelUrl) {
    return ensureSession(modelUrl).then(() => undefined);
  },

  // tensorData: Float32Array, length 3*640*640, NCHW, already
  // letterboxed+/255-normalized by Go (mirrors image.go's toNCHWFloat).
  // Resolves to a Float32Array, length 5*8400 -- icon_detect's raw output
  // (channel-major cx,cy,w,h,conf); Go's decodeYOLO does the rest (see
  // yolo.go, ported into stub_wasm.go).
  async runInference(tensorData) {
    if (!sessionPromise) {
      throw new Error("usbridgeAIVision.runInference called before loadModel");
    }
    const { ort, session } = await sessionPromise;
    const tensor = new ort.Tensor("float32", tensorData, [1, 3, ICON_INPUT_SIZE, ICON_INPUT_SIZE]);
    const outputs = await session.run({ [session.inputNames[0]]: tensor });
    return outputs[session.outputNames[0]].data;
  },
};
