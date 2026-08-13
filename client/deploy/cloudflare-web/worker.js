export default {
  async fetch(request, env) {
    const url = new URL(request.url);

    // app.wasm lives in R2 (46MiB+, over Cloudflare's 25MiB static-asset
    // file limit) -- everything else (index.html/gui.html/wasm_exec.js) is
    // served straight from the ASSETS binding below.
    if (url.pathname === "/app.wasm") {
      const obj = await env.WASM_BUCKET.get("app.wasm");
      if (!obj) {
        return new Response("app.wasm not found in R2", { status: 404 });
      }
      const headers = new Headers();
      obj.writeHttpMetadata(headers);
      headers.set("etag", obj.httpEtag);
      headers.set("Content-Type", "application/wasm");
      // Same reasoning as build_web.sh's --serve mode: a stale cached
      // app.wasm silently surviving across redeploys has repeatedly cost
      // real debugging time in this project. Force revalidation instead.
      headers.set("Cache-Control", "no-cache");
      return new Response(obj.body, { headers });
    }

    return env.ASSETS.fetch(request);
  },
};
