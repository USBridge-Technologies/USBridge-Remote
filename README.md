# USBridge Remote (Beta)

![USBridge Remote](https://raw.githubusercontent.com/USBridge-Technologies/USBridge-Remote/main/assets/banner.png)

<div align="center">

[English](README.md) | [Deutsch](docs/README_DE.md) | [Français](docs/README_FR.md) | [Italiano](docs/README_IT.md) | [Español](docs/README_ES.md) | [Português (Brasil)](docs/README_PT_BR.md) | [Українська](docs/README_UA.md) | [Polski](docs/README_PL.md) | [日本語](docs/README_JA.md) | [한국어](docs/README_KO.md) | [简体中文](docs/README_ZH.md)

[![Beta](https://img.shields.io/badge/status-beta-orange)](https://github.com/USBridge-Technologies/USBridge-Remote/releases)
[![Patreon](https://img.shields.io/badge/Patreon-Support_Us-F96854?logo=patreon&logoColor=white)](https://www.patreon.com/USBridge_Technologies)
[![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)](#)
[![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)](#)
[![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)](#)
[![Android](https://img.shields.io/badge/Android-3DDC84?logo=android&logoColor=white)](https://play.google.com/store/apps/details?id=io.usbridge.client)
[![iOS](https://img.shields.io/badge/iOS-000000?logo=apple&logoColor=white)](#)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.com/invite/xqQ6ybkfWS)
<a href="https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?logo=crowdsupply&logoColor=white" alt="Crowd Supply"></a>

</div>

---

**USBridge Remote** is a unified high-performance client for managing remote machines. I designed it to combine **hardware-level BIOS access** (via USBridge KVM devices) and **software-based remote desktop** in a single, streamlined interface.

 🖥️ **Need hardware-level BIOS control before the OS boots?**  
 USBridge Remote integrates natively with **USBridge-KVM 2.0** for out-of-band, metal-level management. 

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)


> ⚠️ **Beta Software** — This is an early release. Expect bugs. Please report issues via [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues) or join our [Discord](https://discord.com/invite/xqQ6ybkfWS) for support.
> 
> ℹ️ **Note regarding Windows Defender / Antivirus False Positives:**  
> Windows Defender may incorrectly flag `libva.dll` as a threat (`Trojan:Win32/Wacatac.B!ml`) due to heuristics/machine-learning detection on unsigned binaries. **This is a false positive.**  
> We have submitted the file to Microsoft Security Intelligence for official review and whitelisting. In the meantime, if your antivirus removes `libva.dll`, please restore it from Quarantine or add the USBridge folder to your antivirus exclusion list.

---

## Download

### Client
The Client is the control interface — installed on your workstation or laptop (or run directly in your browser). It manages connections, live remote desktop, virtual device passthrough, and snapshot registry.

| Architecture | Windows | macOS | Linux | Android | iOS | Web Browser |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Open App](https://web.usbridge.io) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Open App](https://web.usbridge.io) |

Prefer a direct APK without a Play Store account? A self-updating build is also published on the [latest release](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Zero-Install Web Client**: No installation required. Just open [web.usbridge.io](https://web.usbridge.io) to connect instantly. *(Note: The web client operates with some feature and performance limitations due to browser security sandbox and WebRTC constraints. For the full uncompromised experience, use the native apps).*

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

**Low-latency video & Moonlight Integration** — Enjoy up to 2K resolution with buttery-smooth 120 FPS and zero perceptible lag. My adaptive streaming engine leverages native Moonlight integration to deliver unmatched, ultra-low-latency remote desktop performance.

**Tailscale integration** — Built-in encrypted P2P tunneling. Connect to any machine globally without messing with port forwarding or firewall rules. It works on LAN and over the internet automatically.

**Shared Clipboard** — Copy and paste seamlessly between your local machine and remote targets. It fully supports text, images, and file transfers out-of-the-box.

**Multi-Monitor Support** — I've added the ability to switch between multiple displays. If the target machine has several monitors, you can now easily select which one to view directly from the connection settings. 

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

##  Project Roadmap

I manage software development plans and upcoming features in an open dashboard. If you want to see what is currently being developed, what is planned, or follow the status of upcoming features, check out the live roadmap:

 **[View USBridge Remote Roadmap](https://github.com/orgs/USBridge-Technologies/projects/3)**

---

## Community & Beta Testing

Join our Discord to get the **Beta Tester** role, report bugs, and help me shape the roadmap:

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## Links

- 🌐 [Official Website](https://usbridge.io)
- ❤️ [Patreon Page](https://www.patreon.com/USBridge_Technologies)
- 🛒 [USBridge KVM 2.0 on Crowd Supply](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)

---

## 📜 License

This project is licensed under **GPLv3** (see [`LICENSE`](LICENSE)). The Android/Windows/macOS/Linux client incorporates code from `moonlight-common-c` (also GPLv3).
