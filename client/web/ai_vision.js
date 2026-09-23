// AI Vision's in-browser inference backend. Loaded as an ES module from
// index.html/gui.html, called from Go/wasm (internal/localui/parser_wasm.go)
// via syscall/js. Every other platform runs icon_detect/dbnet/svtr through
// github.com/yalue/onnxruntime_go (cgo -- no wasm build at all), so this is
// the wasm-only equivalent using onnxruntime-web (see web/vendor/ort/
// README.md for which files and why).
//
// Deliberately a thin black box, one ONNX Runtime session per named model
// (icon_detect/dbnet/svtr): this file only turns an already-preprocessed
// NCHW float32 tensor into a model's raw output tensor. All the actual
// pre/post-processing -- letterbox, /255 normalize, YOLO decode, NMS, DBNet
// blob extraction, SVTR CTC decode, coordinate un-mapping -- stays in Go
// (ported from internal/localui's desktop image.go/yolo.go/dbnet.go/svtr.go
// into parser_wasm.go) so both platforms run the exact same math instead of
// two implementations that could quietly drift apart. See ai_vision.go's
// package doc comment for the feature end to end.
//
// Nothing here runs at page load: a model's weights and (the first time any
// model loads) the WebGPU-provider WASM glue (~28MiB, see vendor/ort/
// README.md) are only fetched the first time loadModel(name, ...) is
// actually called for that name -- from Go, only once AI Vision's checkbox
// or the "Local models" toggle is turned on this session
// (api.LazyInitLocalUIParse's doc comment has the full reasoning:
// downloading 100MB+ on every page open whether or not the feature gets
// used was exactly the bug report this replaced), and only once dbnet/svtr
// are actually needed (the OCR stage runs far less often than icon_detect,
// see ai_vision.go's aiVisionOCRInterval, so parser_wasm.go's NewParser
// still loads all three up front rather than adding a second lazy tier
// on top of the per-session one this file already gives it for free). Once
// fetched, the Worker's long-lived immutable Cache-Control on every model/
// runtime file (see deploy/cloudflare-web/worker.js's r2Routes) means the
// browser's own HTTP cache -- not a bespoke Cache API/OPFS layer here --
// covers "only downloaded once" across future page loads too.

const sessions = new Map(); // name -> Promise<{ort, session}>

async function loadSession(modelUrl, warmupDims) {
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
  const count = warmupDims.reduce((a, b) => a * b, 1);
  const dummy = new ort.Tensor("float32", new Float32Array(count), warmupDims);
  await session.run({ [session.inputNames[0]]: dummy });

  return { ort, session };
}

function ensureSession(name, modelUrl, warmupDims) {
  if (!sessions.has(name)) {
    sessions.set(
      name,
      loadSession(modelUrl, warmupDims).catch((err) => {
        // Let the next call retry instead of permanently caching a failed
        // load (a transient network error fetching a multi-MB model
        // shouldn't need a full page reload to recover from).
        sessions.delete(name);
        throw err;
      }),
    );
  }
  return sessions.get(name);
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

  // Idempotent and lazy per name: only actually fetches+compiles a given
  // model on the first call for that name (see ensureSession/this file's
  // top doc comment). Go awaits this once per model, before the first
  // runInference(name, ...) for it. warmupDims is the model's own NCHW
  // input shape (e.g. [1,3,640,640] for icon_detect, [1,3,960,960] for
  // dbnet, [1,3,48,320] for svtr) -- Go already knows it (parser_wasm.go's
  // Config), simpler to pass through than to hardcode three shapes here.
  loadModel(name, modelUrl, warmupDims) {
    return ensureSession(name, modelUrl, warmupDims).then(() => undefined);
  },

  // tensorData: Float32Array, NCHW, already preprocessed by Go to match
  // dims exactly (letterboxed+/255-normalized for icon_detect/dbnet,
  // resized+right-padded for svtr -- see parser_wasm.go). Resolves to a
  // Float32Array of that model's raw output; Go's own decode/postprocess
  // functions do the rest.
  async runInference(name, tensorData, dims) {
    const entry = sessions.get(name);
    if (!entry) {
      throw new Error(`usbridgeAIVision.runInference(${name}) called before loadModel`);
    }
    const { ort, session } = await entry;
    const tensor = new ort.Tensor("float32", tensorData, dims);
    const outputs = await session.run({ [session.inputNames[0]]: tensor });
    return outputs[session.outputNames[0]].data;
  },
};
