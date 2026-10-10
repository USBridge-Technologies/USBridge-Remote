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

**USBridge Remote**는 원격 머신을 관리하기 위한 통합 고성능 클라이언트입니다. **하드웨어 수준의 BIOS 접근**(USBridge KVM 장치를 통해)과 **소프트웨어 기반 원격 데스크탑**을 단일화된 간소화된 인터페이스로 결합하도록 설계되었습니다.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote">
</div>


## 다운로드

### 클라이언트
클라이언트는 제어 인터페이스로, 작업대나 노트북에 설치되거나 브라우저에서 직접 실행됩니다. 연결 관리, 실시간 원격 데스크탑, 가상 장치 패스스루 및 스냅샷 레지스트리를 관리합니다.

| 아키텍처 | Windows | macOS | Linux | Android | iOS | 웹 브라우저 |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [다운로드](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [다운로드](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [앱 열기](https://web.usbridge.io) |
| **ARM64** | — | [다운로드](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [앱 열기](https://web.usbridge.io) |

Play 스토어 계정 없이 직접 APK를 선호하십니까? 자가 업데이트 빌드는 [최신 릴리스](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest)에서도 게시됩니다.

🌐 **제로 설치 웹 클라이언트**: 설치가 필요 없습니다. [web.usbridge.io](https://web.usbridge.io)를 열어 즉시 연결하세요. *(참고: 웹 클라이언트는 브라우저 보안 샌드박스 및 WebRTC 제약으로 인해 일부 기능 및 성능 제한이 있습니다. 완전한 경험을 원하시면 네이티브 앱을 사용하세요).* 웹 클라이언트는 신뢰할 수 있는 `<label>.device.usbridge.io` 이름을 통해 에이전트에 도달합니다: 에이전트는 사용자가 에이전트에 로그인할 때 처음으로 브라우저가 접근할 때 해당 인증서를 받으며, 따라서 첫 번째 시도는 몇 분이 걸릴 수 있습니다. 에이전트의 **상태 → 인증서** 행 또는 [에이전트 문서](agent/docs/README.md#platform-notes-from-the-top-level-readme)에서 자세한 내용을 확인하세요. USBridge KVM은 `https://<kvm>:9443/`에서 웹 클라이언트 자체를 제공하며, 가능할 경우 자신의 신뢰할 수 있는 이름으로 이동합니다.

## 에이전트

에이전트는 원격으로 접근하고자 하는 대상 머신 — 서버 또는 PC에서 실행됩니다. 화면 캡처, 입력 주입 및 Tailscale 네트워킹을 처리합니다.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>아키텍처</th>
          <th>Windows</th>
          <th>macOS</th>
          <th>Linux</th>
        </tr>
        <tr>
          <td><b>x86_64</b></td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">다운로드</a></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">다운로드</a></td>
        </tr>
        <tr>
          <td><b>ARM64</b></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">다운로드</a></td>
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

## 기능

<table>
  <tr>
    <td width="33%" valign="top">
      <code>기본 기능</code><br><br>
      <b>선샤인 오픈 소스</b><br>
      <i>표준 게임 스트리밍</i><br><br>
      ✓ 내장된 오픈 소스 선샤인 스트리머<br>
      ✓ 저지연 데스크탑 및 게임 접근<br>
      ✓ 양방향 파일 및 텍스트 클립보드<br>    
      ✓ 다중 모니터 디스플레이 전환<br>
      ✓ 통합 Tailscale P2P 네트워킹<br>
      ✓ 네이티브 Wayland 지원 (프롬프트 없는 캡처)
    </td>
    <td width="33%" valign="top">
      <code>+ 기본 기능</code><br><br>
      <b>USBRIDGE 무료</b><br>
      <i>Rust 기반 스트림 엔진</i><br><br>
      ✓ 즉시 연결되는 사용자 정의 원격 프로토콜<br>
      ✓ 원시 USB 주변 장치 패스스루<br>
      ✓ Wi-Fi 안정성을 최적화한 적응형 스트리밍<br>
      ✓ HDR10<br>
      ✓ 헤드리스 가상 디스플레이 관리<br>
      ✓ 저지연 게임패드 컨트롤러 패스스루<br>
      ✓ 웹 브라우저 클라이언트 접근 (설치 필요 없음)<br>
      ✓ Windows 사전 로그인 접근 (안전한 자격 증명 입력)
    </td>
    <td width="33%" valign="top">
      <code>+ 기본 기능</code> <code>+ 무료 기능</code><br><br>
      <b>USBRIDGE 프로</b><br>
      <i>전문 워크플로우</i><br><br>
      ✓ 손실 없는 4:4:4 크로마 색상 정확도<br>
      ✓ 압력 및 기울기를 지원하는 Wacom 태블릿 지원<br><br>
    </td>
  </tr>
</table>

## 빠른 시작

1. **에이전트 설치**: 원격으로 접근하고자 하는 머신에 에이전트를 설치합니다. 실행하면 연결 토큰과 Tailscale 주소가 표시됩니다. 인터넷을 통해 접근해야 하는 경우 Tailscale을 연결합니다.

2. **클라이언트 설치**: 작업대, 노트북 또는 휴대폰에 클라이언트를 설치합니다.

3. **연결 추가**: 에이전트 창에 표시된 IP 또는 Tailscale 주소를 입력합니다. 그게 전부입니다.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## 하드웨어 통합

<table>
  <tr>
   <td width="33%" valign="top">
      <b>하드웨어 수준의 BIOS 제어</b><br>
      USBridge Remote는 OS 부팅 전에 아웃 오브 밴드, 베어 메탈 관리를 위해 USBridge-KVM 2.0 장치와 네이티브로 통합됩니다.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVM 펌웨어</b><br>
      USB 캡처 인터페이스가 있는 호환 SBC(예: Radxa Zero 3W/3E, Cubie A7)에 공식 펌웨어를 배포하여 사용자 정의 KVM 노드를 프로비저닝합니다.<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IP 하드웨어 동글 (ESP32-S3)</b><br>
      소프트웨어 USB 패스스루는 macOS에서 한계에 부딪힙니다: 연결할 VHCI 드라이버가 없기 때문에 원격 Wacom 태블릿(또는 다른 USB 장치)을 실제 하드웨어로 제시할 수 없습니다. 이 동글은 Mac에 플러그인되어 <code>vhci-hcd</code>가 다른 곳에서 소프트웨어로 수행하는 작업을 하드웨어에서 수행합니다 — 내보낸 장치는 macOS의 자체 드라이버에 의해 바인딩된 고유한 VID/PID로 열거됩니다.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm firmware & docs"></a>
    </td>
  </tr>
</table>

## 리소스 및 라이센스

<table>
  <tr>
    <td width="33%" valign="top">
      <b>개발 및 로드맵</b><br>
      저는 모든 계획된 기능, 아키텍처 업데이트 및 릴리스 일정을 추적하기 위해 공개 대시보드를 유지합니다.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">라이브 로드맵 보기</a>
    </td>
    <td width="33%" valign="top">
      <b>프로젝트 링크</b><br>
      <ul>
        <li><a href="https://usbridge.io">공식 웹사이트</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (베타 테스트 및 버그)</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Patreon 지원</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>라이센스 (GPLv3)</b><br>
      이 프로젝트는 <b>GPLv3</b> 라이센스 하에 있습니다. 다중 플랫폼 클라이언트는 <code>moonlight-common-c</code> (또한 GPLv3)에서 코드베이스를 통합합니다.<br><br>
      <a href="LICENSE">라이센스 파일 보기</a>
    </td>
  </tr>
</table>