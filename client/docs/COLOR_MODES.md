# HDR and 4:4:4 color

The video dialog has two color options for H.265 and PyroWave streams from a
RustShine host: **4:4:4 chroma** (full-resolution color, sharper text, Pro)
and **HDR** (BT.2020 + PQ, 10-bit; HEVC Main10 on H.265; free). They are
independent. Encoding happens on
the host, so availability depends on the host's GPU and -- 4:4:4 only -- the
license; HDR needs no license, just hardware that can do it. The
streamer-side details, including how each mode was verified, are in
rust-shine's `docs/COLOR_MODES.md`.

## When the options are enabled

A row is enabled only when all of these hold:

1. H.265 or PyroWave is selected. Ticking a row on another codec switches to
   H.265 if the host offers the option there, otherwise to PyroWave.
2. The agent reports it available for that codec, passed through unchanged
   from the streamer's `/api/status`:
   - H.265: `color_444_available` / `hdr_available`. For 4:4:4 that means the
     host's HEVC encoder can produce it **and** the license is Pro or
     Enterprise. For HDR it means only that the encoder can produce it.
   - PyroWave: `pyrowave_color_444_available` / `pyrowave_hdr_available`.
     Any GPU that encodes PyroWave encodes both, so only the license applies
     (4:4:4 Pro, HDR free).
3. HDR only: this client can display HDR (`service.HdrDisplaySupported`). If
   the host offers HDR but this client can't show it, the row says so instead
   of the hardware-unavailable hint.
4. PyroWave only: this client can decode that mode
   (`service.PyroWaveColorDecodeSupported`). Today that is Windows only.

## Host support (what the agent reports)

| Host | GPU / encoder | 4:4:4 | HDR |
|---|---|---|---|
| Linux | NVIDIA (NVENC) | yes | yes |
| Linux | Intel / AMD (VAAPI) | yes | no (not implemented in the streamer) |
| Windows | NVIDIA (NVENC) | yes | yes |
| Windows | Intel (QSV) | yes | yes |
| Windows | AMD (AMF) | no (AMF has no 4:4:4 profile) | written, not verified on AMD hardware |
| macOS | Apple Silicon (VideoToolbox) | no (no 4:4:4 encode in VideoToolbox) | yes |
| SBC (Rockchip / Allwinner) | hardware encoder | no | no |

With H.265, choosing HDR and 4:4:4 together negotiates HDR only: the host
doesn't advertise the combined 10-bit 4:4:4 HEVC mode yet. PyroWave does
the combined mode.

## PyroWave

PyroWave (the intra-only wavelet codec) takes both options on every Linux host
GPU that encodes it; Windows and macOS hosts encode PyroWave 8-bit 4:2:0 only
and don't offer them. The host's Vulkan tonemap writes 4:4:4 as three full-size
planes and HDR as 10-bit BT.2020 PQ planes (R16), and the PyroWave encoder
reads them straight from GPU memory. The stream's sequence header says what
each frame is: chroma resolution, primaries, transfer function and matrix.

Negotiation (moonlight-common-c fork):

- Format bits: `VIDEO_FORMAT_PYROWAVE` `0x10000`, `_444` `0x20000`, `_HDR`
  `0x40000`, `_444_HDR` `0x80000`. The client sends every bit it can decode
  for the chosen options, plus plain PyroWave and H.264 as fallbacks.
- The host's DESCRIBE reply adds `a=x-usbridge-pyrowave:444` while the Pro
  license is active and `a=x-usbridge-pyrowave:hdr`. The client picks the
  best mode both sides have, down to plain PyroWave.
- The SDP then carries `chromaSamplingType` / `dynamicRangeMode` as for H.265.
- A host without these lines (an older rust-shine) negotiates plain PyroWave.
  This was checked live: `0xF0001` requested, `0x10000` negotiated.

The Windows client decodes all four modes on the GPU. See
[WINDOWS_DECODE_PIPELINE.md](./WINDOWS_DECODE_PIPELINE.md#color-modes-444-and-hdr).
The Linux and macOS decoders are 8-bit 4:2:0 only, so those clients never ask
for the PyroWave options. The macOS one is PyroWave's native Metal port
(`third_party/pyrowave/metal`), decoding straight into the IOSurface the
renderer shows.

On a Linux host whose desktop is itself in HDR mode (KDE/GNOME HDR on), an
HDR stream carries the desktop's HDR picture as-is. An SDR stream from that
same desktop is tonemapped back to SDR on the host, so it doesn't look grey.

## Client support (what this app can show)

| Client | 4:4:4 | HDR |
|---|---|---|
| macOS | decodes (not re-checked in this pass) | **yes**: VideoToolbox decodes to 10-bit, Core Animation shows it with EDR |
| Linux | decodes via the CPU fallback (`sws_scale` to RGBA), slower than the GPU NV12 path | no: the option is disabled (8-bit render path, no PQ handling) |
| Windows | decodes (H.265 and PyroWave) | **yes** when Windows HDR is on for the monitor the client window is on, and (H.265) D3D11VA decodes HEVC Main10: HDR10 Vulkan swapchain (PQ, BT.2020); otherwise the option is disabled and the client asks the host for SDR. PyroWave HDR uses the same swapchain. See [WINDOWS_DECODE_PIPELINE.md](./WINDOWS_DECODE_PIPELINE.md#hdr10-output) |
| Android / iOS / Web | not checked | no: the option is disabled |

## Host load (Linux NVIDIA, RTX 2080 Ti, 3840x2160 at 60 fps)

Measured on the host with the streamer's own capture/encode loop, 2026-09-30.
Latency is capture to encoded frame. CPU is the streamer process, where 100%
is one core. ENC is the NVENC unit's utilization.

| Mode | Latency avg / p99 | CPU | NVENC | GPU power |
|---|---|---|---|---|
| SDR 4:2:0 | 8.1 / 8.3 ms | 5% | 40% | 76 W |
| HDR 4:2:0 | 7.9 / 8.1 ms | 5% | 40% | ~79 W |
| SDR 4:4:4 | 11.0 / 12.0 ms | 5% | 58% | ~85 W |
| HDR 4:4:4 | 11.1 / 11.3 ms | 6% | 59% | 86 W |

HDR adds nothing measurable. 4:4:4 adds about 3 ms and roughly half again
the encoder load. It fits 4K60, not 4K120. A 3-minute HDR 4:4:4 run held
59.7 fps with no dropped frames. Full numbers:
rust-shine `docs/COLOR_MODES.md`, "Linux NVENC".
