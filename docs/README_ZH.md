# USBridge Remote (Beta)

![USBridge Remote](https://raw.githubusercontent.com/USBridge-Technologies/USBridge-Remote/main/assets/banner.png)

<div align="center">

[English](README.md) | [Deutsch](docs/README_DE.md) | [Français](docs/README_FR.md) | [Italiano](docs/README_IT.md) | [Español](docs/README_ES.md) | [Português (Brasil)](docs/README_PT_BR.md) | [Українська](docs/README_UA.md) | [Polski](docs/README_PL.md) | [日本語](docs/README_JA.md) | [한국어](docs/README_KO.md) | [简体中文](docs/README_ZH.md)

[![Beta](https://img.shields.io/badge/status-beta-orange)](https://github.com/USBridge-Technologies/USBridge-Remote/releases)
[![Patreon](https://img.shields.io/badge/Patreon-Support_Us-F96854?logo=patreon&logoColor=white)](https://www.patreon.com/USBridge_Technologies)
[![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)](#)
[![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)](#)
[![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)](#)
[![Android](https://img.shields.io/badge/Android-3DDC84?logo=android&logoColor=white)](#)
[![iOS](https://img.shields.io/badge/iOS-000000?logo=apple&logoColor=white)](#)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.com/invite/xqQ6ybkfWS)
<a href="https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?logo=crowdsupply&logoColor=white" alt="Crowd Supply"></a>

</div>

---

**USBridge Remote** 是一个统一的高性能客户端，用于管理远程机器。我设计它以结合 **硬件级 BIOS 访问**（通过 USBridge KVM 设备）和 **基于软件的远程桌面**，在一个简化的界面中。

 🖥️ **需要在操作系统启动之前进行硬件级 BIOS 控制吗？**  
 USBridge Remote 与 **USBridge-KVM 2.0** 原生集成，提供带外的金属级管理。

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)


> ⚠️ **Beta 软件** — 这是一个早期版本。请预期会有错误。请通过 [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues) 报告问题，或加入我们的 [Discord](https://discord.com/invite/xqQ6ybkfWS) 获取支持。
> 
> ℹ️ **关于 Windows Defender / 杀毒软件误报的说明：**  
> Windows Defender 可能会错误地将 `libva.dll` 标记为威胁（`Trojan:Win32/Wacatac.B!ml`），这是由于对未签名二进制文件的启发式/机器学习检测。**这是一个误报。**  
> 我们已将该文件提交给 Microsoft Security Intelligence 进行官方审查和白名单。在此期间，如果您的杀毒软件删除了 `libva.dll`，请从隔离区恢复它或将 USBridge 文件夹添加到您的杀毒软件排除列表中。

---

## 下载

### 客户端
客户端是控制界面 — 安装在您的工作站或笔记本电脑上。它管理连接、实时远程桌面、虚拟设备直通和快照注册。

| 架构 | Windows | macOS | Linux | Android | iOS |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — |
| **ARM64** | — | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64.apk) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |

## 代理

代理在目标机器上运行 — 您想要远程访问的服务器或 PC。它处理屏幕捕获、输入注入和 Tailscale 网络。

| 架构 | Windows | macOS | Linux |
| :--- | :---: | :---: | :---: |
| **x86_64** | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip) | — | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage) |
| **ARM64** | — | [下载](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg) | — |

---

## 演示

<div align="center">
  <a href="https://youtu.be/1pV9PJeBr7M">
    <img src="https://img.youtube.com/vi/1pV9PJeBr7M/maxresdefault.jpg" alt="USBridge Remote 演示" style="max-width: 100%; border-radius: 8px;">
  </a>
</div>

---

## 媒体报道

| 媒体 | 亮点 | 链接 |
| :--- | :--- | :---: |
| **IlSoftware.it**（意大利） | *"USBridge Remote 挑战 RustDesk 和 AnyDesk..."* — 独立深入评测，赞扬软件代理与硬件 KVM 的协同作用、原生 Wayland 支持和 P2P 架构。 | [阅读文章](https://www.ilsoftware.it/alternativa-rustdesk-anydesk-usbridge/) |

---

## 特性

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**一处满足所有需求** — 我统一了工作流程。从单一仪表板管理 USBridge KVM 硬件和软件代理。添加一台机器，连接，您就可以使用了。

**没有限制，没有订阅** — 完全免费。没有会话时间限制，没有连接上限，也不需要在目标机器上创建账户。

**低延迟视频与 Moonlight 集成** — 享受高达 2K 分辨率的流畅 240 FPS 视频，几乎没有可感知的延迟。我的自适应流媒体引擎利用原生 Moonlight 集成，提供无与伦比的超低延迟远程桌面性能。

**Tailscale 集成** — 内置加密 P2P 隧道。无需处理端口转发或防火墙规则，即可全球连接任何机器。它在局域网和互联网自动工作。

**共享剪贴板** — 在本地机器和远程目标之间无缝复制和粘贴。它完全支持文本、图像和文件传输，开箱即用。

<img width="2080" height="1170" alt="Screenshot 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />


---

## Wayland 支持（无提示）

大多数 Linux 上的远程桌面代理在 Wayland 上表现不佳，或者每次会话开始时都会不断弹出权限提示和确认窗口。

我设计的 USBridge 代理原生支持 Wayland。它开箱即用地处理全屏捕获和输入注入，**没有任何烦人的权限提示**或手动确认。它就是这么简单。

---

## 快速开始

1. **在您想要远程访问的机器上安装代理**。启动它 — 它将显示连接令牌和 Tailscale 地址。如果您需要通过互联网访问，请连接 Tailscale。

2. **在您的工作站、笔记本电脑或手机上安装客户端**。

3. **添加连接** — 输入代理窗口中显示的 IP 或 Tailscale 地址。就这样。

---

## 项目路线图

我在一个开放的仪表板上管理软件开发计划和即将推出的功能。如果您想查看当前正在开发的内容、计划的内容或跟踪即将推出的功能的状态，请查看实时路线图：

 **[查看 USBridge Remote 路线图](https://github.com/orgs/USBridge-Technologies/projects/3)**

---

## 社区与 Beta 测试

加入我们的 Discord 获取 **Beta 测试者** 角色，报告错误，并帮助我制定路线图：

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## 链接

- 🌐 [官方网站](https://usbridge.io)
- ❤️ [Patreon 页面](https://www.patreon.com/USBridge_Technologies)
- 🛒 [Crowd Supply 上的 USBridge KVM 2.0](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)