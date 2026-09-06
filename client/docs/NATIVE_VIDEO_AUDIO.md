# Native Hardware Video & Audio (Moonlight Mode)

Moonlight streaming uses platform-native hardware APIs for zero-subprocess,
zero-pipe video decode and audio output on every platform, relying solely on hardware acceleration via Vulkan and Metal.

## Architecture

```
Sunshine (server) → H.264 RTP → moonlight-common-c
                                        │
                                  dr_submit (C callback)
                                        │
                          ┌─────────────▼────────────────┐
                          │   platform_dr_submit (CGO)   │
                          └─────────────┬────────────────┘
                                        │ GPU texture
                                  goVTFrame (Go callback)
                                        │
                             vtFrameCallback (Go func)
                                        │
                           Vulkan / Metal Hardware Context

Opus packets → ar_decode (C) → platform_ar_decode (CGO) → native audio API
```

## Per-platform implementation

| Platform | Video decoder & Rendering | Audio output | File |
|----------|--------------|--------------|------|
| macOS | Metal + VideoToolbox `VTDecompressionSession` | CoreAudio `AudioQueue` | `moonlight_cgo_apple.go` |
| iOS | Metal + VideoToolbox | CoreAudio `AudioQueue` | `moonlight_cgo_apple.go` |
| Linux | Vulkan + libavcodec (`h264_vaapi` / `h264_nvdec`) | ALSA `snd_pcm_writei` | `moonlight_cgo_linux.go` |
| Windows | Vulkan + libavcodec `h264_d3d11va` | WASAPI `IAudioRenderClient` | `moonlight_cgo_windows.go` |
| Android | Vulkan + `AMediaCodec` NDK | `AAudio` NDK (low-latency) | `moonlight_cgo_android.go` |

## C interface (moonlight_cgo_shared.h)

Each platform CGO file implements these four functions:

```c
void platform_ar_init(int channels, int sample_rate);   // open audio device
void platform_ar_cleanup(void);                          // close audio device
void platform_ar_decode(const opus_int16 *pcm,
                        int byte_count, int samples);    // play decoded PCM
int  platform_dr_submit(PDECODE_UNIT du);                // decode H.264 frame
void platform_post_stop(void);                           // teardown after stream stop
```

The shared header provides: opus decoder, connection callbacks, `do_li_start/stop`, input forwarders.

## Build dependencies

### macOS / iOS
No external dependencies — Metal, VideoToolbox and CoreAudio are Apple system frameworks.
```
-framework Metal -framework VideoToolbox -framework CoreMedia -framework CoreFoundation
-framework CoreVideo -framework AudioToolbox
```

### Linux
Install at **build time** (headers) and **run time** (shared libs):
```bash
# Build deps
sudo apt-get install -y libavcodec-dev libavutil-dev libswscale-dev libasound2-dev libvulkan-dev

# Runtime (target machine)
sudo apt-get install -y libavcodec60 libavutil58 libswscale7 libasound2 libvulkan1

# Optional hardware acceleration
sudo apt-get install -y libva2 libva-drm2        # Intel/AMD VA-API
# NVIDIA: install nvidia-driver with CUDA support
```

### Windows
FFmpeg MinGW shared build (avcodec, avutil, swscale) + system WASAPI and Vulkan drivers.

Download FFmpeg: https://github.com/BtbN/FFmpeg-Builds/releases  
Pick: `ffmpeg-master-latest-win64-gpl-shared.zip`

```bash
export FFMPEG_ROOT=/path/to/ffmpeg-win64-gpl-shared
scripts/build_windows.sh
```
The build script copies `avcodec-*.dll`, `avutil-*.dll`, `swscale-*.dll` into `dist/windows/`.

### Android
`AMediaCodec` and `AAudio` are part of the Android NDK. Vulkan API is loaded dynamically.  
Link flags: `-lmediandk -laaudio -landroid -lvulkan`

Note: moonlight-common-c must be compiled for Android ARM64 (NDK toolchain).

## AI Vision live overlay

The "AI Vision" checkbox (video start dialog) reuses the local ui.parse
ONNX pipeline (see the client README's "Local ui.parse Offload" section)
against the live video feed instead of a static screenshot. How the
resulting detection boxes reach the screen depends on whether a
platform's decode path above ever produces a CPU-writable frame:

| Platform | Video path | Overlay mechanism |
|----------|-----------|--------------------|
| Linux | Vulkan (CPU RGBA uploaded to a texture each frame) | Drawn in place into the RGBA buffer before `vk_video_try_submit` (`ai_vision.go`'s `ApplyAIVisionOverlay`) |
| macOS, CPU-fallback decode | Only taken when `metal_video_try_submit` declines | Same in-place RGBA drawing, in `vt_callback`'s fallback branch |
| macOS, Metal fast path (common case) | Zero-copy IOSurface → `CVMetalTextureCache`, no CPU pixel access ever | A second transparent `CALayer` (`g_overlay_layer` in `metal_video_impl_darwin.m`) stacked above the video IOSurface layer, updated only once per completed detection pass (~every 2s) — Core Animation composites it on the GPU for free every frame in between. Kicking off a fresh detection pass costs one occasional CPU readback of the `CVPixelBufferRef` (gated by `goAIVisionShouldSample`), not a per-frame cost. |
| Android / Windows (Vulkan `AHardwareBuffer` zero-copy) | Zero-copy, no CPU pixel access | Not wired up yet — would need the same native-compositor-layer approach as macOS's Metal path (see `VulkanOverlayBridge.kt`'s cursor overlay for the existing Android pattern to extend) |
| iOS | Metal, same zero-copy shape as macOS | Not wired up yet |

## Performance

| Metric | New Native Pipeline (Vulkan/Metal) |
|--------|------------------------------|
| Processes | 0 extra processes |
| OS pipes | 0 |
| IPC latency | 0 ms |
| Video decode | Hardware GPU always |
| Audio | Native API (CoreAudio / ALSA / WASAPI / AAudio) |
| Startup time | ~50 ms (codec open) |
| CPU video (1080p30) | <2% (GPU hardware) |
