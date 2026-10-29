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

**USBridge Remote**는 저지연 **원격 데스크톱** 및 **KVM-over-IP** 생태계입니다: Windows, macOS, Linux, Android, iOS 및 브라우저용 클라이언트 하나, 내장된 **Sunshine / Moonlight** 게임 스트리머가 있는 호스트 에이전트, 그리고 **Radxa Zero 3W**, **Radxa Cubie A7Z** 및 **Sipeed NanoKVM** 보드를 위한 IP-KVM 펌웨어가 포함되어 있습니다. 하드웨어 수준의 BIOS 접근과 소프트웨어 원격 데스크톱을 단일 인터페이스에서 결합하며, 게임패드, Wacom 태블릿, MIDI 및 마이크를 위한 USB 패스스루와 포트 포워딩 없이 Tailscale P2P 네트워킹을 제공합니다.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote features: remote desktop, game streaming, USB passthrough, hardware KVM">
</div>


## 다운로드

**올인원 설치 프로그램 (클라이언트 + 에이전트, 설정 중 하나 또는 둘 다 선택):** [Windows 설치 (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) · [macOS 디스크 이미지 (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-macOS-arm64.dmg)

Linux 패키지 (.deb, .rpm, Arch)는 USBridge 패키지 리포지토리를 추가하여 앱이 시스템의 나머지 부분과 함께 업데이트됩니다. 모든 파일은 [최신 릴리스](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest)에 있습니다.

### 클라이언트
클라이언트는 제어 인터페이스입니다 — 작업대 또는 노트북에 설치되거나 브라우저에서 직접 실행됩니다. 연결, 실시간 원격 데스크톱, 가상 장치 패스스루 및 스냅샷 레지스트리를 관리합니다.

| 플랫폼 | 설치 프로그램 | 포터블 | 패키지 / 스토어 |
| :--- | :--- | :--- | :--- |
| **Windows** x86_64 | [설치 (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) | [포터블 (.zip)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — |
| **macOS** Apple Silicon | [디스크 이미지 (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | — |
| **Linux** x86_64 | [Debian / Ubuntu (.deb)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.deb) | [AppImage](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | [Fedora / RHEL (.rpm)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.rpm) · [Arch (.pkg.tar.zst)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.pkg.tar.zst) |
| **Android** ARM64 | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [APK (자동 업데이트)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | — |
| **iOS / iPadOS** | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | — | — |
| **웹 브라우저** | — | [웹.usbridge.io 열기](https://web.usbridge.io) | — |

🌐 **제로 설치 웹 클라이언트**: 설치가 필요 없습니다. [web.usbridge.io](https://web.usbridge.io)를 열어 즉시 연결하세요. *(참고: 웹 클라이언트는 브라우저 보안 샌드박스 및 WebRTC 제약으로 인해 일부 기능 및 성능 제한이 있습니다. 완전한 경험을 원하시면 네이티브 앱을 사용하세요).* 웹 클라이언트는 신뢰할 수 있는 `<label>.device.usbridge.io` 이름을 통해 에이전트에 도달합니다: 에이전트는 브라우저가 처음으로 방문할 때 해당 인증서를 받고, USBridge 계정에 로그인하는 동안에만 유효합니다 — 따라서 첫 번째 시도는 몇 분이 걸릴 수 있습니다. 에이전트의 **상태 → 인증서** 행 또는 [에이전트 문서](agent/docs/README.md#platform-notes-from-the-top-level-readme)에서 자세한 내용을 확인하세요. USBridge KVM은 또한 `https://<kvm>:9443/`에서 웹 클라이언트를 자체적으로 제공합니다.

## 에이전트

에이전트는 원격으로 접근하려는 대상 머신 — 서버 또는 PC에서 실행됩니다. 화면 캡처, 입력 주입 및 Tailscale 네트워킹을 처리합니다.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>플랫폼</th>
          <th>다운로드</th>
        </tr>
        <tr>
          <td><b>Windows</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64-Setup.exe">설치 (.exe)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">포터블 (.zip)</a></td>
        </tr>
        <tr>
          <td><b>macOS</b><br>Apple Silicon</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">디스크 이미지 (.dmg)</a></td>
        </tr>
        <tr>
          <td><b>Linux</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.deb">Debian / Ubuntu (.deb)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.rpm">Fedora / RHEL (.rpm)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.pkg.tar.zst">Arch (.pkg.tar.zst)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">AppImage (포터블)</a></td>
        </tr>
      </table>
    </td>
    <!-- Right Column: Image -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="USBridge Agent window: connection token, Tailscale address and stream status" width="100%">
    </td>
  </tr>
</table>

## 기능

**하드웨어 가속 인코딩:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · GPU 인코더가 없는 VM 및 머신을 위한 소프트웨어 **H.264 (OpenH264)**

<table>
  <tr>
    <td width="33%" valign="top">
      <code>기본 기능</code><br><br>
      <b>SUNSHINE 오픈 소스</b><br>
      <i>표준 게임 스트리밍</i><br><br>
      ✓ 내장된 오픈 소스 Sunshine 스트리머<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">대체 스트리머</a>: Vibepollo (Apollo가 내장된 Sunshine 포크, Windows) 및 punktfunk (Moonlight 호환, Windows 및 Linux), 한 번의 클릭으로 전환<br>
      ✓ 자신의 하드웨어 및 네트워크에서 Sunshine, RustShine, Vibepollo 및 punktfunk을 비교하는 내장 벤치마크<br>
      ✓ 저지연 데스크톱 및 게임 접근<br>
      ✓ 양방향 파일 및 텍스트 클립보드<br>    
      ✓ 다중 모니터 디스플레이 전환<br>
      ✓ 통합 Tailscale P2P 네트워킹<br>
      ✓ 게스트 접근: 역할과 종료 날짜를 가진 사람 초대<br>
      ✓ 네이티브 Wayland 지원 (프롬프트 없는 캡처)
    </td>
    <td width="33%" valign="top">
      <code>+ 기본 기능</code><br><br>
      <b>USBRIDGE 무료</b><br>
      <i>Rust 기반 스트림 엔진</i><br><br>
      ✓ 즉시 연결되는 맞춤형 원격 프로토콜<br>
      ✓ 원시 <a href="https://www.usbridge.io/usb-passthrough/">USB 주변 장치 패스스루</a><br>
      ✓ Wi-Fi 안정성을 최적화한 적응형 스트리밍<br>
      ✓ HDR10<br>
      ✓ 손실 없는 4:4:4 크로마 색상 정확도<br>
      ✓ 마이크 패스스루 (클라이언트 → 호스트)<br>
      ✓ MIDI 장치 패스스루 (클라이언트 → 호스트)<br>
      ✓ GPU 인코딩: NVENC, AMF, QSV, VAAPI<br>
      ✓ GPU가 없는 VM을 위한 소프트웨어 H.264 (OpenH264)<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> 저지연 웨이브렛 코덱 (Valve 제공)<br>
      ✓ 헤드리스 가상 디스플레이 관리<br>
      ✓ 저지연 게임패드 컨트롤러 패스스루<br>
      ✓ 웹 브라우저 클라이언트 접근 (설치 필요 없음)<br>
      ✓ Windows 사전 로그인 접근 (안전한 자격 증명 입력)
    </td>
    <td width="33%" valign="top">
      <code>+ 기본 기능</code> <code>+ 무료 기능</code><br><br>
      <b>USBRIDGE 프로</b><br>
      <i>전문 워크플로우</i><br><br>
      ✓ 압력 및 기울기를 지원하는 Wacom 태블릿 지원 (<a href="https://www.usbridge.io/macos-wacom-support/">macOS에서도 지원</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">하드웨어 KVM 펌웨어</a> Radxa Zero 3W, Radxa Cubie A7Z 및 Sipeed NanoKVM용: BIOS/UEFI 접근, ISO 가상 미디어, Btrfs 스냅샷 ($19 평생 장치당, 24시간 무료 평가)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">하드웨어 Moonlight 스트리머</a> 해당 보드에서: 모든 HDMI 소스 (PC, PS5, Xbox, Switch)를 Moonlight로 1080p 60 FPS로 스트리밍, 소스에 소프트웨어 필요 없음<br><br>
    </td>
  </tr>
</table>

## 빠른 시작

1. **에이전트 설치**: 원격으로 접근하려는 머신에 에이전트를 설치합니다. 실행하면 연결 토큰과 Tailscale 주소가 표시됩니다. 인터넷을 통한 접근이 필요하면 Tailscale을 연결하세요.

2. **클라이언트 설치**: 작업대, 노트북 또는 휴대폰에 클라이언트를 설치합니다.

3. **연결 추가**: 에이전트 창에 표시된 IP 또는 Tailscale 주소를 입력합니다. 그게 전부입니다.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote quick start: install the agent, install the client, connect">
</div>

## 하드웨어 통합

<table>
  <tr>
   <td width="33%" valign="top">
      <b>하드웨어 수준 BIOS 제어</b><br>
      USBridge Remote는 OS 부팅 전에 아웃 오브 밴드, 베어 메탈 관리를 위해 USBridge-KVM 2.0 장치와 네이티브로 통합됩니다. 또한 첫 번째 하드웨어 Moonlight 스트리머입니다: 기본 Moonlight 클라이언트는 KVM과 쌍을 이루고 HDMI 소스 (PC 또는 게임 콘솔)를 소프트웨어 없이 스트리밍합니다.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVM 펌웨어</b><br>
      HDMI-to-USB 캡처 동글을 사용하여 호환되는 보드 (Radxa Zero 3W, Radxa Cubie A7Z)에 공식 펌웨어를 플래시하여 자신의 IP-KVM을 구축합니다. <b>Sipeed NanoKVM</b>에서는 기본 펌웨어를 대체합니다: 끊김 없는 60 FPS 비디오, 오디오 및 마이크, 게임패드, USB 패스스루 및 Moonlight 지원 (<a href="https://www.usbridge.io/hardware-agent/">펌웨어 다운로드</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IP 하드웨어 동글 (ESP32-S3)</b><br>
      소프트웨어 USB 패스스루는 macOS에서 벽에 부딪힙니다: 연결할 VHCI 드라이버가 없기 때문에 원격 Wacom 태블릿 (또는 다른 USB 장치)을 실제 하드웨어로 제공할 수 없습니다. 이 동글은 Mac에 플러그인되어 다른 곳에서 소프트웨어로 수행하는 <code>vhci-hcd</code>의 하드웨어 작업을 수행합니다 — 내보낸 장치는 자체 VID/PID로 열거되며, macOS의 드라이버에 의해 바인딩됩니다.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm firmware & docs"></a>
    </td>
  </tr>
</table>

### USBridge 펌웨어가 있는 NanoKVM: 지연 없이 60 FPS

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Video: Sipeed NanoKVM flashed with USBridge firmware streams at 60 FPS with almost no input lag"></a>
  <br><br>
  <img src="https://img.shields.io/badge/1080p-60_FPS-2da44e?style=flat-square" alt="1080p 60 FPS">
  <img src="https://img.shields.io/badge/Tailscale-P2P-242424?style=flat-square&logo=tailscale&logoColor=white" alt="Tailscale P2P">
  <img src="https://img.shields.io/badge/Audio-sound_%2B_mic-007ec6?style=flat-square" alt="Audio: sound and microphone">
  <img src="https://img.shields.io/badge/FEC-packet_loss_recovery-f59e0b?style=flat-square" alt="FEC: forward error correction">
</div>

2분 분량의 비디오: 가정 실험실에서 심각한 지연이 있는 Sipeed NanoKVM이 USBridge 펌웨어를 플래시한 후 거의 지연 없이 60 FPS KVM over IP로 변모하여 게임패드로 게임을 할 수 있을 만큼 빠릅니다. Tailscale은 보드 자체에서 작동하므로 포트 포워딩 없이 어디서나 접근할 수 있으며, 오디오는 양방향으로 재생되고 FEC (전방 오류 수정)가 손실된 패킷을 복구하여 Wi-Fi 및 인터넷에서 깨끗한 화질을 유지합니다. NanoKVM, Radxa Zero 3W 및 Radxa Cubie A7Z용 펌웨어는 [하드웨어 에이전트 페이지](https://www.usbridge.io/hardware-agent/)에 있습니다.

## FAQ

<details>
<summary><b>USBridge는 Moonlight 및 Sunshine과 함께 작동합니까?</b></summary>

예. 에이전트는 오픈 소스 Sunshine 스트리머를 제공하므로 기본 Moonlight 클라이언트가 연결됩니다. Vibepollo (내장된 Apollo가 있는 Sunshine 포크)와 punktfunk도 내장되어 있으며 한 번의 클릭으로 전환할 수 있습니다.
</details>

<details>
<summary><b>하드웨어 Moonlight 스트리머가 있습니까?</b></summary>

예. USBridge KVM 2.0 또는 USBridge 펌웨어로 플래시된 Radxa Zero 3W, Radxa Cubie A7Z 또는 Sipeed NanoKVM은 HDMI 입력에 있는 모든 것을 위한 Sunshine 호환 GameStream 호스트입니다: PC, PS5, Xbox 또는 Switch. 소스에 소프트웨어 없이 기본 Moonlight 클라이언트에 1080p 60 FPS로 오디오와 함께 스트리밍합니다. [하드웨어 Moonlight 스트리머](https://www.usbridge.io/moonlight-hardware-streamer/)를 참조하세요.
</details>

<details>
<summary><b>내 PC에서 가장 빠른 스트리머는 무엇입니까?</b></summary>

GPU, 드라이버 및 네트워크에 따라 다릅니다. 내장된 벤치마크는 Sunshine, RustShine, Vibepollo 및 punktfunk을 자신의 하드웨어 및 네트워크에서 실행하고 비교합니다. [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/)를 참조하세요.
</details>

<details>
<summary><b>인코딩을 지원하는 GPU는 무엇입니까?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV 및 Linux의 VAAPI. GPU 인코더가 없는 가상 머신 및 PC는 소프트웨어 H.264 (OpenH264)를 사용합니다.
</details>

<details>
<summary><b>무료 기능과 Pro 기능은 무엇입니까?</b></summary>

4:4:4 색상, HDR10, PyroWave, 게임패드, 마이크, MIDI 및 원시 USB 패스스루는 무료입니다. 압력 및 기울기를 지원하는 Wacom 태블릿과 하드웨어 KVM 펌웨어 ($19 평생 장치당, 24시간 무료 평가)는 Pro입니다.
</details>

<details>
<summary><b>Sipeed NanoKVM 또는 Radxa 보드를 IP-KVM으로 변환할 수 있습니까?</b></summary>

예. [하드웨어 에이전트 페이지](https://www.usbridge.io/hardware-agent/)에서 USBridge 펌웨어를 플래시하세요. BIOS/UEFI 접근, ISO 및 IMG 가상 미디어, 불변 Btrfs 스냅샷, 60 FPS 비디오, 오디오 및 USB 패스스루를 제공합니다. Radxa 보드는 HDMI-to-USB 캡처 동글이 필요합니다.
</details>

<details>
<summary><b>포트 포워딩이 필요합니까?</b></summary>

아니요. 연결은 Tailscale P2P를 통해 자동 NAT 트래버설로 이루어집니다.
</details>

<details>
<summary><b>다른 사람과 컴퓨터에 대한 접근을 공유할 수 있습니까?</b></summary>

예. 역할과 종료 날짜를 가진 사람을 이메일로 초대할 수 있습니다; 게스트는 USBridge를 통해 참여하며 Tailscale 계정이 필요 없습니다.
</details>

## 리소스 및 라이선스

<table>
  <tr>
    <td width="33%" valign="top">
      <b>개발 및 로드맵</b><br>
      계획된 모든 기능, 아키텍처 업데이트 및 릴리스 일정을 추적하기 위해 공개 대시보드를 유지합니다.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">실시간 로드맵 보기</a>
    </td>
    <td width="33%" valign="top">
      <b>프로젝트 링크</b><br>
      <ul>
        <li><a href="https://usbridge.io">공식 웹사이트</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (베타 테스트 및 버그)</a></li>
        <li><a href="https://www.reddit.com/r/USBridge/">레딧</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">유튜브</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Patreon 후원</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>라이선스 (GPLv3)</b><br>
      이 프로젝트는 <b>GPLv3</b> 라이선스 하에 있습니다. 다중 플랫폼 클라이언트는 <code>moonlight-common-c</code> (또한 GPLv3)에서 코드베이스를 통합합니다.<br><br>
      <a href="LICENSE">라이선스 파일 보기</a>
    </td>
  </tr>
</table>