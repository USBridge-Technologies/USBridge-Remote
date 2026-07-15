# USBridge Remote (Beta)

![USBridge Remote](assets/banner.png)

<div align="center">

[![Beta](https://img.shields.io/badge/status-beta-orange)](https://github.com/USBridge-Technologies/USBridge-Remote/releases)
[![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)](#download)
[![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)](#download)
[![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)](#download)
[![Android](https://img.shields.io/badge/Android-3DDC84?logo=android&logoColor=white)](#download)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.com/invite/xqQ6ybkfWS)

</div>

---

**USBridge Remote** is a unified high-performance client for managing remote machines. I designed it to combine **hardware-level BIOS access** (via USBridge KVM devices) and **software-based remote desktop** in a single, streamlined interface.

> ⚠️ **Beta Software** — This is an early release. Expect bugs. Please report issues via [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues) or join our [Discord](https://discord.com/invite/xqQ6ybkfWS) for support.

---

## Download

### Client
The Client is the control interface — installed on your workstation or laptop. It manages connections, live remote desktop, virtual device passthrough, and snapshot registry.

| Architecture | Windows | macOS | Linux | Android | iOS |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.zip) | — | — |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64.apk) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |

## Agent

The Agent runs on the target machine — the server or PC you want to access remotely. It handles screen capture, input injection, and Tailscale networking.

| Architecture | Windows | macOS | Linux |
| :--- | :---: | :---: | :---: |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg) | — |

---

## Demo

<div align="center">
  <a href="https://youtu.be/1pV9PJeBr7M">
    <img src="https://img.youtube.com/vi/1pV9PJeBr7M/maxresdefault.jpg" alt="USBridge Remote Demo" style="max-width: 100%; border-radius: 8px;">
  </a>
</div>

---

## Features

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**One place for everything** — I've unified the workflow. Manage USBridge KVM hardware and software agents from a single dashboard. Add a machine, connect, and you're in.

**No limits, no subscriptions** — Completely free. No session time limits, no connection caps, and no account required on the target machine.

**Low-latency video & Moonlight Integration** — Enjoy up to 2K resolution with buttery-smooth 240 FPS and zero perceptible lag. My adaptive streaming engine leverages native Moonlight integration to deliver unmatched, ultra-low-latency remote desktop performance.

**Tailscale integration** — Built-in encrypted P2P tunneling. Connect to any machine globally without messing with port forwarding or firewall rules. It works on LAN and over the internet automatically.

<img width="2080" height="1170" alt="Screenshot 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />


---

## Wayland Support (No Prompts)

Most remote desktop agents on Linux struggle with Wayland or constantly spam you with permission prompts and confirmation popups every time a session starts. 

I designed the USBridge Agent to support Wayland natively. It handles full screen capture and input injection out-of-the-box **without any annoying permission prompts** or manual confirmations. It just works.

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
