# Deploying the browser/WASM web client to web.usbridge.io

The browser client (`client/cmd/wasm`, built by `client/scripts/build_web.sh`)
is hosted on Cloudflare as a Worker with static assets, not Cloudflare Pages —
this account's token permission set doesn't expose a separate "Cloudflare
Pages: Edit" permission (Cloudflare has been folding Pages into the Workers
platform), and Workers static assets + a custom domain gets the identical
result: free TLS on 443, no origin server to run or patch.

## Why R2, not just Workers static assets

`app.wasm` is a full Fyne GUI compiled to WebAssembly — **~46 MiB**, well
over Cloudflare's **25 MiB per-file limit** for Workers (and Pages, same
underlying limit) static assets. A bare Fyne "hello world" wasm build is
already ~38 MiB on its own (confirmed by building one); this isn't something
`build_web.sh` or Go build flags can trim under the limit.

So the deploy splits in two:
- `index.html` / `gui.html` / `wasm_exec.js` (small) — ordinary Workers
  static assets, served via the `[assets]` binding in `wrangler.toml`.
- `app.wasm` (huge, no size limit that matters) — uploaded straight to an
  **R2 bucket** (`usbridge-web-client-assets`), and `worker.js` streams it
  from there on every `/app.wasm` request. R2 has to be enabled once per
  account via the dashboard (`R2 > Enable`) before its API responds to
  anything other than "Please enable R2 through the Cloudflare Dashboard".

## Where the pieces live

- `client/deploy/cloudflare-web/wrangler.toml` — Worker config: assets
  directory, the R2 binding, and the `web.usbridge.io` custom domain route.
- `client/deploy/cloudflare-web/worker.js` — the actual Worker script:
  `/app.wasm` → stream from R2, everything else → `env.ASSETS.fetch`.
- `.github/workflows/release-all.yml`'s `client-web-deploy` job — builds,
  uploads to R2, and runs `wrangler deploy` on every **real** release (gated
  on `!inputs.prerelease`, same as the GitHub Release's own `make_latest`
  gate — a `test-*` pre-release build must never overwrite the public site).

## One-time setup this repo's CI does NOT automate

These were done once by hand when the site was first stood up, and don't
need repeating unless the underlying Cloudflare account/zone changes:

1. **Enable R2** on the account (`dash.cloudflare.com` → R2 → Enable) — no
   API for this, it's a one-time dashboard click.
2. **WAF allowlist** — `usbridge.io`'s zone has a custom "Anti-Mirror"
   firewall rule that blocks any request whose `Host` isn't
   `usbridge.io`/`www.usbridge.io`/`localhost`. `web.usbridge.io` had to be
   added to that rule's expression (`Security > WAF > Custom rules` in the
   dashboard, or `PUT /zones/:id/filters/:filter_id` via the API) before the
   subdomain would stop returning Cloudflare's own "Sorry, you have been
   blocked" page. This is a one-time fix for that specific rule, not
   something `client-web-deploy` re-touches on every run.

## The `CLOUDFLARE_API_TOKEN` secret

Repo secret (Settings → Secrets and variables → Actions), scoped as
narrowly as Cloudflare's token UI allows:
- `Zone` → `DNS` → `Edit`, **Zone Resources: Specific zone → usbridge.io**
- `Zone` → `Firewall Services` → `Edit`, same zone scope
- `Account` → `Workers Scripts` → `Edit`

Never committed anywhere in this repo — `client-web-deploy` reads it only
via `${{ secrets.CLOUDFLARE_API_TOKEN }}` into a step-local env var, and
GitHub Actions masks the literal value in every log line automatically.
`CLOUDFLARE_ACCOUNT_ID` and the R2 bucket name are plain env vars in the
workflow, not secrets — neither authenticates anything on its own without
the token.

## Manual redeploy (without CI)

```bash
cd client
./scripts/build_web.sh                      # builds web/app.wasm etc.

export CLOUDFLARE_API_TOKEN=...              # never commit this
ACCOUNT_ID=a8e647507553ba57609773213ae46f27
BUCKET=usbridge-web-client-assets

curl -sf -X PUT \
  "https://api.cloudflare.com/client/v4/accounts/$ACCOUNT_ID/r2/buckets/$BUCKET/objects/app.wasm" \
  -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  -H "Content-Type: application/wasm" \
  --data-binary @web/app.wasm

mkdir -p deploy/cloudflare-web/public
cp web/index.html web/gui.html web/wasm_exec.js deploy/cloudflare-web/public/
cd deploy/cloudflare-web
npx --yes wrangler@3 deploy
```
