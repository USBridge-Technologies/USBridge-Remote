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

**USBridge Remote** 是一个低延迟的 **远程桌面** 和 **KVM-over-IP** 生态系统：一个适用于 Windows、macOS、Linux、Android、iOS 和浏览器的客户端，一个内置 **Sunshine / Moonlight** 游戏流媒体的主机代理，以及适用于 **Radxa Zero 3W**、**Radxa Cubie A7Z** 和 **Sipeed NanoKVM** 板的 IP-KVM 固件。它将 **硬件级 BIOS 访问** 和 **软件远程桌面** 集成在一个界面中，支持游戏手柄、Wacom 平板、MIDI 和麦克风的 USB 直通，以及无需端口转发的 Tailscale P2P 网络。

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote features: remote desktop, game streaming, USB passthrough, hardware KVM">
</div>

## 下载

### 客户端
客户端是控制界面 — 安装在您的工作站或笔记本电脑上（或直接在浏览器中运行）。它管理连接、实时远程桌面、虚拟设备直通和快照注册。

| 架构 | Windows | macOS | Linux | Android | iOS | Web 浏览器 |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [打开应用](https://web.usbridge.io) |
| **ARM64** | — | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [打开应用](https://web.usbridge.io) |

想要直接下载 APK 而不使用 Play 商店账户？自我更新的构建也发布在 [最新版本](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest)。

🌐 **零安装 Web 客户端**：无需安装。只需打开 [web.usbridge.io](https://web.usbridge.io) 即可立即连接。*(注意：由于浏览器安全沙箱和 WebRTC 限制，Web 客户端在某些功能和性能上存在限制。要获得完整的无妥协体验，请使用本地应用程序。)* Web 客户端通过其受信任的 `<label>.device.usbridge.io` 名称访问代理：代理在浏览器首次访问时获取该证书，并且仅在您登录到代理中的 USBridge 账户时有效 — 因此第一次尝试可能需要几分钟。请查看代理的 **状态 → 证书** 行，或查看 [代理文档](agent/docs/README.md#platform-notes-from-the-top-level-readme) 以获取详细信息。USBridge KVM 还在 `https://<kvm>:9443/` 上为 Web 客户端提供服务，并在可以时切换到其自己的受信任名称。

## 代理

代理在目标机器上运行 — 您想要远程访问的服务器或 PC。它处理屏幕捕获、输入注入和 Tailscale 网络。

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>架构</th>
          <th>Windows</th>
          <th>macOS</th>
          <th>Linux</th>
        </tr>
        <tr>
          <td><b>x86_64</b></td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">下载</a></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">下载</a></td>
        </tr>
        <tr>
          <td><b>ARM64</b></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">下载</a></td>
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

## 特性

**硬件加速编码：** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · 以及用于没有 GPU 编码器的虚拟机和计算机的软件 **H.264 (OpenH264)**

<table>
  <tr>
    <td width="33%" valign="top">
      <code>基本功能</code><br><br>
      <b>SUNSHINE 开源</b><br>
      <i>标准游戏流媒体</i><br><br>
      ✓ 内置开源 Sunshine 流媒体<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">替代流媒体</a>：Vibepollo（内置 Apollo 的 Sunshine 分支，Windows）和 punktfunk（兼容 Moonlight，Windows 和 Linux），一键切换<br>
      ✓ 内置基准测试，比较 Sunshine、RustShine、Vibepollo 和 punktfunk 在您自己的硬件和网络上的表现<br>
      ✓ 低延迟桌面和游戏访问<br>
      ✓ 双向文件和文本剪贴板<br>    
      ✓ 多显示器切换<br>
      ✓ 集成 Tailscale P2P 网络<br>
      ✓ 客户访问：通过电子邮件邀请人，设置角色和结束日期<br>
      ✓ 原生 Wayland 支持（无提示捕获）
    </td>
    <td width="33%" valign="top">
      <code>+ 基本特性</code><br><br>
      <b>USBRIDGE 免费版</b><br>
      <i>基于 Rust 的流媒体引擎</i><br><br>
      ✓ 即时连接自定义远程协议<br>
      ✓ 原始 <a href="https://www.usbridge.io/usb-passthrough/">USB 外设直通</a><br>
      ✓ 针对 Wi-Fi 稳定性优化的自适应流媒体<br>
      ✓ HDR10<br>
      ✓ 无损 4:4:4 色度准确度<br>
      ✓ 麦克风直通（客户端 → 主机）<br>
      ✓ MIDI 设备直通（客户端 → 主机）<br>
      ✓ GPU 编码：NVENC、AMF、QSV、VAAPI<br>
      ✓ 无 GPU 的虚拟机使用软件 H.264 (OpenH264)<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> 低延迟小波编码（由 Valve 提供）<br>
      ✓ 无头虚拟显示管理<br>
      ✓ 低延迟游戏手柄控制器直通<br>
      ✓ Web 浏览器客户端访问（无需安装）<br>
      ✓ Windows 登录前访问（安全凭据输入）
    </td>
    <td width="33%" valign="top">
      <code>+ 基本特性</code> <code>+ 免费特性</code><br><br>
      <b>USBRIDGE 专业版</b><br>
      <i>专业工作流程</i><br><br>
      ✓ 支持带压力和倾斜的 Wacom 平板（<a href="https://www.usbridge.io/macos-wacom-support/">macOS 也支持</a>）<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">硬件 KVM 固件</a> 适用于 Radxa Zero 3W、Radxa Cubie A7Z 和 Sipeed NanoKVM：BIOS/UEFI 访问、ISO 虚拟媒体、Btrfs 快照（每个设备 $19 终身，24 小时免费评估）<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">硬件 Moonlight 流媒体</a> 在这些板上：任何 HDMI 源（PC、PS5、Xbox、Switch）以 1080p 60 FPS 流式传输到 Moonlight，无需在源上安装软件<br><br>
    </td>
  </tr>
</table>

## 快速开始

1. **在您想要远程访问的机器上安装代理。** 启动它 — 它将显示连接令牌和 Tailscale 地址。如果您需要通过互联网访问，请连接 Tailscale。

2. **在您的工作站、笔记本电脑或手机上安装客户端。**

3. **添加连接** — 输入代理窗口中显示的 IP 或 Tailscale 地址。就这样。

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote quick start: install the agent, install the client, connect">
</div>

## 硬件集成

<table>
  <tr>
   <td width="33%" valign="top">
      <b>硬件级 BIOS 控制</b><br>
      USBridge Remote 与 USBridge-KVM 2.0 设备原生集成，用于在操作系统启动之前的带外、裸机管理。它也是第一个硬件 Moonlight 流媒体：标准 Moonlight 客户端与 KVM 配对，并流式传输任何 HDMI 源（PC 或游戏机），无需在其上安装软件。<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVM 固件</b><br>
      在兼容板（Radxa Zero 3W、Radxa Cubie A7Z）上闪存官方固件，并使用 HDMI 转 USB 捕获适配器构建自己的 IP-KVM。在 <b>Sipeed NanoKVM</b> 上，它替换了标准固件：流畅的 60 FPS 而不是卡顿的画面，音频和麦克风，游戏手柄，USB 直通和 Moonlight 支持（<a href="https://www.usbridge.io/hardware-agent/">下载固件</a>）。<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IP 硬件适配器 (ESP32-S3)</b><br>
      软件 USB 直通在 macOS 上遇到瓶颈：没有 VHCI 驱动程序可供连接，因此远程 Wacom 平板（或其他 USB 设备）无法作为真实硬件呈现。此适配器插入 Mac，并在硬件中执行 <code>vhci-hcd</code> 在其他地方的软件功能 — 导出的设备以其自己的 VID/PID 枚举，由 macOS 自己的驱动程序绑定。<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm firmware & docs"></a>
    </td>
  </tr>
</table>

### NanoKVM 与 USBridge 固件：60 FPS 而不是延迟

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Video: Sipeed NanoKVM flashed with USBridge firmware streams at 60 FPS with almost no input lag"></a>
</div>

一段 2 分钟的视频：一台在家庭实验室中延迟严重的 Sipeed NanoKVM 在闪存 USBridge 固件后，成为一台几乎没有输入延迟的 60 FPS KVM over IP，足够快以便用游戏手柄玩游戏。适用于 NanoKVM、Radxa Zero 3W 和 Radxa Cubie A7Z 的固件在 [硬件代理页面](https://www.usbridge.io/hardware-agent/) 上。

## 常见问题

<details>
<summary><b>USBridge 是否与 Moonlight 和 Sunshine 兼容？</b></summary>

是的。代理内置开源 Sunshine 流媒体，因此标准 Moonlight 客户端可以连接到它。Vibepollo（内置 Apollo 的 Sunshine 分支）和 punktfunk 也内置，并且可以一键切换。
</details>

<details>
<summary><b>是否有硬件 Moonlight 流媒体？</b></summary>

是的。USBridge KVM 2.0，或闪存了 USBridge 固件的 Radxa Zero 3W、Radxa Cubie A7Z 或 Sipeed NanoKVM，是 Sunshine 兼容的 GameStream 主机，适用于其 HDMI 输入上的任何内容：PC、PS5、Xbox 或 Switch。它以 1080p 60 FPS 流式传输音频到标准 Moonlight 客户端，源上无需安装软件。请参见 [硬件 Moonlight 流媒体](https://www.usbridge.io/moonlight-hardware-streamer/)。
</details>

<details>
<summary><b>哪个流媒体在我的 PC 上最快？</b></summary>

这取决于 GPU、驱动程序和网络。内置基准测试在您自己的硬件和网络上运行 Sunshine、RustShine、Vibepollo 和 punktfunk 并进行比较。请参见 [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/)。
</details>

<details>
<summary><b>支持哪些 GPU 进行编码？</b></summary>

NVIDIA NVENC、AMD AMF、Intel QSV 和 Linux 上的 VAAPI。没有 GPU 编码器的虚拟机和 PC 使用软件 H.264 (OpenH264)。
</details>

<details>
<summary><b>什么是免费的，什么需要专业版？</b></summary>

4:4:4 色彩、HDR10、PyroWave、游戏手柄、麦克风、MIDI 和原始 USB 直通是免费的。带压力和倾斜的 Wacom 平板，以及硬件 KVM 固件（每个设备 $19 终身，24 小时免费评估），是专业版。
</details>

<details>
<summary><b>我可以将 Sipeed NanoKVM 或 Radxa 板变成 IP-KVM 吗？</b></summary>

可以。请从 [硬件代理页面](https://www.usbridge.io/hardware-agent/) 闪存 USBridge 固件。您将获得 BIOS/UEFI 访问、ISO 和 IMG 虚拟媒体、不变的 Btrfs 快照、60 FPS 视频、音频和 USB 直通。Radxa 板需要 HDMI 转 USB 捕获适配器。
</details>

<details>
<summary><b>我需要端口转发吗？</b></summary>

不需要。连接通过 Tailscale 点对点进行，自动进行 NAT 穿透。
</details>

<details>
<summary><b>我可以与其他人共享对计算机的访问吗？</b></summary>

可以。通过电子邮件邀请人，设置角色和结束日期；来宾通过 USBridge 加入，无需 Tailscale 账户。
</details>

## 资源与许可证

<table>
  <tr>
    <td width="33%" valign="top">
      <b>开发与路线图</b><br>
      我维护一个公共仪表板，以跟踪所有计划的功能、架构更新和发布日程。<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">查看实时路线图</a>
    </td>
    <td width="33%" valign="top">
      <b>项目链接</b><br>
      <ul>
        <li><a href="https://usbridge.io">官方网站</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord（测试版测试与错误报告）</a></li>
        <li><a href="https://www.reddit.com/r/USBridge/">Reddit</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">YouTube</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Patreon 支持</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>许可证 (GPLv3)</b><br>
      本项目根据 <b>GPLv3</b> 许可证发布。多平台客户端包含来自 <code>moonlight-common-c</code>（同样是 GPLv3）的代码库。<br><br>
      <a href="LICENSE">查看许可证文件</a>
    </td>
  </tr>
</table>