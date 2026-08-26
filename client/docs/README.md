# USBridge Client Documentation

Technical reference for the USBridge Client — the cross-platform control app (Windows/macOS/Linux/Android/iOS/Web) that talks to either a [USBridge Hardware KVM](https://github.com/USBridge-Technologies/USBridge-KVM-2.0) or a [USBridge Agent](../../agent/docs/README.md) (software-only). See the [top-level README](../README.md) for downloads and feature highlights.

## Using the App

* **[Interface Guide](./interface-guide.md)** — the four tabs (Control, Devices, Snapshots, Scripts) and what each one does.
* **[Mouse & Touchpad Modes](./MOUSE_TOUCHPAD.md)** — relative/absolute pointer translation math.
* **[Virtual Keyboard](./virtual_keyboard.md)** — the on-screen keyboard used in fullscreen mode (mobile/touch).

## Platform-Specific Notes

* **[Native Video & Audio Pipeline](./NATIVE_VIDEO_AUDIO.md)** — the Vulkan/Metal zero-copy rendering stack.
* **[NBD on Android](./NBD_ANDROID_USAGE.md)**
* **[Android Video Testing](./ANDROID_VIDEO_TESTING.md)**
* **[Android Logcat](./ANDROID_LOGCAT.md)**

## Protocol & Security

* **[API Endpoints](./api_endpoints.md)** — the Master QR Sync pairing protocol and signed-request scheme. This is the *exact same protocol* whether the client is talking to a hardware KVM or a software Agent — see [Agent vs. Hardware KVM](../../agent/docs/README.md#agent-vs-hardware-kvm) for what differs above the protocol layer.
* **[Auto-Update](../../docs/AUTO_UPDATE.md)** — how the client verifies and applies its own updates.

## Related

* **[Agent Documentation](../../agent/docs/README.md)** — for the machine being controlled, if it doesn't have a hardware KVM.
* **[USBridge-KVM 2.0 Documentation](https://github.com/USBridge-Technologies/USBridge-KVM-2.0/tree/main/docs)** — for the hardware KVM itself: BIOS-in-Terminal, Starlark scripting, MCP AI-agent integration, immutable snapshots, and the full REST API reference. The **Scripts** tab and its [MCP proxy](https://github.com/USBridge-Technologies/USBridge-KVM-2.0/blob/main/docs/content/3-bios-in-terminal/mcp-ai-agents.md) only do anything when connected to a hardware KVM — see the feature comparison linked above.
