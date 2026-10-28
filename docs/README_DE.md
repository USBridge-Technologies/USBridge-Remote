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

**USBridge Remote** ist ein einheitlicher Hochleistungs-Client zur Verwaltung von Remote-Maschinen. Entwickelt, um **Hardware-BIOS-Zugriff** (über USBridge KVM-Geräte) und **softwarebasierten Remote-Desktop** in einer einzigen, optimierten Benutzeroberfläche zu kombinieren.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote">
</div>


## Download

### Client
Der Client ist die Steueroberfläche — installiert auf Ihrem Arbeitsplatz oder Laptop (oder direkt in Ihrem Browser ausgeführt). Er verwaltet Verbindungen, Live-Remote-Desktop, virtuellen Geräte-Passthrough und Snapshot-Registry.

| Architektur | Windows | macOS | Linux | Android | iOS | Webbrowser |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [App öffnen](https://web.usbridge.io) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [App öffnen](https://web.usbridge.io) |

Bevorzugen Sie ein direktes APK ohne Play Store-Konto? Ein selbstaktualisierender Build wird ebenfalls in der [neuesten Version](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest) veröffentlicht.

🌐 **Zero-Install Web-Client**: Keine Installation erforderlich. Öffnen Sie einfach [web.usbridge.io](https://web.usbridge.io), um sofort zu verbinden. *(Hinweis: Der Web-Client funktioniert mit einigen Einschränkungen hinsichtlich Funktionen und Leistung aufgrund der Sicherheits-Sandbox des Browsers und der WebRTC-Beschränkungen. Für das vollständige, unkompromittierte Erlebnis verwenden Sie die nativen Apps).* Der Web-Client erreicht einen Agenten über seinen vertrauenswürdigen `<label>.device.usbridge.io` Namen: Der Agent erhält dieses Zertifikat beim ersten Mal, wenn ein Browser darauf zugreift, und nur während Sie in Ihrem USBridge-Konto im Agenten angemeldet sind — daher kann der allererste Versuch einige Minuten dauern. Siehe die Zeile **Status → Zertifikat** des Agenten oder die [Agent-Dokumentation](agent/docs/README.md#platform-notes-from-the-top-level-readme) für Details. Ein USBridge KVM dient auch dem Web-Client selbst unter `https://<kvm>:9443/` und wechselt zu seinem eigenen vertrauenswürdigen Namen, wenn es kann.

## Agent

Der Agent läuft auf der Zielmaschine — dem Server oder PC, auf den Sie remote zugreifen möchten. Er verarbeitet Bildschirmaufnahmen, Eingabeinjektionen und Tailscale-Netzwerke.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Architektur</th>
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

## Funktionen

**Hardware-beschleunigte Codierung:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · plus Software **H.264 (OpenH264)** für VMs und Maschinen ohne GPU-Codierer

<table>
  <tr>
    <td width="33%" valign="top">
      <code>BASISFUNKTIONALITÄT</code><br><br>
      <b>SUNSHINE OPEN-SOURCE</b><br>
      <i>Standard Game Streaming</i><br><br>
      ✓ Eingebauter Open-Source Sunshine-Streamer<br>
      ✓ Niedriglatente Desktop- & Gaming-Zugriffe<br>
      ✓ Bidirektionale Datei- & Textzwischenablage<br>    
      ✓ Multi-Monitor-Anzeigeschaltung<br>
      ✓ Integriertes Tailscale P2P-Netzwerk<br>
      ✓ Gastzugang: Laden Sie Personen per E-Mail mit einer Rolle und einem Enddatum ein<br>
      ✓ Native Wayland-Unterstützung (promptloses Capturing)
    </td>
    <td width="33%" valign="top">
      <code>+ BASISFUNKTIONEN</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Rust-basierte Stream-Engine</i><br><br>
      ✓ Sofortverbindung mit benutzerdefiniertem Remote-Protokoll<br>
      ✓ Roh-USB-Peripheriegeräte-Passthrough<br>
      ✓ Adaptives Streaming optimiert für Wi-Fi-Stabilität<br>
      ✓ HDR10<br>
      ✓ Verlustfreie 4:4:4 Chroma-Farbtiefe<br>
      ✓ Mikrofon-Passthrough (Client → Host)<br>
      ✓ MIDI-Geräte-Passthrough (Client → Host)<br>
      ✓ GPU-Codierung: NVENC, AMF, QSV, VAAPI<br>
      ✓ Software H.264 (OpenH264) für VMs ohne GPU<br>
      ✓ PyroWave-Niedriglatente Wavelet-Codierung (von Valve)<br>
      ✓ Headless-Management virtueller Displays<br>
      ✓ Niedriglatente Gamepad-Controller-Passthrough<br>
      ✓ Webbrowser-Clientzugang (keine Installation erforderlich)<br>
      ✓ Windows-Zugriff vor der Anmeldung (sichere Eingabe von Anmeldeinformationen)
    </td>
    <td width="33%" valign="top">
      <code>+ BASISFUNKTIONEN</code> <code>+ KOSTENLOSE FUNKTIONEN</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Professionelle Workflows</i><br><br>
      ✓ Wacom-Tablet-Unterstützung mit Druck- & Neigungsfunktion<br><br>
    </td>
  </tr>
</table>

## Schnellstart

1. **Installieren Sie den Agenten** auf der Maschine, auf die Sie remote zugreifen möchten. Starten Sie ihn — er zeigt ein Verbindungstoken und eine Tailscale-Adresse an. Verbinden Sie Tailscale, wenn Sie über das Internet zugreifen müssen.

2. **Installieren Sie den Client** auf Ihrem Arbeitsplatz, Laptop oder Telefon.

3. **Fügen Sie eine Verbindung hinzu** — geben Sie die IP- oder Tailscale-Adresse ein, die im Agentenfenster angezeigt wird. Das war's.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## Hardware-Integration

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Hardware-Level BIOS-Steuerung</b><br>
      USBridge Remote integriert sich nativ mit dem USBridge-KVM 2.0-Gerät für Out-of-Band-Management vor dem OS-Boot.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Kaufen Sie USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVM-Firmware</b><br>
      Setzen Sie die offizielle Firmware auf einem kompatiblen SBC (z.B. Radxa Zero 3W/3E, Cubie A7) mit einer USB-Capture-Schnittstelle ein, um einen benutzerdefinierten KVM-Knoten bereitzustellen.<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Firmware herunterladen"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IP Hardware-Dongle (ESP32-S3)</b><br>
      Software-USB-Passthrough stößt auf macOS an eine Wand: Es gibt keinen VHCI-Treiber, an den man anschließen kann, sodass ein entferntes Wacom-Tablet (oder ein anderes USB-Gerät) nicht als echtes Hardwaregerät präsentiert werden kann. Dieser Dongle wird in den Mac eingesteckt und erledigt in Hardware, was <code>vhci-hcd</code> in Software anderswo tut — das exportierte Gerät wird mit seiner eigenen VID/PID aufgelistet, gebunden durch den eigenen Treiber von macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm Firmware & Dokumentation"></a>
    </td>
  </tr>
</table>

## Ressourcen & Lizenz

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Entwicklung & Fahrplan</b><br>
      Ich pflege ein öffentliches Dashboard, um alle geplanten Funktionen, architektonischen Updates und Veröffentlichungszeitpläne zu verfolgen.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Live-Fahrplan anzeigen</a>
    </td>
    <td width="33%" valign="top">
      <b>Projektlinks</b><br>
      <ul>
        <li><a href="https://usbridge.io">Offizielle Website</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Beta-Tests & Fehler)</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Patreon-Unterstützung</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Lizenz (GPLv3)</b><br>
      Dieses Projekt ist unter der <b>GPLv3</b> lizenziert. Der plattformübergreifende Client integriert Code aus <code>moonlight-common-c</code> (ebenfalls GPLv3).<br><br>
      <a href="LICENSE">Lizenzdatei anzeigen</a>
    </td>
  </tr>
</table>