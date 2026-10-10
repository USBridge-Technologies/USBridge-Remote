# USBridge Remote

<div align="center">
  <img src="./assets/banner6.svg" width="1400" alt="USBridge Remote">
</div>

<div align="center">

[English](README.md) | [Deutsch](docs/README_DE.md) | [Français](docs/README_FR.md) | [Italiano](docs/README_IT.md) | [Español](docs/README_ES.md) | [Português (Brasil)](docs/README_PT_BR.md) | [Українська](docs/README_UA.md) | [Polski](docs/README_PL.md) | [日本語](docs/README_JA.md) | [한국어](docs/README_KO.md) | [简体中文](docs/README_ZH.md)

[![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)](#)
[![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)](#)
[![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)](#)
[![Android](https://img.shields.io/badge/Android-3DDC84?logo=android&logoColor=white)](https://play.google.com/store/apps/details?id=io.usbridge.client)
[![iOS](https://img.shields.io/badge/iOS-000000?logo=apple&logoColor=white)](https://apps.apple.com/us/app/usbridge-remote-desktop/id6787665935)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.com/invite/xqQ6ybkfWS)

</div>

---

**USBridge Remote** is a unified high-performance client for managing remote machines. Engineered to combine **hardware-level BIOS access** (via USBridge KVM devices) and **software-based remote desktop** in a single, streamlined interface.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote">
</div>


## Download

### Client
The Client is the control interface — installed on your workstation or laptop (or run directly in your browser). It manages connections, live remote desktop, virtual device passthrough, and snapshot registry.

| Architecture | Windows | macOS | Linux | Android | iOS | Web Browser |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Open App](https://web.usbridge.io) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Open App](https://web.usbridge.io) |

Prefer a direct APK without a Play Store account? A self-updating build is also published on the [latest release](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Zero-Install Web Client**: No installation required. Just open [web.usbridge.io](https://web.usbridge.io) to connect instantly. *(Note: The web client operates with some feature and performance limitations due to browser security sandbox and WebRTC constraints. For the full uncompromised experience, use the native apps).* The web client reaches an Agent through its trusted `<label>.device.usbridge.io` name: the Agent gets that certificate the first time a browser comes to it, and only while you're signed in to your USBridge account in the Agent — so the very first attempt can take a few minutes. See the Agent's **Status → Certificate** row, or the [Agent docs](agent/docs/README.md#platform-notes-from-the-top-level-readme) for details. A USBridge KVM also serves the web client itself at `https://<kvm>:9443/` and moves to its own trusted name when it can.

## Agent

The Agent runs on the target machine — the server or PC you want to access remotely. It handles screen capture, input injection, and Tailscale networking.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Architecture</th>
          <th>Windows</th>
          <th>macOS</th>
          <th>Linux</th>
        </tr>
        <tr>
          <td><b>x86_64</b></td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Download</a></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">Download</a></td>
        </tr>
        <tr>
          <td><b>ARM64</b></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Download</a></td>
          <td>—</td>
        </tr>
      </table>
    </td>
    <!-- Right Column: Image -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="USBridge Agent Interface" width="100%">
    </td>
  </tr>
</table>

## Features

<table>
  <tr>
    <td width="33%" valign="top">
      <code>BASIC FUNCTIONALITY</code><br><br>
      <b>SUNSHINE OPEN-SOURCE</b><br>
      <i>Standard Game Streaming</i><br><br>
      ✓ Built-in open-source Sunshine streamer<br>
      ✓ Low-latency desktop & gaming access<br>
      ✓ Bi-directional file & text clipboard<br>    
      ✓ Multi-monitor display switching<br>
      ✓ Integrated Tailscale P2P networking<br>
      ✓ Native Wayland support (promptless capture)
    </td>
    <td width="33%" valign="top">
      <code>+ BASIC FEATURES</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Rust based Stream Engine</i><br><br>
      ✓ Instant-connect custom remote protocol<br>
      ✓ Raw USB peripheral device passthrough<br>
      ✓ Adaptive streaming optimized for Wi-Fi stability<br>
      ✓ HDR10<br>
      ✓ Headless virtual display management<br>
      ✓ Low-latency gamepad controller passthrough<br>
      ✓ Web browser client access (no installation required)<br>
      ✓ Windows pre-login access (secure credential entry)
    </td>
    <td width="33%" valign="top">
      <code>+ BASIC FEATURES</code> <code>+ FREE FEATURES</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Professional Workflows</i><br><br>
      ✓ Lossless 4:4:4 chroma color accuracy<br>
      ✓ Wacom tablet support with pressure & tilt<br><br>
    </td>
  </tr>
</table>

## Quick Start

1. **Install the Agent** on the machine you want to access remotely. Launch it — it will display a connection token and Tailscale address. Connect Tailscale if you need access over the internet.

2. **Install the Client** on your workstation, laptop, or phone.

3. **Add a connection** — enter the IP or Tailscale address shown in the Agent window. That's it.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## Hardware Integration

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Hardware-Level BIOS Control</b><br>
      USBridge Remote integrates natively with the USBridge-KVM 2.0 appliance for out-of-band, bare-metal management before OS boot.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVM Firmware</b><br>
      Deploy the official firmware on a compatible SBC (e.g., Radxa Zero 3W/3E, Cubie A7) with a USB capture interface to provision a custom KVM node.<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IP Hardware Dongle (ESP32-S3)</b><br>
      Software USB passthrough hits a wall on macOS: there is no VHCI driver to attach to, so a remote Wacom tablet (or other USB device) can't be presented as real hardware. This dongle plugs into the Mac and does in hardware what <code>vhci-hcd</code> does in software elsewhere — the exported device enumerates with its own VID/PID, bound by macOS's own driver.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm firmware & docs"></a>
    </td>
  </tr>
</table>

## Resources & License

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Development & Roadmap</b><br>
      I maintain a public dashboard to track all planned features, architectural updates, and release schedules.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">View Live Roadmap</a>
    </td>
    <td width="33%" valign="top">
      <b>Project Links</b><br>
      <ul>
        <li><a href="https://usbridge.io">Official Website</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Beta Testing & Bugs)</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Patreon Support</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>License (GPLv3)</b><br>
      This project is licensed under the <b>GPLv3</b>. The multi-platform client incorporates codebase from <code>moonlight-common-c</code> (also GPLv3).<br><br>
      <a href="LICENSE">View LICENSE File</a>
    </td>
  </tr>
</table>


