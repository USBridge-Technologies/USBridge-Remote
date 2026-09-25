# USBridge MCP bridge

`bridge.mjs` is the source (readable ESM, one real dependency: `ws`) for
what Claude Desktop (or any other stdio-only MCP client) actually runs:
`bin/bridge.cjs`, a single self-contained bundle with `ws` inlined, so a
user only needs Node itself installed -- no `npm install` step, no
`node_modules`, and no manual download either: the config the Scripts & AI
page generates (`MCPBridgeConfigJSON`, `client/internal/gui/view/
scripts_tab.go`) runs it via `node -e "...fetch(...).then(r=>r.text())
.then(c=>{...eval(c)})"`, fetching this file straight from GitHub at launch
(`mcpBridgeSourceURL`, pinned to `main`) with a fresh per-install `--token`.
Before eval-ing, that script SHA-256-hashes the fetched bytes and checks
them against `mcpBridgeSHA256` (same file, hardcoded) -- pinning the branch
alone would let anyone with push access to `main`, or a compromised
raw.githubusercontent.com response, get arbitrary code run on any machine
with this config pasted in; the hash check means a mismatch just fails
closed (bridge refuses to start) instead. See `bridge.mjs`'s own doc
comment for why this process exists and how it relays between Claude
Desktop's stdio and the USBridge web client's browser tab.

## Rebuilding bin/bridge.cjs

```sh
cd client/web/mcp-bridge
npm install
npx esbuild bridge.mjs --bundle --platform=node --format=cjs --target=node18 --outfile=bin/bridge.cjs
shasum -a 256 bin/bridge.cjs
```

`--format=cjs`, not `esm`: esbuild's CJS->ESM interop shims `ws`'s internal
`require()` calls (it's a CommonJS package) in a way that breaks on Node
builtins specifically when the *output* format is ESM (`Dynamic require of
"events" is not supported` at runtime) -- a CJS bundle sidesteps that
entirely since real `require()` is available natively there. Confirmed by
building both ways and running the result.

**After rebuilding, update `mcpBridgeSHA256` in `client/internal/gui/view/
scripts_tab.go` to the new `shasum` output above.** Nothing enforces this
automatically -- forgetting it doesn't run stale code, it just makes every
already-pasted config's integrity check fail (fails closed, see above)
until the constant is fixed, so it's safe to forget short-term but must be
fixed before the release ships.

`bin/bridge.cjs` is committed (same convention as `web/vendor/ort/`'s
onnxruntime-web files) rather than built on demand, so a clone of this repo
can serve/link it immediately without a Node toolchain in the deploy
pipeline. It also needs to reach GitHub's `main` branch specifically (not
just whatever branch is being worked on) since that's what the raw-fetch
URL above is pinned to.
