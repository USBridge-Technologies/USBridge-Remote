# USBridge Remote (Beta)

![USBridge Remote](assets/banner.png)

<div align="center">

[![Beta](https://img.shields.io/badge/status-beta-orange)](https://github.com/USBridge/USBridge-Remote-Beta/releases)
[![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)](#download)
[![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)](#download)
[![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)](#download)
[![Android](https://img.shields.io/badge/Android-3DDC84?logo=android&logoColor=white)](#download)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.com/invite/xqQ6ybkfWS)

</div>

---

**USBridge Remote** is a unified high-performance client for managing remote machines. I designed it to combine **hardware-level BIOS access** (via USBridge KVM devices) and **software-based remote desktop** in a single, streamlined interface.

> ⚠️ **Beta Software** — This is an early release. Expect bugs. Please report issues via [GitHub Issues](https://github.com/USBridge/USBridge-Remote-Beta/issues) or join our [Discord](https://discord.com/invite/xqQ6ybkfWS) for support.

---

## Download

### Client
The Client is the control interface — installed on your workstation or laptop. It manages connections, live remote desktop, virtual device passthrough, and snapshot registry.

| | Windows | macOS | Linux | Android |
|:---|:---:|:---:|:---:|:---:|
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/download/v2.0.0/USBridge_client_windows_amd64-2.0.1.zip) | | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/download/v2.0.0/USBridge_client_linux_amd64-2.0.1.tar.gz) | |
| **ARM64** | | *In progress (signing)* | | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/download/v2.0.0/USBridge_client_android_arm64-2.0.1.apk) |

### Agent
The Agent runs on the target machine — the server or PC you want to access remotely. It handles screen capture, input injection, and Tailscale networking.
| | Windows | macOS | Linux |
|:---|:---:|:---:|:---:|
| **x86_64** | [Download](https://github.com/USBridge/USBridge-Remote-Beta/releases/download/v1.0.0-alpha/USBridgeAgent-1.0.0-windows-x86_64.zip) | | 🚧 |
| **ARM64** | | [Download](https://github.com/USBridge/USBridge-Remote-Beta/releases/download/v1.0.0-alpha/USBridgeAgent-1.0.0-macOS-arm64.zip) | 🚧 |

---

## Features

![USBridge Remote — Remote Desktop](assets/screenshot_USBridge_client.png)

**One place for everything** — I've unified the workflow. Manage USBridge KVM hardware and software agents from a single dashboard. Add a machine, connect, and you're in.

**No limits, no subscriptions** — Completely free. No session time limits, no connection caps, and no account required on the target machine.

**Low-latency video** — My adaptive streaming engine selects the most efficient protocol based on your connection. Enjoy up to 2K resolution with minimal lag thanks to FFmpeg hardware acceleration (DXGI, AVFoundation, VAAPI).

**Tailscale integration** — Built-in encrypted P2P tunneling. Connect to any machine globally without messing with port forwarding or firewall rules. It works on LAN and over the internet automatically.

![USBridge Remote — Agent](assets/screenshot_USBridge_agent.png)

---

## Quick Start

1. **Install the Agent** on the machine you want to access remotely. Launch it — it will display a connection token and Tailscale address. Connect Tailscale if you need access over the internet.

2. **Install the Client** on your workstation, laptop, or phone.

3. **Add a connection** — enter the IP or Tailscale address shown in the Agent window. That's it.

---

## Community & Beta Testing

Join our Discord to get the **Beta Tester** role, report bugs, and help me shape the roadmap:

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## Links

- 🌐 [Official Website](https://usbridge.io)
- 🛒 [USBridge KVM 2.0 on Crowd Supply](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)
