# USBridge MCP bridge

`bridge.mjs` is the source (readable ESM, one real dependency: `ws`) for
what Claude Desktop (or any other stdio-only MCP client) actually runs:
`bin/bridge.cjs`, a single self-contained bundle with `ws` inlined, so a
user only needs Node itself installed -- no `npm install` step, no
`node_modules` to go with the downloaded file. See `bridge.mjs`'s own doc
comment for why this process exists and how it relays between Claude
Desktop's stdio and the USBridge web client's browser tab (the Scripts & AI
page generates the ready-to-paste config + this file's download link, with
a fresh per-install `--token`).

## Rebuilding bin/bridge.cjs

```sh
cd client/web/mcp-bridge
npm install
npx esbuild bridge.mjs --bundle --platform=node --format=cjs --target=node18 --outfile=bin/bridge.cjs
```

`--format=cjs`, not `esm`: esbuild's CJS->ESM interop shims `ws`'s internal
`require()` calls (it's a CommonJS package) in a way that breaks on Node
builtins specifically when the *output* format is ESM (`Dynamic require of
"events" is not supported` at runtime) -- a CJS bundle sidesteps that
entirely since real `require()` is available natively there. Confirmed by
building both ways and running the result.

`bin/bridge.cjs` is committed (same convention as `web/vendor/ort/`'s
onnxruntime-web files) rather than built on demand, so a clone of this repo
can serve/link it immediately without a Node toolchain in the deploy
pipeline.
