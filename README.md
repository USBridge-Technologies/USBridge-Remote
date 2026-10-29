# USBridge Remote

<div align="center">
  <img src="./assets/banner6.svg" width="1400" alt="USBridge Remote: low-latency remote desktop and KVM-over-IP">
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

**USBridge Remote** is a low-latency **remote desktop** and **KVM-over-IP** ecosystem: one client for Windows, macOS, Linux, Android, iOS and the browser, a host agent with a built-in **Sunshine / Moonlight** game streamer, and IP-KVM firmware for **Radxa Zero 3W**, **Radxa Cubie A7Z** and **Sipeed NanoKVM** boards. It combines **hardware-level BIOS access** and **software remote desktop** in a single interface, with USB passthrough for gamepads, Wacom tablets, MIDI and microphones, and Tailscale P2P networking with no port forwarding.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote features: remote desktop, game streaming, USB passthrough, hardware KVM">
</div>


## Download

**All-in-one installer (Client + Agent, pick either or both during setup):** [Windows Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) · [macOS disk image (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-macOS-arm64.dmg)

Linux packages (.deb, .rpm, Arch) add the USBridge package repository, so the apps update with the rest of the system. All files are on the [latest release](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

### Client
The Client is the control interface — installed on your workstation or laptop (or run directly in your browser). It manages connections, live remote desktop, virtual device passthrough, and snapshot registry.

| Platform | Installer | Portable | Packages / Store |
| :--- | :--- | :--- | :--- |
| **Windows** x86_64 | [Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) | [Portable (.zip)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — |
| **macOS** Apple Silicon | [Disk image (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | — |
| **Linux** x86_64 | [Debian / Ubuntu (.deb)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.deb) | [AppImage](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | [Fedora / RHEL (.rpm)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.rpm) · [Arch (.pkg.tar.zst)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.pkg.tar.zst) |
| **Android** ARM64 | [APK (self-updating)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) |
| **iOS / iPadOS** | — | — | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |
| **Web browser** | — | [Open web.usbridge.io](https://web.usbridge.io) | — |

🌐 **Zero-Install Web Client**: No installation required. Just open [web.usbridge.io](https://web.usbridge.io) to connect instantly. *(Note: The web client operates with some feature and performance limitations due to browser security sandbox and WebRTC constraints. For the full uncompromised experience, use the native apps).* The web client reaches an Agent through its trusted `<label>.device.usbridge.io` name: the Agent gets that certificate the first time a browser comes to it, and only while you're signed in to your USBridge account in the Agent — so the very first attempt can take a few minutes. See the Agent's **Status → Certificate** row, or the [Agent docs](agent/docs/README.md#platform-notes-from-the-top-level-readme) for details. A USBridge KVM also serves the web client itself at `https://<kvm>:9443/` and moves to its own trusted name when it can.

## Agent

The Agent runs on the target machine — the server or PC you want to access remotely. It captures the screen and sound, passes your keyboard, mouse and gamepad through to that machine, and connects it over Tailscale.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Platform</th>
          <th>Downloads</th>
        </tr>
        <tr>
          <td><b>Windows</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64-Setup.exe">Setup (.exe)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Portable (.zip)</a></td>
        </tr>
        <tr>
          <td><b>macOS</b><br>Apple Silicon</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Disk image (.dmg)</a></td>
        </tr>
        <tr>
          <td><b>Linux</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.deb">Debian / Ubuntu (.deb)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.rpm">Fedora / RHEL (.rpm)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.pkg.tar.zst">Arch (.pkg.tar.zst)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">AppImage (portable)</a></td>
        </tr>
      </table>
    </td>
    <!-- Right Column: Image -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="USBridge Agent window: connection token, Tailscale address and stream status" width="100%">
    </td>
  </tr>
</table>

## Features

**Hardware-accelerated encoding:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · plus software **H.264 (OpenH264)** for VMs and machines without a GPU encoder

<table>
  <tr>
    <td width="33%" valign="top">
      <code>BASIC FUNCTIONALITY</code><br><br>
      <b>SUNSHINE OPEN-SOURCE</b><br>
      <i>Standard Game Streaming</i><br><br>
      ✓ Built-in open-source Sunshine streamer<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">Alternative streamers</a>: Vibepollo (Sunshine fork with Apollo built in, Windows) and punktfunk (Moonlight-compatible, Windows & Linux), switch in one click<br>
      ✓ Built-in benchmark to compare Sunshine, RustShine, Vibepollo and punktfunk on your own hardware and network<br>
      ✓ Low-latency desktop & gaming access<br>
      ✓ Bi-directional file & text clipboard<br>    
      ✓ Multi-monitor display switching<br>
      ✓ Integrated Tailscale P2P networking<br>
      ✓ Guest access: invite people by e-mail with a role and an end date<br>
      ✓ Native Wayland support (promptless capture)
    </td>
    <td width="33%" valign="top">
      <code>+ BASIC FEATURES</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Rust based Stream Engine</i><br><br>
      ✓ Instant-connect custom remote protocol<br>
      ✓ Raw <a href="https://www.usbridge.io/usb-passthrough/">USB peripheral device passthrough</a><br>
      ✓ Adaptive streaming optimized for Wi-Fi stability<br>
      ✓ HDR10<br>
      ✓ Lossless 4:4:4 chroma color accuracy<br>
      ✓ Microphone passthrough (client → host)<br>
      ✓ MIDI device passthrough (client → host)<br>
      ✓ GPU encoding: NVENC, AMF, QSV, VAAPI<br>
      ✓ Software H.264 (OpenH264) for VMs without a GPU<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> low-latency wavelet codec (by Valve)<br>
      ✓ Headless virtual display management<br>
      ✓ Low-latency gamepad controller passthrough<br>
      ✓ Web browser client access (no installation required)<br>
      ✓ Windows pre-login access (secure credential entry)
    </td>
    <td width="33%" valign="top">
      <code>+ BASIC FEATURES</code> <code>+ FREE FEATURES</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Professional Workflows</i><br><br>
      ✓ Wacom tablet support with pressure & tilt (<a href="https://www.usbridge.io/macos-wacom-support/">macOS too</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">Hardware KVM firmware</a> for Radxa Zero 3W, Radxa Cubie A7Z and Sipeed NanoKVM: BIOS/UEFI access, ISO virtual media, Btrfs snapshots ($19 lifetime per device, 24 h free evaluation)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">Hardware Moonlight streamer</a> on those boards: any HDMI source (PC, PS5, Xbox, Switch) to Moonlight at 1080p 60 FPS, no software on the source<br><br>
    </td>
  </tr>
</table>

## Quick Start

1. **Install the Agent** on the machine you want to access remotely. Launch it — it will display a connection token and Tailscale address. Connect Tailscale if you need access over the internet.

2. **Install the Client** on your workstation, laptop, or phone.

3. **Add a connection** — enter the IP or Tailscale address shown in the Agent window. That's it.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote quick start: install the agent, install the client, connect">
</div>

## Hardware Integration

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Hardware-Level BIOS Control</b><br>
      USBridge Remote integrates natively with the USBridge-KVM 2.0 appliance for out-of-band, bare-metal management before OS boot. It is also the first hardware Moonlight streamer: the stock Moonlight client pairs with the KVM and streams any HDMI source (a PC or a game console) with no software on it.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVM Firmware</b><br>
      Flash the official firmware on a compatible board (Radxa Zero 3W, Radxa Cubie A7Z) with an HDMI-to-USB capture dongle to build your own IP-KVM. On a <b>Sipeed NanoKVM</b> it replaces the stock firmware: smooth 60 FPS instead of a stuttering picture, audio and microphone, gamepads, USB passthrough and Moonlight support (<a href="https://www.usbridge.io/hardware-agent/">download the firmware</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IP Hardware Dongle (ESP32-S3)</b><br>
      Software USB passthrough hits a wall on macOS: there is no VHCI driver to attach to, so a remote Wacom tablet (or other USB device) can't be presented as real hardware. This dongle plugs into the Mac and does in hardware what <code>vhci-hcd</code> does in software elsewhere — the exported device enumerates with its own VID/PID, bound by macOS's own driver.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm firmware & docs"></a>
    </td>
  </tr>
</table>

### NanoKVM with USBridge firmware: 60 FPS instead of lag

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Video: Sipeed NanoKVM flashed with USBridge firmware streams at 60 FPS with almost no input lag"></a>
  <br><br>
  <img src="https://img.shields.io/badge/1080p-60_FPS-2da44e?style=flat-square" alt="1080p 60 FPS">
  <img src="https://img.shields.io/badge/Tailscale-P2P-242424?style=flat-square&logo=tailscale&logoColor=white" alt="Tailscale P2P">
  <img src="https://img.shields.io/badge/Audio-sound_%2B_mic-007ec6?style=flat-square" alt="Audio: sound and microphone">
  <img src="https://img.shields.io/badge/FEC-packet_loss_recovery-f59e0b?style=flat-square" alt="FEC: forward error correction">
</div>

A 2-minute video: a Sipeed NanoKVM with heavy lag in a home lab becomes a 60 FPS KVM over IP with almost no input lag after flashing USBridge firmware, fast enough to play games with a gamepad. Tailscale works on the board itself, so it is reachable from anywhere with no port forwarding; sound plays both ways, and FEC (forward error correction) rebuilds lost packets, so the picture stays clean on Wi-Fi and over the internet. Firmware for the NanoKVM, Radxa Zero 3W and Radxa Cubie A7Z is on the [Hardware Agent page](https://www.usbridge.io/hardware-agent/).

## FAQ

<details>
<summary><b>Does USBridge work with Moonlight and Sunshine?</b></summary>

Yes. The Agent ships the open-source Sunshine streamer, so the stock Moonlight client connects to it. Vibepollo (a Sunshine fork with Apollo built in) and punktfunk are built in too and switch in one click.
</details>

<details>
<summary><b>Is there a hardware Moonlight streamer?</b></summary>

Yes. A USBridge KVM 2.0, or a Radxa Zero 3W, Radxa Cubie A7Z or Sipeed NanoKVM flashed with USBridge firmware, is a Sunshine-compatible GameStream host for whatever is on its HDMI input: a PC, PS5, Xbox or Switch. It streams 1080p 60 FPS with audio to the stock Moonlight client, with no software on the source. See [Hardware Moonlight Streamer](https://www.usbridge.io/moonlight-hardware-streamer/).
</details>

<details>
<summary><b>Which streamer is fastest on my PC?</b></summary>

It depends on the GPU, the driver and the network. The built-in benchmark runs Sunshine, RustShine, Vibepollo and punktfunk on your own hardware and network and compares them. See [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/).
</details>

<details>
<summary><b>Which GPUs are supported for encoding?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV and VAAPI on Linux. Virtual machines and PCs without a GPU encoder use software H.264 (OpenH264).
</details>

<details>
<summary><b>What is free and what needs Pro?</b></summary>

4:4:4 color, HDR10, PyroWave, gamepads, microphone, MIDI and raw USB passthrough are free. Wacom tablets with pressure and tilt, and the hardware KVM firmware ($19 lifetime per device, 24 h free evaluation), are Pro.
</details>

<details>
<summary><b>Can I turn a Sipeed NanoKVM or a Radxa board into an IP-KVM?</b></summary>

Yes. Flash the USBridge firmware from the [Hardware Agent page](https://www.usbridge.io/hardware-agent/). You get BIOS/UEFI access, ISO and IMG virtual media, immutable Btrfs snapshots, 60 FPS video, audio and USB passthrough. Radxa boards need an HDMI-to-USB capture dongle.
</details>

<details>
<summary><b>Do I need port forwarding?</b></summary>

No. Connections go over Tailscale peer-to-peer, with automatic NAT traversal.
</details>

<details>
<summary><b>Can I share access to a computer with someone else?</b></summary>

Yes. Invite people by e-mail with a role and an end date; guests join through USBridge and need no Tailscale account.
</details>

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
        <li><a href="https://www.reddit.com/r/USBridge/">Reddit</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">YouTube</a></li>
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


