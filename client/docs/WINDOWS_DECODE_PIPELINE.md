# Windows decode pipeline: threading, decoder threads, jitter buffer

Investigation of October 2026 (client 3.0.88). Windows client on a laptop with
an AMD Radeon 780M iGPU (Vulkan Video decode, zero-copy render), streaming 4K
HEVC from a Linux USBridge agent.

## Symptom

- The macOS client streams the same agent at a smooth 120 fps.
- The Windows client streamed at about 60 fps, and input lag grew to 1-2 seconds.
- Against other streamers the Windows client only showed a lower frame rate,
  without the growing lag.

## Findings from `app.log`

1. **Decode ran on the network receive thread.** The client set
   `CAPABILITY_DIRECT_SUBMIT`, so `avcodec_send_packet`, `avcodec_receive_frame`
   and frame delivery all ran inside moonlight-common-c's RTP receive loop. The
   log showed `SLOW video receive-loop iteration: ~9000us not draining socket`
   on every frame. At 120 fps the budget is 8.33 ms.
2. **The playout buffer slept on that same thread.** Our moonlight-common-c
   fork's playout (jitter) buffer added 7-50 ms per frame. On the
   direct-submit path that sleep also stops the socket from being drained.
3. **The YCbCr descriptor pool was too small.** It held one descriptor per
   cached image. A YCbCr-converted combined image sampler can use several
   descriptors per set. On AMD, `combinedImageSamplerDescriptorCount` is 3
   for 2-plane formats. The pool ran out after a few of ffmpeg's surfaces, and
   frames decoded into any other surface were never drawn. The log showed
   `vkAllocateDescriptorSets failed` 4,915 times in one session.
4. **The renderer called `vkQueueWaitIdle(decode queue)` before each frame.**
   That waits for every newer decode ffmpeg has queued, not only the frame
   being drawn. It also touched a queue that ffmpeg submits to from another
   thread, without ffmpeg's queue lock.
5. The display runs at 60 Hz with FIFO_RELAXED, so on-screen frame rate is
   capped at 60. That part is expected. The lag is not.

### Why the lag grows to seconds

When decode costs more than one frame interval, the receive thread falls
behind. Unread packets queue in the kernel socket buffer, which is 2.4 MB
(`Actual receive buffer size: 2424832`). At an 8 Mbps stream that is about
2.4 seconds of video. The lag therefore grows until the buffer overflows, and
then a loss and IDR cycle resets it. The official clients avoid this: they
decode off the network thread from a bounded 15-frame queue. If that queue
overflows, they flush it and request an IDR frame, so the backlog never grows
past a few frames.

## Official Moonlight comparison

Sources: moonlight-qt `de2467e` (2026-10-03) and moonlight-common-c `f900dd4`
(2026-09-26), cloned fresh for this investigation.

| | moonlight-qt (Windows) | USBridge 3.0.88 (Windows) |
|---|---|---|
| Capability | `CAPABILITY_PULL_RENDERER` (`ffmpeg.cpp:153`) | `CAPABILITY_DIRECT_SUBMIT` |
| Decode thread | own `FFDecoder` thread, `LiWaitForNextVideoFrame` / `LiPollNextVideoFrame` | RTP receive thread |
| Backpressure | bounded 15-frame queue, flushed with an IDR request on overflow | kernel socket buffer, seconds deep |
| Decoder flags | `AV_CODEC_FLAG_LOW_DELAY` | none |
| Hardware decoder | D3D11VA (default renderer) | Vulkan Video, zero-copy |
| Jitter buffer | none | fork's playout buffer, always on |

## Offline benchmark

`tools/decode_bench/decode_bench.c` uses the same threading logic as the
client, with real Vulkan Video decode on the same GPU:

- A "network" thread releases one access unit every 1/fps seconds.
- The three modes are `direct`, `thread` (moonlight-common-c's `VideoDec`
  thread) and `pull` (the moonlight-qt loop).
- Latency is measured from the scheduled packet arrival until the frame's
  timeline semaphore signals, meaning GPU decode is done and the frame is
  ready to render.
- The clips are 4K HEVC, P-frames only with an infinite GOP, encoded by NVENC
  at 8 and 40 Mbps.

### 1. Threading mode alone (one decoder thread, `LOW_DELAY`)

4K @ 120 fps, 10 s:

| clip | mode | frames decoded | latency p50 | latency p99 | max time the network thread is blocked | CPU (% of one core) |
|---|---|---|---|---|---|---|
| 8 Mbps | direct | 1199/1199 | **507 ms** | **1021 ms** | 10.3 ms | 9.6 |
| 8 Mbps | thread | 173 (queue overflow, then no IDR in a file) | 66 ms | 140 ms | 0.05 ms | 36.7 |
| 8 Mbps | pull | 176 (same) | 68 ms | 141 ms | 0.01 ms | 25.8 |
| 40 Mbps | direct | 1199/1199 | **698 ms** | **1370 ms** | 10.4 ms | 10.8 |
| 40 Mbps | thread | 122 (same) | 70 ms | 141 ms | 0.48 ms | 35.5 |
| 40 Mbps | pull | 120 (same) | 68 ms | 141 ms | 0.02 ms | 29.4 |

4K @ 60 fps: all three modes are the same. Latency p50 is 9.3-9.8 ms and
p99 is 10.1-10.8 ms, with no overflows. CPU is 15-21% of one core.

`direct` reproduces the field bug: lag climbs steadily, to about 1 s in 10 s.
In `thread` and `pull` the network thread is never blocked, but the decoder
still cannot keep up, so the bounded queue overflows. In a live stream an
overflow becomes an IDR request instead of seconds of lag.

### 2. The real limit: ffmpeg's Vulkan decoder with one thread

`ffmpeg -hwaccel vulkan -threads N` on the 40 Mbps clip, 780M:

| decoder threads | Vulkan decode throughput |
|---|---|
| 1 | **104 fps** (9.6 ms/frame, strictly one frame in flight) |
| 2 | 202 fps |
| auto | 216 fps |

D3D11VA decodes at 263 fps with `-threads auto`. With one decoder thread, a
4K@120 stream can never keep up on this GPU, whatever thread calls into
libavcodec. Frame threading requires `AV_CODEC_FLAG_LOW_DELAY` to be off,
because libavcodec disables frame threads when it is set.

### 3. Two frame threads, steady state (after the first 0.5 s)

4K @ 120 fps:

| clip | mode / threads | latency p50 | latency p99 | CPU |
|---|---|---|---|---|
| 8 Mbps | direct / 1 | 541 ms | 1029 ms | 11.7 |
| 8 Mbps | direct / 2 | 50 ms | 157 ms | 18.5 |
| 8 Mbps | thread / 2 | 34 ms | 144 ms | 31.0 |
| 8 Mbps | **pull / 2** | **18 ms** | **79 ms** | 33.7 |
| 8 Mbps | pull / 3 | 19 ms | 69 ms | 37.5 |
| 40 Mbps | direct / 1 | 743 ms | 1394 ms | 10.2 |
| 40 Mbps | direct / 2 | 11 ms | 49 ms | 27.8 |
| 40 Mbps | thread / 2 | 13 ms | 70 ms | 32.0 |
| 40 Mbps | **pull / 2** | **11 ms** | **48 ms** | 35.1 |
| 40 Mbps | pull / 3 | 18 ms | 66 ms | 26.6 |

The p99 tail comes from short episodes where the shared iGPU decodes below
120 fps. Latency then climbs by about 1.2 ms per frame and drains back down.
This is GPU clock and load behavior, not threading. In these episodes `pull`
recovers best.

At 4K @ 60 fps, 2 threads cost exactly one frame of latency:

| mode / threads | latency p50 | latency p99 |
|---|---|---|
| pull / 1 | 9.4-9.7 ms | 10.3-10.7 ms |
| pull / 2 | 16.8 ms | 17.1 ms |

## What changed

- **Decode mode** (`moonlight_cgo_windows.go`, env `USBRIDGE_DECODE_MODE`):
  - `pull` is the default. It uses `CAPABILITY_PULL_RENDERER` and its own
    decode thread running moonlight-qt's loop.
  - `thread` and `direct` remain available for A/B testing.
- **Decoder threads** (`win_vk_decode_threads`, env `USBRIDGE_DECODE_THREADS`):
  - 2 frame threads when width × height × fps is above 4K@60. Example: 4K@120.
  - 1 thread with `LOW_DELAY` otherwise. Examples: 4K@60, 1440p@120.
  - The 4K@60 cutoff was measured on the 780M. A faster GPU would manage with
    1 thread and save a frame of latency, so this heuristic may need a
    per-GPU check later.
- **Jitter buffer** is a latency/smoothness slider in the video settings
  dialog's bitrate card (off, low, balanced, smooth, smoothest), on every
  platform, off by default (`service.SetPlayoutBufferLevel`; the fork's
  `FramePacer.h`). The FEC slider next to it sets the agent's streamer FEC
  (`POST /api/video/fec`, shown only when `/api/video/info` has `fec`).
  - The fork now re-reads `USBRIDGE_PLAYOUT_BUFFER` (the level) at every
    stream start.
  - The fork now honors the flag on the queued and pull path too. Before, the
    off switch worked only on the direct-submit path.
- **Renderer** (`vk_video_impl_windows.c`):
  - The YCbCr descriptor pool is sized at `cache × 3`.
  - The image-view cache grew from 8 to 32 entries.
  - Eviction is round-robin, and it waits for the in-flight fence before
    rewriting a descriptor set.
  - Each frame now waits on its own `AVVkFrame` timeline semaphore value
    instead of `vkQueueWaitIdle(decode queue)`.
- **Smooth Motion** (frame-smoothing concealment) is hidden from the video
  settings dialog. It stays off. In live use it made the picture worse and
  gave no visible benefit.

## Reproducing

```sh
# MSYS2 UCRT64 shell
gcc -O2 tools/decode_bench/decode_bench.c -o decode_bench.exe \
    $(pkg-config --cflags --libs libavformat libavcodec libavutil) -lvulkan-1 -lwinmm
ffmpeg -f lavfi -i "testsrc2=size=3840x2160:rate=120,noise=alls=12:allf=t" -t 12 \
    -c:v hevc_nvenc -preset p1 -tune ull -rc cbr -b:v 40M -bf 0 -g 100000 -f hevc clip_40m.hevc
./decode_bench.exe clip_40m.hevc pull 120 10
BENCH_THREADS=2 BENCH_NOLOWDELAY=1 ./decode_bench.exe clip_40m.hevc pull 120 10
```

In a live session, the client logs its configuration at stream start:

- `libavcodec/win: decode mode = …, playout buffer …`
- `libavcodec/win: using hevc (… N decode threads, WxH@FPS)`

## Live result (same laptop, Linux USBridge agent, 4K@120 HEVC, 18 Mbps)

- The stream starts and runs at 110-125 fps decoded. The display stays at
  60, because of the 60 Hz panel and vsync.
- `SLOW video receive-loop iteration` and `vkAllocateDescriptorSets failed`
  are gone. The lag no longer builds up.
- Two startup fixes came out of live testing:
  - The decoder is now created in `dr_setup` rather than on the first frame.
    Vulkan Video session setup takes about 1 s and overflowed the queue.
  - The GUI's first frame no longer goes through a 4K CPU readback, which
    took about 270 ms. A nil-pixels frame is enough for the video widget to
    create the Vulkan overlay. CPU readback now runs on its own thread, and
    only for AI Vision, or as a fallback if the overlay is not up after 2 s.
- `avcodec_send_packet` returning `EAGAIN` used to count as accepted, which
  silently dropped the packet. It is now drained and retried.
- If no frame is decoded for 1 s, a `decode stall:` line is logged. It
  records the decode thread's stage, the in/out counters, the ffmpeg error
  codes and the RTP packet and FEC counters.

Two problems remained in live use of Vulkan Video decode:

- **Queueing latency.** With either 1 or 2 decoder threads, frames waited
  30-120 ms in the decode queue at 4K@120. The decoder only just kept up,
  so every GPU clock dip built a backlog. The same thing happened at a
  requested 60 fps, because the host actually sent about 107 fps.
- **Brightness flashes.** The renderer moved the decoder's image to
  `SHADER_READ_ONLY` behind ffmpeg's back. ffmpeg still used that image as
  a reference, so the following frames decoded with corrupted references.

## D3D11VA decode with a Vulkan interop (current default)

`ffmpeg -hwaccel <x> -threads N` on the 40 Mbps 4K clip, 780M, 3 runs each:

| decoder | throughput |
|---|---|
| D3D11VA, 1 thread | **232 fps** |
| Vulkan, 1 thread | 151 fps |
| Vulkan, 2 threads | 151 fps |

The earlier "1 thread 104 fps / 2 threads 202 fps" result for Vulkan did
not reproduce. Vulkan throughput on this iGPU varies with GPU state.

`d3d11_interop_windows.c` decodes with D3D11VA, which is also what
moonlight-qt uses on Windows. For each frame:

1. One `CopySubresourceRegion` copies the frame into a ring of 4 shared
   NV12/P010 textures.
2. Vulkan imports those textures (`VK_EXTERNAL_MEMORY_HANDLE_TYPE_D3D11_TEXTURE_BIT`).
3. A shared `ID3D11Fence`, imported as a Vulkan timeline semaphore, orders
   the copy before the render.

The renderer acquires each image from `VK_QUEUE_FAMILY_EXTERNAL` in
`GENERAL` layout and releases it the same way. It never touches an image
libavcodec uses as a reference.

`tools/decode_bench/d3d11_interop_bench.c` shows the interop is correct:
plane 0 read back through Vulkan byte-matches `av_hwframe_transfer_data`.
It also measures latency from scheduled arrival until the frame is readable
by Vulkan:

| 4K | D3D11VA + interop p50 / p99 | Vulkan decode p50 / p99 |
|---|---|---|
| 120 fps, 8 Mbps | 6.8-7.8 / 12-16 ms | 8.4 / 13-60 ms |
| 120 fps, 40 Mbps | 7.1-7.2 / 14-18 ms | 8.4-8.5 / 19-26 ms |
| 60 fps | 8-9 / 13-15 ms | 7-9 / 12-15 ms |

Live, 4K@120 HEVC from the Linux agent:

- Decode holds a steady 120 fps.
- Queue wait (`enqueueToSubmit`) is p50 17 µs and p90 73 µs, down from
  30-120 ms with Vulkan decode.
- No reconnects. A few queue overflows happened only in the first half
  minute.

If the interop cannot be set up, the client falls back to Vulkan Video
decode. Missing pieces include: no LUID match, no D3D11.4 fences, or no
external memory/semaphore extensions. `USBRIDGE_DECODER=vulkan` forces
the fallback for comparison.

## NVIDIA, HDR10 and GPU load

### NVIDIA mis-imports NV12

On an RTX 3090, a D3D11 NV12 texture imported into Vulkan behaves like this:

- Luma (plane 0) reads back byte-exact.
- About 90% of chroma (plane 1) bytes differ. No row offset explains the
  difference.
- The driver still reports NV12 import as supported.

On screen this shows up as ghosted or wrong colors.
`d3d11_interop_bench.c` checks both planes with `BENCH_VERIFY=N` and uses
`BENCH_VKDEV` to choose the GPU.

### Fix: convert to RGB on the D3D11 side

Single-plane RGB formats import byte-exact on both vendors (60 of 60
frames). So on every GPU except AMD, the D3D11 video processor
(`VideoProcessorBlt`) converts each frame into a shared RGB texture first:

- SDR goes to BGRA8, using BT.601 studio range to full range, the same
  conversion as the renderer's sampler.
- HDR10 goes to RGB10A2, kept as PQ / BT.2020.

AMD keeps the plain copy, because it is verified exact and about 2 ms
faster. `USBRIDGE_D3DX_MODE=nv12|rgb` overrides the choice.

### HDR10 output

- **Swapchain.** When a 10-bit frame arrives, the renderer switches to an
  HDR10 swapchain (`A2B10G10R10`, `HDR10_ST2084`). This only works while
  Windows HDR is on for that display, and needs `VK_EXT_swapchain_colorspace`.
- **Video.** Frames are already PQ, so they are shown as-is.
- **Overlays.** The HUD and AI Vision overlays use `hud_hdr.frag`, which
  maps sRGB to SDR white at 203 nits in PQ.
- **Requesting HDR.** On Windows, `HdrDisplaySupported()` is true only
  when both of these hold:
  - Windows HDR is on for the monitor the client window is on.
  - D3D11VA decodes HEVC Main10 to P010.

  The video dialog offers HDR only in that case, and `ConnectToMoonlight`
  re-checks at connect time. If it no longer holds, the client asks the
  host for SDR. An HDR stream therefore never reaches an SDR display in
  normal use.
- **Tone-mapping fallback.** The video processor's HDR-to-SDR tone mapping
  is still there, but only for Windows HDR being switched off mid-stream.

### GPU engine load

Measured with Windows "GPU Engine" counters for the bench process only,
4K@120 at 40 Mbps. This covers decode and transfer, not our draw:

| path | 3D engine | video engine | latency p50 / p99 |
|---|---|---|---|
| NVIDIA SDR (video processor to BGRA) | 0.5 % | 51 % | 6.7 / 14 ms |
| NVIDIA HDR10 (P010 to RGB10A2 PQ) | 0.5 % | 47 % | 6.1 / 14 ms |
| AMD SDR (NV12 copy) | 10 % | 90 % | 9.0 / 25 ms |
| AMD HDR10 to SDR tone map (fallback only) | 50 % | 91 % | 12.5 / 25 ms |

On NVIDIA the video processor runs on the video engine, so games sharing
the GPU are not affected.

## PyroWave on Windows (GPU only, zero-copy)

PyroWave is the intra-only wavelet codec that rust-shine hosts send.

On Linux and macOS the client decodes it with
`pyrowave_decoder_decode_cpu_buffer_synchronous`: PyroWave decodes on its own
Vulkan device, reads the frame back to the CPU, and the renderer uploads it
again. On Windows nothing leaves the GPU (`pyrowave_decode_windows.c`):

- **Device.** PyroWave runs on the renderer's own `VkDevice`, through
  `pyrowave_create_device` with create infos rebuilt from what ffmpeg enabled.
  Both GPUs here already enable everything PyroWave's decoder needs (Vulkan
  1.3 subgroup size control, 8/16-bit storage).
- **Queue.** PyroWave gets an async-compute queue that nothing else submits
  to. Its queue-lock callbacks share the lock the renderer takes around its
  own submits and `vkDeviceWaitIdle`.
- **Images.** PyroWave decodes straight into a ring of 4
  `G8_B8_R8_3PLANE_420` images, using R8 storage views of each plane. The
  ring uses concurrent sharing between the graphics and compute families, so
  no ownership transfer is needed, and stays in `GENERAL` layout. The renderer
  samples it through a BT.709 YCbCr sampler, as external mode 2.
- **Sync.** A timeline semaphore orders decode before sampling. A decode
  call only records and submits GPU work, so it runs inline on the pull
  decode thread.

### Rejected: separate PyroWave device

PyroWave on its own device, sharing images through exported `OPAQUE_WIN32`
memory, verified byte-exact on the Radeon 780M. On an RTX 3090 one image of
the ring intermittently came out with an empty luma plane (4 of 5 runs).
That held even with dedicated allocations and an explicit `GENERAL`
initialization.

### Verification and performance

`tools/decode_bench/pyrowave_bench.c` encodes 4K frames with PyroWave's own
encoder on this GPU and checks the decode against PyroWave's CPU decode.
With `BENCH_SHARED=1 BENCH_VERIFY=16` it is byte-exact on both GPUs, every
run.

Paced at 120 fps. Latency is from frame arrival to the frame being ready for
the renderer:

| GPU | 4K@120, 400 Mbps p50 / p99 | 1440p@120, 250 Mbps p50 / p99 |
|---|---|---|
| RTX 3090 | 2.1 / 7-10 ms | 1.5 / 7-8 ms |
| Radeon 780M | 3.0 / 4.8 ms | 2.6 / 3.5 ms |

There were no decode failures. GPU engine load during 4K@120 decode:

- **RTX 3090:** 3D/compute about 7%, copy engines about 3%.
- **Radeon 780M:** compute about 12% and 3D about 14%. The 3D sample may
  include the tail of the bench's encode phase. Copy is about 5%.

PyroWave is a shader codec and has no fixed-function decoder.

### Live test

`cmd/pyrowavesmoke` against the Linux agent, with the laptop on Wi-Fi:

- At 60 Mbps, 4K desktop: `negotiated=pyrowave frames=397 fps=49.6` over
  8 s, including stream start.
- At 150 Mbps and above, the Wi-Fi link drops packets ("Unrecoverable
  frame"). PyroWave's bitrates need a wired link or strong Wi-Fi.

### Color modes: 4:4:4 and HDR

A PyroWave stream can be 4:4:4, HDR, or both (see
[COLOR_MODES.md](./COLOR_MODES.md#pyrowave)). The decoder reads each
frame's sequence header and picks the ring format to match:

| Stream | Ring image | Plane views | Renderer YCbCr model |
|---|---|---|---|
| 4:2:0 SDR | `G8_B8_R8_3PLANE_420` | R8 | BT.709 |
| 4:4:4 SDR | `G8_B8_R8_3PLANE_444` | R8 | BT.709 |
| 4:2:0 HDR | `G16_B16_R16_3PLANE_420` | R16 | BT.2020, HDR10 swapchain |
| 4:4:4 HDR | `G16_B16_R16_3PLANE_444` | R16 | BT.2020, HDR10 swapchain |

- **Bit depth.** The wavelet itself has no bit depth. The host feeds it the
  tonemap's 10-bit samples, as P010-style values in R16. PyroWave decodes them
  back into R16 planes, where Vulkan's narrow range for 16-bit formats matches
  P010.
- **Format changes.** A change of chroma or bit depth rebuilds the decoder
  and ring, the same as a size change.
- **Sampler.** The renderer only uses cosited chroma and linear chroma
  filtering where the format supports them. The NVIDIA 4:4:4 formats don't
  offer cosited chroma, and 4:4:4 has no chroma to reconstruct anyway.
- **Capability.** `pyrowave_win_color_supported` checks that the GPU can
  write and sample each ring format. Only then does the client offer the
  matching `VIDEO_FORMAT_PYROWAVE_*` bit.

`tools/decode_bench/pyrowave_color_bench.c` compiles the production
`pyrowave_decode_windows.c` in, with stubs for the client hooks. For each
mode it:

1. Encodes 4K frames with PyroWave's encoder. The test content has
   1-pixel red/blue chroma stripes, which 4:2:0 would average away.
2. Stamps the header the way rust-shine does.
3. Decodes through `pyrowave_win_decode`.
4. Reads the ring back and compares it with PyroWave's CPU decode.
5. Builds the renderer's YCbCr sampler for the format.

Results, 3840x2160 at 400 Mbps (60 fps budget), 120 frames per mode. Latency
is submit to timeline semaphore, unpaced:

| Mode | RTX 3090 p50 / p95 | Radeon 780M p50 / p95 | vs CPU decode |
|---|---|---|---|
| 4:2:0 SDR | 1.06 / 1.85 ms | 2.38 / 2.85 ms | byte-exact |
| 4:4:4 SDR | 1.67 / 3.43 ms | 3.71 / 4.40 ms | byte-exact, stripes kept |
| 4:2:0 HDR | 1.30 / 3.53 ms | 2.62 / 3.36 ms | within 1 8-bit step |
| 4:4:4 HDR | 1.41 / 3.49 ms | 4.77 / 5.59 ms | within 1 8-bit step, stripes kept |

The renderer sampler builds for every format on both GPUs. The first frame of
each mode takes 12–200 ms, because it creates the decoder and ring.

The encoder's CPU input is 8-bit, so the 16-bit modes check the R16 path and
its scaling, not 10-bit precision itself. Real 10-bit content comes from the
host's tonemap.

### Building

`scripts/build_pyrowave_windows.sh` builds the vendored tree with MSYS2
UCRT64 (cmake + ninja) into one static archive. `build_windows.sh` calls it.

- A MinGW-only `CMakeLists.txt` hook force-includes `<windows.h>` for
  `pyrowave_c.cpp`, so the vendored tree stays untouched.
- On COFF only volk's `vk*` symbols are localized, so they can't shadow
  `vulkan-1.dll`. Localizing everything except `pyrowave_*`, as the ELF
  build does, breaks Granite's COMDAT template instantiations.
