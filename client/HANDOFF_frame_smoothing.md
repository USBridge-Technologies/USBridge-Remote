# Frame Smoothing (concealment) — session handoff

Working notes to resume this task after a reboot. Nothing in this repo is committed
yet — see "Next steps" at the bottom.

## What this feature does

Windows/Vulkan client only. When the network stalls and the next real decoded frame
is late, instead of freezing on the last real frame, the render thread
motion-extrapolates a synthetic frame from the last two real frames and presents that
until the real frame arrives. Real frames are never delayed — zero added latency on
the happy path. Off by default; UI checkbox is **"Smooth Motion (Beta)"** next to AI
Vision / Net Graph in the video settings dialog.

Plan doc (original design, written before implementation): `C:\Users\Amir\.claude\plans\misty-bubbling-wren.md`

## Architecture (as actually built, not just planned)

- **Backend**: single-level 16×16 block-match optical flow (`flow_blockmatch.comp`)
  + backward-reprojection warp (`warp_extrapolate.comp`, samples `frame_N` at
  `-t·flow` instead of literal forward-push — avoids disocclusion-hole handling
  entirely). Both are new compute shaders, precompiled with `glslc` and embedded as
  SPIR-V arrays in `internal/service/shader_arrays.h` (source kept in a comment
  there, same convention as the existing HUD/ycbcr shaders).
- **Critical fix mid-session**: the client actually decodes via the **zero-copy
  Vulkan Video Decode path** (`vk_render_frame_vkimage` in
  `vk_video_impl_windows.c`), NOT the RGBA CPU-submit path
  (`vk_render_frame`) the plan originally scoped to. Concealment is now wired into
  BOTH: on the zero-copy path, capturing a real frame means an extra unletterboxed
  draw through the *existing* YCbCr→RGB pipeline into an offscreen RGBA texture
  (`g_conceal_tex[2]`) — cheap, GPU-only, no CPU readback, confirmed by the user
  ("у нас на зирокопи все — так и делай чтоб быстро работало и негрузило").
- **Decision logic** (Go, unit tested, `internal/service/frame_smoothing.go`):
  `decideConcealment` / `updateExpectedIntervalMs`. Native code (C) only executes the
  decision via cgo exports (`goFrameSmoothingDecide`,
  `goFrameSmoothingUpdateInterval` in `frame_smoothing_windows.go`).
- **Net Graph HUD**: concealed ticks now drawn as purple dots on the RTT graph
  (`netGraphDrawConcealedMarkers` in `net_graph.go`), fed by
  `GetFrameSmoothingStats().ConcealedFrames` diffed per-tick.

## Threshold tuning history (the hard-won part — don't redo this blind)

Three rounds of live testing against a real direct-LAN stream, each one found via
`app.log`:

1. **1.3× ratio**: fired constantly on ordinary frame-to-frame jitter
   (`gap=11ms expected=8ms`, `gap=25ms expected=17ms`) — every "stall" replaced a
   good real frame with a worse synthesized one. This is what caused "картинка
   дерганая при включенном" (choppy when enabled).
2. **1.8× + 10ms floor**: better, but this stream's own render-thread jitter sits
   close enough to that line (`gap=24ms expected=12ms`, `gap=25ms expected=14ms`)
   that it still misfired.
3. **2.2× + 15ms floor**: fixed the false positives, but then **mathematically could
   never catch a single dropped frame** — a ratio ≥2.0 can never satisfy a
   >2.0×-of-2x-gap test. User reported real packet loss "не заменяло похоже на
   сгенерированные" (didn't seem to substitute with generated frames) — this was why.
4. **Current: 1.9× ratio + 13ms floor** (`frameSmoothingTriggerRatio`,
   `frameSmoothingMinOverageMs` in `frame_smoothing.go`). Just under the 2.0×
   "one dropped frame" line so single drops at 30–120fps still trigger, while still
   clearing all the healthy-jitter false-positive cases above. All of these exact
   numbers are pinned as regression tests in `frame_smoothing_test.go` — read those
   test names before changing the constants again.
5. Also fixed: `updateExpectedIntervalMs` now rejects intervals below
   `frameSmoothingMinPlausibleIntervalMs` (4ms) — a burst of already-buffered frames
   flushing right after connect was corrupting the EMA baseline to ~3ms
   (`expected=3ms`), which then falsely flagged the next ordinary frame.

**Last live result** (end of session): 1 concealment trigger in ~30s of otherwise
healthy streaming (`gap=33ms expected=15ms t=1.19`) — a plausible real gap, not noise.
Down from continuous per-second false triggers before the fix.

**Not yet re-verified after the 1.9×/13ms change**: real packet-loss/high-latency
behavior. The math bug (#3 above) that likely caused the "didn't substitute" report
is fixed, but this needs a real bad-network retest to confirm concealment actually
kicks in now, not just that it stopped false-firing.

## Net Graph HUD resize (unrelated ask, done same session)

User: "нетграф сделай больше раза в 2 — сейчас слишком мелкий". Doubled:
`netGraphCanvasW/H` 320×200→640×400, font 13pt→26pt, `marginX`/`col2`/dot/bar sizes
all scaled to match, in `net_graph.go`. **Must stay in sync by hand** with
`vk_video_impl_windows.c`'s `VK_HUD_W`/`VK_HUD_H`/`VK_HUD_MARGIN` (already updated to
640/400/24) — there's an explicit runtime mismatch check/log if these ever drift
apart (`vk_hud_set_pixels: HUD canvas size mismatch`). macOS/iOS's own native
`HUD_MARGIN` (`metal_video_impl_darwin.m`/`metal_video_impl_ios.m`) was deliberately
**left untouched** (still 12) — not wired to the Go constant, out of scope this
session, cosmetic-only mismatch.

## Files changed/added (nothing committed)

```
 M internal/gui/i18n/localization.go        (FrameSmoothing/Hint/Badge strings)
 M internal/gui/view/video_start_dialog.go  ("Smooth Motion" checkbox row)
 M internal/service/net_graph.go            (purple markers + 2x HUD resize)
 M internal/service/net_graph_windows.go    (ConcealedFrames hook, USBRIDGE_NET_GRAPH env var)
 M internal/service/shader_arrays.h         (+2 compute shaders' SPIR-V)
 M internal/service/vk_video_impl_windows.c (the whole feature's native half)
?? internal/service/frame_smoothing.go              (decision logic, unit tested)
?? internal/service/frame_smoothing_supported_other.go
?? internal/service/frame_smoothing_supported_windows.go
?? internal/service/frame_smoothing_test.go
?? internal/service/frame_smoothing_windows.go      (cgo boundary + USBRIDGE_FRAME_SMOOTHING env var)
```

## How to resume testing after reboot

Build (from `client/`, ~50s incremental / ~10min if `vk_video_impl_windows.c` itself
changed):
```powershell
.\scripts\fast_rebuild.ps1
```

Launch with both debug features force-enabled (no UI clicking needed) + autoconnect
deeplink (direct LAN, per user's explicit choice over tailscale):
```powershell
cd dist\windows
$env:USBRIDGE_FRAME_SMOOTHING = "1"
$env:USBRIDGE_NET_GRAPH = "1"
$url = "usbridge://connect?internal_host=192.168.200.240&master_key=<REDACTED_DEVICE_MASTER_KEY>&protocol=direct&immediate=true"
Start-Process -FilePath ".\USBridge_Client.exe" -ArgumentList $url
```
(Master key redacted before commit -- it's the user's own device credential;
grab the current value from the device's own pairing screen when resuming.)

Logs: `dist\windows\logs\app.log`. Grep for `conceal` to see trigger events
(throttled to once per stall, format: `concealing a stall (gap=Xms expected=Yms t=Z)`).

Run unit tests (from `client/`):
```bash
go test ./internal/service/...
```

## Open items / next steps

1. **User needs to retest with real packet loss / high latency** against the
   1.9×/13ms tuning — confirm concealment now actually engages (not just that it
   stopped false-firing on healthy jitter).
2. **User needs to visually confirm** the 2x Net Graph HUD is legible and purple
   dots are visible during a real concealment event.
3. No computer-use / GUI automation was available this session — all UI testing was
   done via the two `USBRIDGE_*` env vars instead of clicking checkboxes. Fine as a
   debug aid; the real checkboxes were never clicked/visually confirmed to wire up
   correctly end-to-end (though the Go code path is identical, so this is low risk).
4. **Nothing is committed.** Once the user confirms it looks right, commit with an
   appropriate message covering: the feature, the zero-copy-path fix, the threshold
   tuning history, and the Net Graph resize.
5. Known, deliberate scope cuts (see plan doc + code comments for detail):
   single-level block-match (not 3-level pyramid), backward-reprojection (not
   forward warp + hole-fill), NVOF backend not implemented (interface left for it),
   macOS/iOS HUD_MARGIN not synced to the 2x resize.
