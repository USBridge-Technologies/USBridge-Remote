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

**USBridge Remote** ist ein latenzfreies **Remote-Desktop** und **KVM-over-IP** Ökosystem: ein Client für Windows, macOS, Linux, Android, iOS und den Browser, ein Host-Agent mit einem integrierten **Sunshine / Moonlight** Spielestreamer und IP-KVM-Firmware für **Radxa Zero 3W**, **Radxa Cubie A7Z** und **Sipeed NanoKVM** Boards. Es kombiniert **Hardware-BIOS-Zugriff** und **Software-Remote-Desktop** in einer einzigen Schnittstelle, mit USB-Passthrough für Gamepads, Wacom-Tablets, MIDI und Mikrofone sowie Tailscale P2P-Netzwerk ohne Portweiterleitung.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote features: remote desktop, game streaming, USB passthrough, hardware KVM">
</div>


## Download

### Client
Der Client ist die Steuerungsschnittstelle — installiert auf Ihrem Arbeitsplatzrechner oder Laptop (oder direkt in Ihrem Browser ausgeführt). Er verwaltet Verbindungen, Live-Remote-Desktop, virtuellen Geräte-Passthrough und Snapshot-Registry.

| Architektur | Windows | macOS | Linux | Android | iOS | Webbrowser |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [App öffnen](https://web.usbridge.io) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [App öffnen](https://web.usbridge.io) |

Bevorzugen Sie eine direkte APK ohne ein Play Store-Konto? Ein selbstaktualisierender Build wird ebenfalls auf der [neuesten Version](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest) veröffentlicht.

🌐 **Zero-Install Web-Client**: Keine Installation erforderlich. Öffnen Sie einfach [web.usbridge.io](https://web.usbridge.io), um sofort zu verbinden. *(Hinweis: Der Web-Client funktioniert mit einigen Einschränkungen hinsichtlich Funktionen und Leistung aufgrund der Sicherheitseinschränkungen des Browsers und der WebRTC-Beschränkungen. Für das vollständige, uneingeschränkte Erlebnis verwenden Sie die nativen Apps).* Der Web-Client erreicht einen Agenten über seinen vertrauenswürdigen `<label>.device.usbridge.io` Namen: der Agent erhält dieses Zertifikat beim ersten Besuch durch einen Browser und nur während Sie in Ihrem USBridge-Konto im Agenten angemeldet sind — daher kann der allererste Versuch einige Minuten dauern. Siehe die Zeile **Status → Zertifikat** des Agenten oder die [Agent-Dokumentation](agent/docs/README.md#platform-notes-from-the-top-level-readme) für Details. Ein USBridge KVM bedient auch den Web-Client selbst unter `https://<kvm>:9443/` und wechselt zu seinem eigenen vertrauenswürdigen Namen, wenn es kann.

## Agent

Der Agent läuft auf der Zielmaschine — dem Server oder PC, auf den Sie remote zugreifen möchten. Er kümmert sich um Bildschirmaufnahme, Eingabeinjektion und Tailscale-Netzwerk.

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

**Hardwarebeschleunigte Kodierung:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · plus Software **H.264 (OpenH264)** für VMs und Maschinen ohne GPU-Kodierer

<table>
  <tr>
    <td width="33%" valign="top">
      <code>BASISFUNKTIONALITÄT</code><br><br>
      <b>SUNSHINE OPEN-SOURCE</b><br>
      <i>Standard-Spielestreaming</i><br><br>
      ✓ Eingebauter Open-Source Sunshine-Streamer<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">Alternative Streamer</a>: Vibepollo (Sunshine-Fork mit Apollo integriert, Windows) und punktfunk (Moonlight-kompatibel, Windows & Linux), wechseln mit einem Klick<br>
      ✓ Eingebauter Benchmark zum Vergleich von Sunshine, RustShine, Vibepollo und punktfunk auf Ihrer eigenen Hardware und Ihrem Netzwerk<br>
      ✓ Latenzfreier Desktop- & Gaming-Zugriff<br>
      ✓ Bidirektionaler Datei- & Text-Clipboard<br>    
      ✓ Multi-Monitor-Anzeigeschaltung<br>
      ✓ Integriertes Tailscale P2P-Netzwerk<br>
      ✓ Gastzugang: Personen per E-Mail mit einer Rolle und einem Enddatum einladen<br>
      ✓ Native Wayland-Unterstützung (promptlose Aufnahme)
    </td>
    <td width="33%" valign="top">
      <code>+ BASISFUNKTIONEN</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Rust-basierte Stream-Engine</i><br><br>
      ✓ Sofortverbindung mit benutzerdefiniertem Remote-Protokoll<br>
      ✓ Rohes <a href="https://www.usbridge.io/usb-passthrough/">USB-Peripheriegeräte-Passthrough</a><br>
      ✓ Adaptives Streaming optimiert für Wi-Fi-Stabilität<br>
      ✓ HDR10<br>
      ✓ Verlustfreie 4:4:4 Chroma-Farbenaufrichtigkeit<br>
      ✓ Mikrofon-Passthrough (Client → Host)<br>
      ✓ MIDI-Geräte-Passthrough (Client → Host)<br>
      ✓ GPU-Kodierung: NVENC, AMF, QSV, VAAPI<br>
      ✓ Software H.264 (OpenH264) für VMs ohne GPU<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> Latenzfreier Wavelet-Codec (von Valve)<br>
      ✓ Headless-Management virtueller Displays<br>
      ✓ Latenzfreies Gamepad-Controller-Passthrough<br>
      ✓ Webbrowser-Clientzugang (keine Installation erforderlich)<br>
      ✓ Windows-Zugriff vor der Anmeldung (sichere Eingabe von Anmeldeinformationen)
    </td>
    <td width="33%" valign="top">
      <code>+ BASISFUNKTIONEN</code> <code>+ KOSTENLOSE FUNKTIONEN</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Professionelle Workflows</i><br><br>
      ✓ Wacom-Tablet-Unterstützung mit Druck- & Neigung (<a href="https://www.usbridge.io/macos-wacom-support/">auch macOS</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">Hardware KVM-Firmware</a> für Radxa Zero 3W, Radxa Cubie A7Z und Sipeed NanoKVM: BIOS/UEFI-Zugriff, ISO-virtuelle Medien, Btrfs-Snapshots (19 $ lebenslang pro Gerät, 24 h kostenlose Bewertung)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">Hardware Moonlight-Streamer</a> auf diesen Boards: jede HDMI-Quelle (PC, PS5, Xbox, Switch) zu Moonlight in 1080p 60 FPS, keine Software auf der Quelle<br><br>
    </td>
  </tr>
</table>

## Schnellstart

1. **Installieren Sie den Agenten** auf der Maschine, auf die Sie remote zugreifen möchten. Starten Sie ihn — er zeigt ein Verbindungstoken und die Tailscale-Adresse an. Verbinden Sie Tailscale, wenn Sie über das Internet zugreifen müssen.

2. **Installieren Sie den Client** auf Ihrem Arbeitsplatzrechner, Laptop oder Telefon.

3. **Fügen Sie eine Verbindung hinzu** — geben Sie die IP- oder Tailscale-Adresse ein, die im Agentenfenster angezeigt wird. Das war's.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote quick start: install the agent, install the client, connect">
</div>

## Hardware-Integration

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Hardware-Level BIOS-Kontrolle</b><br>
      USBridge Remote integriert sich nativ mit dem USBridge-KVM 2.0 Gerät für Out-of-Band, Bare-Metal-Management vor dem OS-Boot. Es ist auch der erste Hardware Moonlight-Streamer: der Standard Moonlight-Client koppelt sich mit dem KVM und streamt jede HDMI-Quelle (einen PC oder eine Spielkonsole) ohne Software darauf.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVM-Firmware</b><br>
      Flashen Sie die offizielle Firmware auf einem kompatiblen Board (Radxa Zero 3W, Radxa Cubie A7Z) mit einem HDMI-zu-USB-Capture-Dongle, um Ihr eigenes IP-KVM zu erstellen. Auf einem <b>Sipeed NanoKVM</b> ersetzt es die Standardfirmware: flüssige 60 FPS anstelle eines ruckelnden Bildes, Audio und Mikrofon, Gamepads, USB-Passthrough und Moonlight-Unterstützung (<a href="https://www.usbridge.io/hardware-agent/">Firmware herunterladen</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Firmware herunterladen"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IP Hardware-Dongle (ESP32-S3)</b><br>
      Software-USB-Passthrough stößt auf ein Hindernis unter macOS: Es gibt keinen VHCI-Treiber, an den man sich anhängen kann, sodass ein remote Wacom-Tablet (oder ein anderes USB-Gerät) nicht als echtes Hardwaregerät präsentiert werden kann. Dieser Dongle wird in den Mac eingesteckt und macht in Hardware, was <code>vhci-hcd</code> in Software anderswo macht — das exportierte Gerät wird mit seiner eigenen VID/PID aufgelistet, gebunden durch den eigenen Treiber von macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm Firmware & Dokumentation"></a>
    </td>
  </tr>
</table>

### NanoKVM mit USBridge-Firmware: 60 FPS statt Verzögerung

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Video: Sipeed NanoKVM geflasht mit USBridge-Firmware streamt bei 60 FPS mit fast keiner Eingabeverzögerung"></a>
</div>

Ein 2-minütiges Video: ein Sipeed NanoKVM mit starker Verzögerung in einem Heimlabor wird nach dem Flashen der USBridge-Firmware zu einem 60 FPS KVM über IP mit fast keiner Eingabeverzögerung, schnell genug, um Spiele mit einem Gamepad zu spielen. Firmware für das NanoKVM, Radxa Zero 3W und Radxa Cubie A7Z ist auf der [Hardware-Agent-Seite](https://www.usbridge.io/hardware-agent/) verfügbar.

## FAQ

<details>
<summary><b>Funktioniert USBridge mit Moonlight und Sunshine?</b></summary>

Ja. Der Agent liefert den Open-Source Sunshine-Streamer, sodass der Standard Moonlight-Client sich damit verbindet. Vibepollo (ein Sunshine-Fork mit Apollo integriert) und punktfunk sind ebenfalls integriert und wechseln mit einem Klick.
</details>

<details>
<summary><b>Gibt es einen Hardware-Moonlight-Streamer?</b></summary>

Ja. Ein USBridge KVM 2.0 oder ein Radxa Zero 3W, Radxa Cubie A7Z oder Sipeed NanoKVM, geflasht mit USBridge-Firmware, ist ein Sunshine-kompatibler GameStream-Host für alles, was an seinem HDMI-Eingang angeschlossen ist: ein PC, PS5, Xbox oder Switch. Es streamt 1080p 60 FPS mit Audio an den Standard Moonlight-Client, ohne Software auf der Quelle. Siehe [Hardware Moonlight Streamer](https://www.usbridge.io/moonlight-hardware-streamer/).
</details>

<details>
<summary><b>Welcher Streamer ist am schnellsten auf meinem PC?</b></summary>

Es hängt von der GPU, dem Treiber und dem Netzwerk ab. Der eingebaute Benchmark führt Sunshine, RustShine, Vibepollo und punktfunk auf Ihrer eigenen Hardware und Ihrem Netzwerk aus und vergleicht sie. Siehe [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/).
</details>

<details>
<summary><b>Welche GPUs werden für die Kodierung unterstützt?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV und VAAPI unter Linux. Virtuelle Maschinen und PCs ohne GPU-Kodierer verwenden Software H.264 (OpenH264).
</details>

<details>
<summary><b>Was ist kostenlos und was benötigt Pro?</b></summary>

4:4:4 Farben, HDR10, PyroWave, Gamepads, Mikrofon, MIDI und rohes USB-Passthrough sind kostenlos. Wacom-Tablets mit Druck und Neigung sowie die Hardware-KVM-Firmware (19 $ lebenslang pro Gerät, 24 h kostenlose Bewertung) sind Pro.
</details>

<details>
<summary><b>Kann ich ein Sipeed NanoKVM oder ein Radxa-Board in ein IP-KVM verwandeln?</b></summary>

Ja. Flashen Sie die USBridge-Firmware von der [Hardware-Agent-Seite](https://www.usbridge.io/hardware-agent/). Sie erhalten BIOS/UEFI-Zugriff, ISO- und IMG-virtuelle Medien, unveränderliche Btrfs-Snapshots, 60 FPS Video, Audio und USB-Passthrough. Radxa-Boards benötigen einen HDMI-zu-USB-Capture-Dongle.
</details>

<details>
<summary><b>Benötige ich Portweiterleitung?</b></summary>

Nein. Verbindungen erfolgen über Tailscale Peer-to-Peer mit automatischer NAT-Traversierung.
</details>

<details>
<summary><b>Kann ich den Zugriff auf einen Computer mit jemand anderem teilen?</b></summary>

Ja. Laden Sie Personen per E-Mail mit einer Rolle und einem Enddatum ein; Gäste treten über USBridge bei und benötigen kein Tailscale-Konto.
</details>

## Ressourcen & Lizenz

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Entwicklung & Roadmap</b><br>
      Ich pflege ein öffentliches Dashboard, um alle geplanten Funktionen, architektonischen Updates und Veröffentlichungspläne zu verfolgen.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Live-Roadmap anzeigen</a>
    </td>
    <td width="33%" valign="top">
      <b>Projektlinks</b><br>
      <ul>
        <li><a href="https://usbridge.io">Offizielle Website</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Beta-Tests & Fehler)</a></li>
        <li><a href="https://www.reddit.com/r/USBridge/">Reddit</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">YouTube</a></li>
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