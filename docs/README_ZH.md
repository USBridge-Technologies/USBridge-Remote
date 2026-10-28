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

**USBridge Remote** 是一个统一的高性能客户端，用于管理远程机器。旨在将 **硬件级 BIOS 访问**（通过 USBridge KVM 设备）和 **基于软件的远程桌面** 结合在一个流畅的界面中。

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote">
</div>


## 下载

### 客户端
客户端是控制界面 — 安装在您的工作站或笔记本电脑上（或直接在浏览器中运行）。它管理连接、实时远程桌面、虚拟设备直通和快照注册。

| 架构 | Windows | macOS | Linux | Android | iOS | Web 浏览器 |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [打开应用](https://web.usbridge.io) |
| **ARM64** | — | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [打开应用](https://web.usbridge.io) |

想要一个不需要 Play 商店账户的直接 APK？自我更新的构建也发布在 [最新版本](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest)。

🌐 **零安装 Web 客户端**：无需安装。只需打开 [web.usbridge.io](https://web.usbridge.io) 即可立即连接。*(注意：由于浏览器安全沙箱和 WebRTC 限制，Web 客户端在某些功能和性能上存在限制。要获得完整的无妥协体验，请使用本地应用程序。)* Web 客户端通过其受信任的 `<label>.device.usbridge.io` 名称访问代理：代理在浏览器首次访问时获取该证书，并且仅在您登录到代理中的 USBridge 账户时有效 — 因此第一次尝试可能需要几分钟。有关详细信息，请参见代理的 **状态 → 证书** 行或 [代理文档](agent/docs/README.md#platform-notes-from-the-top-level-readme)。USBridge KVM 还在 `https://<kvm>:9443/` 上为 Web 客户端提供服务，并在可以时切换到其自己的受信任名称。

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

## 功能

**硬件加速编码：** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · 以及用于没有 GPU 编码器的虚拟机和机器的软件 **H.264 (OpenH264)**

<table>
  <tr>
    <td width="33%" valign="top">
      <code>基本功能</code><br><br>
      <b>SUNSHINE 开源</b><br>
      <i>标准游戏流媒体</i><br><br>
      ✓ 内置开源 Sunshine 流媒体<br>
      ✓ 低延迟桌面和游戏访问<br>
      ✓ 双向文件和文本剪贴板<br>    
      ✓ 多显示器显示切换<br>
      ✓ 集成 Tailscale P2P 网络<br>
      ✓ 客户访问：通过电子邮件邀请人，指定角色和结束日期<br>
      ✓ 原生 Wayland 支持（无提示捕获）
    </td>
    <td width="33%" valign="top">
      <code>+ 基本功能</code><br><br>
      <b>USBRIDGE 免费版</b><br>
      <i>基于 Rust 的流媒体引擎</i><br><br>
      ✓ 即时连接自定义远程协议<br>
      ✓ 原始 USB 外设直通<br>
      ✓ 针对 Wi-Fi 稳定性的自适应流媒体<br>
      ✓ HDR10<br>
      ✓ 无损 4:4:4 色度准确度<br>
      ✓ 麦克风直通（客户端 → 主机）<br>
      ✓ MIDI 设备直通（客户端 → 主机）<br>
      ✓ GPU 编码：NVENC、AMF、QSV、VAAPI<br>
      ✓ 无 GPU 的虚拟机使用软件 H.264 (OpenH264)<br>
      ✓ PyroWave 低延迟小波编解码器（由 Valve 提供）<br>
      ✓ 无头虚拟显示管理<br>
      ✓ 低延迟游戏手柄控制器直通<br>
      ✓ Web 浏览器客户端访问（无需安装）<br>
      ✓ Windows 登录前访问（安全凭据输入）
    </td>
    <td width="33%" valign="top">
      <code>+ 基本功能</code> <code>+ 免费功能</code><br><br>
      <b>USBRIDGE 专业版</b><br>
      <i>专业工作流程</i><br><br>
      ✓ 支持 Wacom 平板，具有压力和倾斜<br><br>
    </td>
  </tr>
</table>

## 快速开始

1. **在您想要远程访问的机器上安装代理**。启动它 — 它将显示连接令牌和 Tailscale 地址。如果您需要通过互联网访问，请连接 Tailscale。

2. **在您的工作站、笔记本电脑或手机上安装客户端**。

3. **添加连接** — 输入代理窗口中显示的 IP 或 Tailscale 地址。就这样。

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## 硬件集成

<table>
  <tr>
   <td width="33%" valign="top">
      <b>硬件级 BIOS 控制</b><br>
      USBridge Remote 与 USBridge-KVM 2.0 设备原生集成，用于操作系统启动前的带外、裸金属管理。<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVM 固件</b><br>
      在兼容的 SBC（例如，Radxa Zero 3W/3E、Cubie A7）上部署官方固件，配备 USB 捕获接口，以配置自定义 KVM 节点。<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IP 硬件加密狗 (ESP32-S3)</b><br>
      软件 USB 直通在 macOS 上遇到瓶颈：没有 VHCI 驱动程序可供连接，因此远程 Wacom 平板（或其他 USB 设备）无法作为真实硬件呈现。此加密狗插入 Mac，并在硬件上执行 <code>vhci-hcd</code> 在其他地方的软件功能 — 导出的设备以其自己的 VID/PID 枚举，由 macOS 自己的驱动程序绑定。<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm firmware & docs"></a>
    </td>
  </tr>
</table>

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
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord（测试版和错误报告）</a></li>
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