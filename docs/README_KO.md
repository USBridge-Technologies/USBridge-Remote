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

**USBridge Remote**는 원격 머신을 관리하기 위한 통합 고성능 클라이언트입니다. **하드웨어 수준의 BIOS 접근**(USBridge KVM 장치를 통해)과 **소프트웨어 기반 원격 데스크탑**을 단일화된 인터페이스로 결합하도록 설계되었습니다.

 🖥️ **운영 체제가 부팅되기 전에 하드웨어 수준의 BIOS 제어가 필요하신가요?**  
 USBridge Remote는 **USBridge-KVM 2.0**과 네이티브로 통합되어 오프 밴드, 메탈 수준의 관리를 제공합니다.

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)

> 
🔥 **자신만의 IP-KVM을 만드세요!**  
DIY 펌웨어를 공식 출시했습니다. 이제 호환 가능한 SBC(예: Radxa Zero 3W/3E 또는 Cubie A7)와 USB 캡처 카드를 사용하여 본격적인 USBridge KVM 2.0 장치로 변환할 수 있습니다.

[![Get the Firmware](https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge)](https://www.usbridge.io/kvm-software)

> ⚠️ **베타 소프트웨어** — 초기 릴리스입니다. 버그가 있을 수 있습니다. 문제를 보고하려면 [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues)를 통해 보고하거나 지원을 위해 [Discord](https://discord.com/invite/xqQ6ybkfWS)에 참여해 주세요.

---

## 다운로드

### 클라이언트
클라이언트는 제어 인터페이스로, 워크스테이션이나 노트북에 설치되거나 브라우저에서 직접 실행됩니다. 연결, 실시간 원격 데스크탑, 가상 장치 패스스루 및 스냅샷 레지스트리를 관리합니다.

| 아키텍처 | Windows | macOS | Linux | Android | iOS | 웹 브라우저 |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [다운로드](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [다운로드](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [앱 열기](https://web.usbridge.io) |
| **ARM64** | — | [다운로드](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [앱 열기](https://web.usbridge.io) |

Play 스토어 계정 없이 직접 APK를 선호하십니까? 자가 업데이트 빌드도 [최신 릴리스](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest)에 게시됩니다.

🌐 **제로 설치 웹 클라이언트**: 설치가 필요 없습니다. [web.usbridge.io](https://web.usbridge.io)를 열어 즉시 연결하세요. *(참고: 웹 클라이언트는 브라우저 보안 샌드박스 및 WebRTC 제약으로 인해 일부 기능 및 성능 제한이 있습니다. 완전한 경험을 원하시면 네이티브 앱을 사용하세요).*

## 에이전트

에이전트는 원격으로 접근하려는 대상 머신 — 서버 또는 PC에서 실행됩니다. 화면 캡처, 입력 주입 및 Tailscale 네트워킹을 처리합니다.

| 아키텍처 | Windows | macOS | Linux |
| :--- | :---: | :---: | :---: |
| **x86_64** | [다운로드](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip) | — | [다운로드](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage) |
| **ARM64** | — | [다운로드](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg) | — |

---

## 데모

<div align="center">
  <a href="https://youtu.be/1pV9PJeBr7M">
    <img src="https://img.youtube.com/vi/1pV9PJeBr7M/maxresdefault.jpg" alt="USBridge Remote Demo" style="max-width: 100%; border-radius: 8px;">
  </a>
</div>

---

## 기능

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**모든 것을 위한 하나의 장소** — 워크플로우를 통합했습니다. 단일 대시보드에서 USBridge KVM 하드웨어 및 소프트웨어 에이전트를 관리하세요. 머신을 추가하고 연결하면 됩니다.

**제한 없음, 구독 없음** — 완전히 무료입니다. 세션 시간 제한, 연결 제한이 없으며 대상 머신에서 계정이 필요하지 않습니다.

**저지연 비디오 및 Moonlight 통합** — 부드러운 120 FPS로 최대 2K 해상도를 즐기세요. 제 적응형 스트리밍 엔진은 네이티브 Moonlight 통합을 활용하여 비교할 수 없는 초저지연 원격 데스크탑 성능을 제공합니다.

**Tailscale 통합** — 내장된 암호화된 P2P 터널링. 포트 포워딩이나 방화벽 규칙을 건드리지 않고 전 세계의 어떤 머신에도 연결할 수 있습니다. LAN 및 인터넷에서 자동으로 작동합니다.

**공유 클립보드** — 로컬 머신과 원격 대상 간에 원활하게 복사 및 붙여넣기. 텍스트, 이미지 및 파일 전송을 기본적으로 완벽하게 지원합니다.

**다중 모니터 지원** — 여러 디스플레이 간에 전환할 수 있는 기능을 추가했습니다. 대상 머신에 여러 모니터가 있는 경우 연결 설정에서 직접 어떤 모니터를 볼지 쉽게 선택할 수 있습니다.

<img width="2080" height="1170" alt="Screenshot 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />

---

## Wayland 지원 (프롬프트 없음)

대부분의 리눅스 원격 데스크탑 에이전트는 Wayland에서 어려움을 겪거나 세션이 시작될 때마다 권한 프롬프트 및 확인 팝업으로 스팸을 보냅니다.

USBridge 에이전트는 Wayland를 네이티브로 지원하도록 설계되었습니다. **귀찮은 권한 프롬프트**나 수동 확인 없이 전체 화면 캡처 및 입력 주입을 기본적으로 처리합니다. 그냥 작동합니다.

---

## 빠른 시작

1. **에이전트를 설치하세요** 원격으로 접근하려는 머신에 설치합니다. 실행하면 연결 토큰과 Tailscale 주소가 표시됩니다. 인터넷을 통해 접근해야 하는 경우 Tailscale에 연결하세요.

2. **클라이언트를 설치하세요** 워크스테이션, 노트북 또는 전화에 설치합니다.

3. **연결 추가** — 에이전트 창에 표시된 IP 또는 Tailscale 주소를 입력합니다. 그게 전부입니다.

---

## 프로젝트 로드맵

소프트웨어 개발 계획 및 향후 기능을 공개 대시보드에서 관리합니다. 현재 개발 중인 것, 계획된 것 또는 향후 기능의 상태를 확인하고 싶다면 실시간 로드맵을 확인하세요:

 **[USBridge Remote 로드맵 보기](https://github.com/orgs/USBridge-Technologies/projects/3)**

---

## 커뮤니티 및 베타 테스트

**베타 테스터** 역할을 얻고 버그를 보고하며 로드맵을 형성하는 데 도움을 주려면 Discord에 참여하세요:

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## 링크

- 🌐 [공식 웹사이트](https://usbridge.io)
- ❤️ [Patreon 페이지](https://www.patreon.com/USBridge_Technologies)
- 🛒 [Crowd Supply의 USBridge KVM 2.0](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)

---

## 📜 라이센스

이 프로젝트는 **GPLv3** 라이센스 하에 있습니다 (자세한 내용은 [`LICENSE`](LICENSE)를 참조하세요). Android/Windows/macOS/Linux 클라이언트는 `moonlight-common-c`의 코드를 포함하고 있습니다 (또한 GPLv3).