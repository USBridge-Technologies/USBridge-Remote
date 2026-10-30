# USBridge Agent Documentation

The Agent turns any Windows, macOS, or Linux machine into something the [USBridge Client](../../client/docs/README.md) can connect to and control — **no hardware required**. It shares the same pairing protocol and much of the same codebase as the physical [USBridge-KVM 2.0](https://github.com/USBridge-Technologies/USBridge-KVM-2.0) appliance, but it's software running *on* the machine you're accessing, not a hardware bridge sitting *in front of* it. See the [top-level README](../README.md) for downloads and a feature overview.

---

## Agent vs. Hardware KVM

This is the one thing to understand before anything else: the client talks to both the same way, but they are not equivalent.

| Capability | Software Agent | Hardware USBridge-KVM |
| :--- | :---: | :---: |
| Screen capture | ✅ (software, via Sunshine/RustShine) | ✅ (direct HDMI capture, works pre-OS/BIOS-level) |
| Keyboard/mouse input | ✅ (software injection: `SendInput`/`CGEvent`/direct) | ✅ (physical USB HID gadget — works before any OS loads) |
| System audio capture | ✅ | ✅ (HDMI audio extraction) |
| Power / hard reset control | ❌ | ✅ — [Power Management Module](https://github.com/USBridge-Technologies/USBridge-KVM-2.0/blob/main/docs/content/6-hardware-connectivity/power-management-module-control.md) |
| Virtual media (mount `.iso`/`.img`) | ❌ | ✅ |
| Internet sharing to the target | ❌ | ✅ — USB-LAN/RNDIS bridge |
| Versioned/immutable backup storage | ❌ | ✅ — Btrfs snapshots |
| MCP server (AI agent access) | ✅ — smaller tool catalog, see below | ✅ — full catalog incl. `mountdrive.*`/`scripts.*`/`pcpanel.*` |
| UI element detection (`ui.parse`) + click-at-detected-element | ✅ — via the Client's local ONNX offload, see below | ✅ — native, on the KVM's own NPU |
| Remote Starlark script execution | ❌ | ✅ |

The rule of thumb: the Agent gives you everything a **software remote-desktop tool** can give you — because that's exactly what it is, an OS-level agent. Anything that needs to act **before or independently of** that OS — power-cycling a frozen machine, mounting install media for a bare-metal OS install, reading BIOS/UEFI screens, surviving that OS being fully compromised — needs the physical hardware KVM instead. If you need that, the Agent and the hardware unit aren't competing options; they're complementary, and the same client manages both from one dashboard.

---

## USB Capture & Host-Side Emulation

There are two independent things going on under "USB support," and they don't follow the same rules:

- **Standard input** (keyboard, mouse, gamepad) travels over the normal Moonlight streaming protocol — no physical device capture involved, works between any Client/Agent OS pair, and is **always free**.
- **USB passthrough** captures a *real, specific* physical device plugged into the Client machine and makes it appear on the Agent machine. This needs the Client to actually hold that device and the Agent to import it — capability differs by OS on both ends, and which *devices* are free vs. paid depends on the device's own USB class, not on the OS or the mechanism used to move it.

**Client-side capture — how a physical device plugged into the Client is captured, by OS:**

| Client OS | Boot keyboard / mouse | Physical gamepad (Xbox-class) | Any other USB device (drives, tablets, audio, vendor…) |
| :--- | :--- | :--- | :--- |
| Windows | HID report descriptor reconstruction (Windows holds the real device exclusively, so raw claim isn't possible) | Real pad state read and re-sent as clean XInput/GIP | Raw `libusb` passthrough |
| Linux | Raw `libusb` passthrough | Real pad state read and re-sent as clean XInput/GIP | Raw `libusb` passthrough |
| macOS | HID report descriptor reconstruction | Not captured as a physical device — use the Moonlight controller input path instead | Not supported (HID-class devices only) |
| Android | Best-effort via the Android USB host API (per-device permission grant) | — | Best-effort via the Android USB host API |

**Host-side (Agent) emulation — what actually appears on the Agent machine, and what it costs:**

| What reaches the Agent | How it's presented on the Agent | Agent OS | License |
| :--- | :--- | :--- | :--- |
| Keyboard / mouse (Moonlight input) | Native injection — `SendInput` / `CGEvent` / direct | Windows, macOS, Linux | **Free** |
| Gamepad (Moonlight input) | Synthetic XInput pad over the local USB stack (Windows) / `uinput` virtual pad (Linux) — no ViGEmBus needed | Windows, Linux | **Free** |
| Gamepad (Moonlight input) | Not implemented yet | macOS | — |
| Boot keyboard / mouse **passed through physically** | Real device imported over the USB passthrough stack | Windows, Linux Agent | **Free** |
| Physical gamepad (Xbox-class interface) **passed through physically** | Real device imported over the USB passthrough stack | Windows, Linux Agent | **Free** |
| Any other physical device (drives, tablets, audio, vendor devices…) | Real device imported over the USB passthrough stack | Windows, Linux Agent | **Pro / Enterprise** |

The Agent's USB passthrough component is a separate, closed-source binary from the rest of this (open-source) Agent — it never downloads or runs on its own; it only starts after you explicitly enable it from the USB status row in the main window. Which specific device classes are free is decided by that component itself at connect time, from the device's own real USB descriptors — never by anything this Agent reports about itself.

---

## MCP / AI Agent Access

Both the Agent and the hardware KVM answer the same `POST /api/mcp` JSON-RPC 2.0 endpoint (HMAC-signed, same scheme as every other request), and the [Client](../../client/docs/README.md) exposes a local proxy (`client/internal/api/mcp_proxy.go`, default `http://127.0.0.1:8765/api/mcp`) so a native AI tool on the Client machine can reach whichever device is currently connected without signing requests itself.

**Tool catalog.** The Agent's is smaller than the hardware KVM's (`agent/internal/api/mcp.go`'s `mcpToolCatalog`) — no `mountdrive.*`/`media.*`/`rndis.*` (input works directly, no USB-HID gadget to arm), no `scripts.*` (no on-device Starlark engine), no `pcpanel.*` (no physical front panel). What's left covers driving the OS UI and seeing the result: `screen.get_image`, `keyboard.send`, `mouse.action`, `device.info`.

**Clicking on a detected UI element.** The hardware KVM's `ui.parse` (YOLOv8 + DBNet/SVTR on its NPU) returns pixel bounding boxes for every icon/button/text on screen; its `mouse.action` `move_to`/`click_at`/`double_click_at` actions take a pixel x/y plus the capture's `screen_width`/`screen_height` and click there in one step. The Agent didn't have either half of that until recently:

- *Detection* — the Agent itself has no NPU/ONNX pipeline, so it never lists `ui.parse`. If the connected Client has its own local `ui.parse` offload enabled (an ONNX pipeline running the same three models on the Client's own CPU/GPU — see [`client/internal/localui`](../../client/internal/localui/models/README.md)), the Client's MCP proxy answers `ui.parse` on the Agent's behalf. If a video session is already streaming, it skips the extra device round trip entirely and detects against the frame the Client is already decoding (`api.RequestLiveFrame`, served instantly from the same stable "last rendered frame" the pause-snapshot display uses — see `client/internal/api/live_frame.go`); only with no active stream does it fall back to fetching a screenshot via the Agent's `screen.get_image`. The proxy also injects `ui.parse` into the Agent's `tools/list` response when this is active (see `client/internal/api/local_ui_intercept.go`'s `injectLocalUIParseTool`), so an MCP client that discovers tools by listing them — not by calling `ui.parse` blind — still finds it.
- *Fast by default* — `ui.parse` (no arguments, or `{"text":false}`) runs icon/element detection only, sub-second on a 4K frame — enough to locate and click something. Pass `{"text":true}` to also run OCR (DBNet+SVTR) and get recognized text back, several seconds slower on a busy screen; an agent that just needs to click shouldn't pay that cost on every call.
- *Clicking* — `agent/internal/api/mcp_mouse_absolute.go` adds `move_to`/`click_at`/`double_click_at` to the Agent's `mouse.action`, using the exact same pixel→normalized-axis conversion (`pixelToAbsoluteXY`, a 0..32767 axis) as the hardware KVM, so a box's pixel center from `ui.parse` lands in the right place regardless of which backend answered it. `click_at`/`double_click_at` take a before/after screenshot diff (pure Go, no OpenCV) around the click and return `{status, screen_changed_pct, screen_visibly_changed}`, matching the hardware KVM's own confirmation shape. The Client overrides these fields with its own diff computed from its before/after video frames when a session is streaming (`client/internal/api/mouse_click_diff.go`) — the Agent's own capture backing this diff can be unreliable on Linux (see below), and the Client's frames are the real picture regardless.

Net effect: an MCP client following `ui.parse`'s documented recipe ("compute the box center, pass it plus `image_width`/`image_height` to `mouse.action`'s `move_to`/`click_at`") doesn't need a different code path depending on whether it's driving a hardware KVM or a software Agent.

**`screen.get_image` is Client-answered, not Agent-captured.** Same mechanism as `ui.parse` above: when a video session is streaming, the Client answers this tool too (`client/internal/api/local_ui_intercept.go`'s `tryLocalScreenImage`) directly from its own decoded frame — the Agent is never asked at all. This is deliberate, not just an optimization: the Agent has no reliable capture of its own on Linux (`agent/internal/capture/screen_linux_x11.go`'s `Snapshot()` always returns an error there), since the only thing available to it, `kbinani/screenshot`, either hangs 30-45s on an interactive Wayland portal dialog nobody's there to answer, or returns a blank frame on a compositor that blocks plain X11 reads for privacy — neither worth having as a silent fallback. So `screen.get_image`/`ui.parse` on Linux **require a Client connected and its video view open**; with no stream active, both cleanly error instead of guessing. A real Agent-side capture belongs in the streaming backend that already captures real frames for encoding, not as a bolted-on fallback here — see `rust-shine/LINUX_SCREENSHOT_EXPORT_TODO.md`. `click_at`/`double_click_at`'s own before/after diff is answered the same way: the Client recomputes `screen_changed_pct`/`screen_visibly_changed` from its own frames (`client/internal/api/mouse_click_diff.go`) whenever a stream is active, since the Agent has nothing reliable to offer there either.

**Typing text on Linux (`keyboard.send` action `text`).** Unlike macOS (`CGEventKeyboardSetUnicodeString`) and Windows (`VK_PACKET`), Linux's `uinput` has no direct Unicode-injection primitive — it only emulates HID scancodes, physical key positions the host's own active keyboard layout turns into characters. `agent/internal/input/controller_linux.go`'s `Text()` handles this by splitting the input into runs per script (Latin vs. Cyrillic, `keys_linux.go`'s `scriptLayout`/`splitTextRuns`) and switching the host's active layout to match each run (`SetKeyboardLayout`, the same `org.kde.keyboard` D-Bus mechanism the explicit layout-switch endpoint uses) before driving the corresponding positional HID codes (`asciiToHID` for Latin, `cyrillicToHID` for а ЙЦУКЕН-position Cyrillic). A character in neither table (CJK, emoji, accented Latin, …) is silently skipped. **This is newly implemented and needs more real-world testing** — confirmed live that keystrokes reach the kernel input device correctly (`evtest` on the target sees them), but getting them to land in the intended application's focused text field on a Wayland/KDE target was still unreliable in initial testing (a synthetic absolute-mouse click at a text field's coordinates doesn't always transfer keyboard focus there the way a real click does) and needs more investigation before this should be considered solid.

---

## Quick Start

1. Run the Agent on the machine you want to access. It displays a Master QR pairing token plus its LAN and Tailscale addresses.
2. Open the Client anywhere else, scan (or enter) the token, and connect.

That's the whole setup. See the [top-level README](../README.md#-quick-start) for platform-specific download links.

---

## Reference

* **[Auto-Update](../../docs/AUTO_UPDATE.md)** — how the Agent verifies and applies updates, including headless/silent-update behavior and the separate RustShine update channel.
* **[Shared Pairing](SHARED_PAIRING.md)** — how Sunshine/RustShine/Punktfunk share one TLS identity and trust list, so switching streamers (or unpairing a device) doesn't require re-pairing with each one separately.
* **[API Endpoints](../../client/docs/api_endpoints.md)** — the Master QR Sync pairing protocol and signed-request scheme; identical whether the client is talking to an Agent or a hardware KVM.
* **[Security & Authentication Model](https://github.com/USBridge-Technologies/USBridge-KVM-2.0/blob/main/docs/content/10-developer-api/security-model.md)** — the same layered pairing/signing/streaming security model used across the whole USBridge ecosystem, written up in full on the hardware KVM's docs (the Agent doesn't have a separate write-up because there's nothing different to say — it's the same scheme).

### Platform Notes (from the top-level README)

* **Wayland (Linux):** full screen capture and input injection with no permission-prompt spam — KMS capture needs one `pkexec` grant, which persists across reboots and streamer updates (see [KMS_CAPTURE_LINUX.md](KMS_CAPTURE_LINUX.md)).
* **System Tray:** closing the window minimizes to a tray icon (status-aware, with Open/Restart Streaming/Autostart/Quit) instead of quitting; falls back to actually quitting on a Linux session with no reachable tray host (e.g. GNOME without the AppIndicator extension). Stays visible even when the engine runs headless — see [Launch at Login](../README.md#-launch-at-login-autostart) for how each platform gets a tray icon onto an otherwise-invisible background instance.
* **Launch at Login:** reflects your OS's actual autostart state live (no separate on/off flag of its own); always launches with `--headless` so the engine comes up silently and a later normal launch just attaches a GUI to it.
* **GPU Clock Lock (Windows + NVIDIA):** holds an NVML max-clock lock for the streaming session so the encoder doesn't stall waiting on a GPU that idled down between frames.
* **RustShine / WebRTC (Patreon):** the standard Sunshine backend doesn't support WebRTC, so the [Web Client](https://web.usbridge.io) needs the Patreon-gated RustShine streaming engine to connect to an Agent. Toggle it from the Permissions column once unlocked.
* **HTTPS Certificate:** RustShine's WebRTC also needs the Agent's HTTPS listener to present a certificate the browser trusts, not the self-signed one it generates on first launch — a self-signed cert gets silently rejected by `https://web.usbridge.io`'s background fetch/WebSocket calls (mixed-content/untrusted-TLS, no click-through the way a top-level page navigation gets). To fix that, the Agent registers its own `<label>.device.usbridge.io` hostname with USBridge's backend right away (automatic, also after a network change) and gets a browser-trusted certificate for it (Google Trust Services) **when it's actually needed**: the first time a browser opens the Web Client on that hostname, or when you press **Retry** in the Certificate dialog. This needs you to be **signed in to your USBridge account** in the Agent (the certificate is tied to that account); signed out, the Certificate dialog says so. The very first browser attempt can fail while the certificate is issued — try again a few minutes later. A certificate nobody used for 30 days isn't renewed. The Status panel's **Certificate** row shows which one is active right now (tap the ℹ for the hostname and expiry); until it shows the trusted hostname, only the Web Client is affected — Sunshine, Moonlight, and the native Client all work over the self-signed cert exactly as before.
* **Audio device (Linux):** the Agent checks the saved `audio_sink` against the live PulseAudio/PipeWire sinks before every streamer start. If that device no longer exists (e.g. an HDMI output that went away with a hardware change), it resets to the system default instead of letting Sunshine stream silence ("No such entity"). Both Sunshine and RustShine make the chosen sink the default for the duration of the stream, so what you hear on the host is what gets streamed.
* **Sunshine build (Linux):** the bundled Sunshine comes from the usbridge fork's CI release, built with the CUDA toolkit (NVENC CUDA path) and merged with upstream. A local source build (only when no release exists) picks its parallel job count from available RAM (`USBRIDGE_SUNSHINE_JOBS` overrides it).
* **Sunshine on Windows is downloaded, not bundled:** the Windows Agent ships without a streamer. The first time Sunshine is the streamer (it is a fallback: a fresh install downloads RustShine, the default, and fetches Sunshine only if that download fails, or when Sunshine is picked) the Agent downloads the usbridge fork's Windows build from the latest Streamers-Forks release into its state folder (`sunshine-host`), checking the release manifest's Ed25519 signature and the archive's SHA-256 before anything is put in place, then starts it. Every 6 h it looks for a newer release and installs it, waiting while a client is streaming; the Sunshine card's ↻ glyph checks right away (and moves an older install off the Sunshine it was bundled with). Linux and macOS still bundle Sunshine: the Linux KMS grant covers only a root-owned installed tree.
* **Virtual display driver (Windows setup):** the virtual display driver (MttVDD, for streaming with no monitor attached) is its own checkbox in both Windows setups, under the Agent in the all-in-one setup. It is ticked by default on a machine with a real GPU, a VM's passed-through one included, and unticked on a virtual machine whose only display adapters are virtual (VirtualBox, VMware, Hyper-V, virtio, Parallels, QEMU): there the VM's own screen is what gets streamed, and the extra monitor would extend the desktop off the VM window, offsetting the mouse. Tick it to install it anyway; the Agent's own **Install** button for it works everywhere too.
* **Software OpenGL (Windows):** the Agent's window needs OpenGL 2.1, which a VM without 3D acceleration or a remote desktop session doesn't have (Windows' own OpenGL there is 1.1). The Windows Agent ships Mesa's llvmpipe software OpenGL in `softgl\` next to its executable; at start the window checks the system OpenGL and, only when it falls short, uses that one instead. With a real GPU driver it is never loaded.
* **Ports:** the API port (8080) and the Sunshine port (47990) can be changed in the agent. Clients take the API port as part of the address (`192.168.1.5:9090`, `[fd7a::1]:9090`): the agent's QR code and quick-connect link include it whenever it isn't 8080, and it can be typed into the client's address field. Moonlight's ports follow the Sunshine port the agent reports in `/api/video/info` (`sunshine_port`: NvHTTP one below it, HTTPS six below).
* **Streamer benchmark:** `/api/bench/*` lets the client switch the backend, and play a fast-moving test video fullscreen on the host, for its Sunshine vs RustShine comparison. See [`../../client/docs/STREAMER_BENCHMARK.md`](../../client/docs/STREAMER_BENCHMARK.md).
* **Host keyboard layout (KDE Wayland):** `GET/POST /api/keyboard/layout` reads or switches kwin's active layout over the `org.kde.keyboard` D-Bus service. The client uses it to type Cyrillic through Sunshine: Sunshine's Linux Unicode injection (an IBus Ctrl+Shift+U hex sequence) prints the hex digits outside GTK apps. The endpoint returns 501 on other desktops and OSes.
* **Mouse never stays held:** a mouse button held over the Agent's input WebSocket is released when the connection closes, when a new client connects, or after 4 s with no traffic from the client (answered pings count). An HTTP press that isn't followed up is released after 5 s. RustShine does the same for its own input channel: it releases on reconnect or after 3 s of control-channel silence.
* **Lock screen / UAC (Windows):** the Windows Agent runs the streaming backend as SYSTEM inside the active session rather than the logged-in user's own token, so capture and input keep working straight through Win+L, sign-out to the logon screen, and UAC prompts — not just the ordinary unlocked desktop. It can also raise a synthetic Ctrl+Alt+Del on demand (`App.SendSAS()` / `POST /token/send-sas` on the Agent's local admin API) for machines where "require CTRL+ALT+DEL" is enabled, since Windows deliberately blocks ordinary keyboard injection from reaching that gesture on its own.

See [`../README.md`](../README.md) for the full detail on each of these — this page indexes it, it doesn't duplicate it.
