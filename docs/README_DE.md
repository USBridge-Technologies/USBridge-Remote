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

**USBridge Remote** ist ein latenzfreies **Remote Desktop** und **KVM-over-IP** Ökosystem: ein Client für Windows, macOS, Linux, Android, iOS und den Browser, ein Host-Agent mit einem integrierten **Sunshine / Moonlight** Spielestreamer und IP-KVM-Firmware für **Radxa Zero 3W/3E**, **Radxa Cubie A7Z** und **Sipeed NanoKVM** Boards. Es kombiniert **hardware-level BIOS-Zugriff** und **software Remote Desktop** in einer einzigen Schnittstelle, mit USB-Passthrough für Gamepads, Wacom-Tablets, MIDI und Mikrofone sowie Tailscale P2P-Netzwerk ohne Portweiterleitung.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote features: remote desktop, game streaming, USB passthrough, hardware KVM">
</div>


## Download

**All-in-one Installer (Client + Agent, wähle während der Installation entweder oder beide):** [Windows Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) · [macOS Disk Image (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-macOS-arm64.dmg)

Alle Dateien sind im [neuesten Release](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

### Client
Der Client ist die Steueroberfläche — installiert auf deinem Arbeitsplatz oder Laptop (oder direkt im Browser ausgeführt). Er verwaltet Verbindungen, live Remote Desktop, virtuellen Geräte-Passthrough und Snapshot-Registry.

| Plattform | Installer | Portable | Pakete / Store |
| :--- | :--- | :--- | :--- |
| **Windows** x86_64 | [Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) | [Portable (.zip)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — |
| **macOS** Apple Silicon | [Disk Image (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | — |
| **Linux** x86_64 | [Debian / Ubuntu (.deb)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.deb) · [Fedora / RHEL (.rpm)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.rpm) · [Arch (.pkg.tar.zst)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.pkg.tar.zst) | [AppImage](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | [APT · DNF · pacman Repository](#install-from-the-terminal) |
| **Android** ARM64 | [APK (selbstaktualisierend)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) |
| **iOS / iPadOS** | — | — | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |
| **Webbrowser** | — | [Open web.usbridge.io](https://web.usbridge.io) | — |

### Installation über das Terminal

Ein Befehl installiert den Client und den Agenten. Unter Linux wird das native Paket für deine Distribution ausgewählt (apt auf Debian/Ubuntu, dnf auf Fedora/RHEL, pacman auf Arch), und das Paket fügt das signierte USBridge-Repository hinzu, sodass die Apps dann mit dem Rest des Systems aktualisiert werden:

```sh
# Linux und macOS
curl -fsSL https://www.usbridge.io/install.sh | sh
# Nur Agent (eine Maschine, die du nur remote erreichst)
curl -fsSL https://www.usbridge.io/install.sh | sh -s -- --agent
```

```powershell
# Windows (PowerShell)
irm https://www.usbridge.io/install.ps1 | iex
```

<details>
<summary><b>Füge das APT-Repository manuell hinzu (Debian / Ubuntu)</b></summary>

```sh
curl -fsSL https://raw.githubusercontent.com/USBridge-Technologies/USBridge-Remote/main/scripts/deb/usbridge-archive-keyring.gpg \
  | sudo tee /usr/share/keyrings/usbridge-archive-keyring.gpg >/dev/null
echo "deb [arch=amd64 signed-by=/usr/share/keyrings/usbridge-archive-keyring.gpg] https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download ./" \
  | sudo tee /etc/apt/sources.list.d/usbridge.list
sudo apt update && sudo apt install usbridge-client usbridge-agent
```

Ein heruntergeladenes `.deb`, `.rpm` oder Arch-Paket richtet dasselbe Repository ein, wenn du es installierst.
</details>

🌐 **Zero-Install Web Client**: Keine Installation erforderlich. Öffne einfach [web.usbridge.io](https://web.usbridge.io), um sofort zu verbinden. *(Hinweis: Der Web-Client funktioniert mit einigen Einschränkungen bei Funktionen und Leistung aufgrund der Sicherheitseinschränkungen des Browsers und der WebRTC-Beschränkungen. Für das vollständige, unkompromittierte Erlebnis verwende die nativen Apps).* Der Web-Client erreicht einen Agenten über seinen vertrauenswürdigen `<label>.device.usbridge.io` Namen: der Agent erhält dieses Zertifikat beim ersten Besuch eines Browsers, und nur während du in deinem USBridge-Konto im Agenten angemeldet bist — daher kann der allererste Versuch einige Minuten dauern. Siehe die **Status → Zertifikat** Zeile des Agenten oder die [Agenten-Dokumentation](agent/docs/README.md#platform-notes-from-the-top-level-readme) für Details. Ein USBridge KVM dient auch dem Web-Client selbst unter `https://<kvm>:9443/` und wechselt zu seinem eigenen vertrauenswürdigen Namen, wenn es kann.

## Agent

Der Agent läuft auf der Zielmaschine — dem Server oder PC, auf den du remote zugreifen möchtest. Er erfasst den Bildschirm und den Ton, leitet deine Tastatur, Maus und Gamepad an diese Maschine weiter und verbindet sie über Tailscale.

<table>
  <tr>
    <!-- Linke Spalte: Downloads Tabelle -->
    <td valign="middle">
      <table>
        <tr>
          <th>Plattform</th>
          <th>Downloads</th>
        </tr>
        <tr>
          <td><b>Windows</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64-Setup.exe">Setup (.exe)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Portable (.zip)</a></td>
        </tr>
        <tr>
          <td><b>macOS</b><br>Apple Silicon</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Disk Image (.dmg)</a></td>
        </tr>
        <tr>
          <td><b>Linux</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.deb">Debian / Ubuntu (.deb)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.rpm">Fedora / RHEL (.rpm)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.pkg.tar.zst">Arch (.pkg.tar.zst)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">AppImage (portable)</a></td>
        </tr>
      </table>
    </td>
    <!-- Rechte Spalte: Bild -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="USBridge Agent Fenster: Verbindungstoken, Tailscale-Adresse und Stream-Status" width="100%">
    </td>
  </tr>
</table>

## Funktionen

**Hardwarebeschleunigte Codierung:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · plus Software **H.264 (OpenH264)** für VMs und Maschinen ohne GPU-Codierer

<table>
  <tr>
    <td width="33%" valign="top">
      <code>BASISFUNKTIONALITÄT</code><br><br>
      <b>SUNSHINE OPEN-SOURCE</b><br>
      <i>Standard Game Streaming</i><br><br>
      ✓ Eingebauter Open-Source Sunshine-Streamer<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">Alternative Streamer</a>: Vibepollo (Sunshine-Fork mit Apollo integriert, Windows) und punktfunk (Moonlight-kompatibel, Windows & Linux), Wechsel mit einem Klick<br>
      ✓ Eingebauter Benchmark zum Vergleich von Sunshine, RustShine, Vibepollo und punktfunk auf deiner eigenen Hardware und deinem Netzwerk<br>
      ✓ Latenzfreier Desktop- & Gaming-Zugriff<br>
      ✓ Bidirektionale Datei- & Textzwischenablage<br>    
      ✓ Multi-Monitor-Anzeigeschaltung<br>
      ✓ Integriertes Tailscale P2P-Netzwerk<br>
      ✓ Gastzugang: lade Personen per E-Mail mit einer Rolle und einem Enddatum ein<br>
      ✓ Native Wayland-Unterstützung (promptlose Erfassung)
    </td>
    <td width="33%" valign="top">
      <code>+ BASISFUNKTIONEN</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Rust-basierte Stream Engine</i><br><br>
      ✓ Sofortverbindung mit benutzerdefiniertem Remote-Protokoll<br>
      ✓ Rohes <a href="https://www.usbridge.io/usb-passthrough/">USB-Peripheriegeräte-Passthrough</a><br>
      ✓ Adaptives Streaming optimiert für Wi-Fi-Stabilität<br>
      ✓ HDR10<br>
      ✓ Verlustfreie 4:4:4 Chroma-Farbenaufrichtigkeit<br>
      ✓ Mikrofon-Passthrough (Client → Host)<br>
      ✓ MIDI-Geräte-Passthrough (Client → Host)<br>
      ✓ GPU-Codierung: NVENC, AMF, QSV, VAAPI<br>
      ✓ Software H.264 (OpenH264) für VMs ohne GPU<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> Latenzfreier Wavelet-Codec (von Valve)<br>
      ✓ Headless-Management virtueller Displays<br>
      ✓ Latenzfreies Gamepad-Controller-Passthrough<br>
      ✓ Webbrowser-Client-Zugriff (keine Installation erforderlich)<br>
      ✓ Windows-Vor-Anmeldezugriff (sichere Eingabe von Anmeldeinformationen)
    </td>
    <td width="33%" valign="top">
      <code>+ BASISFUNKTIONEN</code> <code>+ KOSTENLOSE FUNKTIONEN</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Professionelle Workflows</i><br><br>
      ✓ Wacom-Tablet-Unterstützung mit Druck & Neigung (<a href="https://www.usbridge.io/macos-wacom-support/">macOS ebenfalls</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">Hardware KVM-Firmware</a> für Radxa Zero 3W/3E, Radxa Cubie A7Z und Sipeed NanoKVM: BIOS/UEFI-Zugriff, ISO-virtuelle Medien, Btrfs-Snapshots (19 $ lebenslang pro Gerät, 24 h kostenlose Bewertung)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">Hardware Moonlight-Streamer</a> auf diesen Boards: jede HDMI-Quelle (PC, PS5, Xbox, Switch) zu Moonlight in 1080p 60 FPS, keine Software auf der Quelle<br><br>
    </td>
  </tr>
</table>

## Schnellstart

1. **Installiere den Agenten** auf der Maschine, auf die du remote zugreifen möchtest. Starte ihn — er zeigt ein Verbindungstoken und eine Tailscale-Adresse an. Verbinde Tailscale, wenn du über das Internet zugreifen musst.

2. **Installiere den Client** auf deinem Arbeitsplatz, Laptop oder Telefon.

3. **Füge eine Verbindung hinzu** — gib die IP- oder Tailscale-Adresse ein, die im Agentenfenster angezeigt wird. Das war's.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote schnellstart: installiere den agenten, installiere den client, verbinde">
</div>

## Hardware-Integration

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Hardware-Level BIOS-Kontrolle</b><br>
      USBridge Remote integriert sich nativ mit dem USBridge-KVM 2.0 Gerät für Out-of-Band, Bare-Metal-Management vor dem OS-Boot. Es ist auch der erste Hardware Moonlight-Streamer: der Standard-Moonlight-Client koppelt sich mit dem KVM und streamt jede HDMI-Quelle (einen PC oder eine Spielkonsole) ohne Software darauf.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Kaufe USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVM-Firmware</b><br>
      Flash die offizielle Firmware auf einem kompatiblen Board (Radxa Zero 3W/3E, Radxa Cubie A7Z) mit einem HDMI-zu-USB-Capture-Dongle, um dein eigenes IP-KVM zu erstellen. Auf einem <b>Sipeed NanoKVM</b> ersetzt es die Standardfirmware: flüssige 60 FPS anstelle eines ruckelnden Bildes, Audio und Mikrofon, Gamepads, USB-Passthrough und Moonlight-Unterstützung (<a href="https://www.usbridge.io/hardware-agent/">lade die Firmware herunter</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Firmware herunterladen"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IP Hardware-Dongle (ESP32-S3)</b><br>
      Software-USB-Passthrough stößt auf ein Hindernis unter macOS: Es gibt keinen VHCI-Treiber, an den man sich anhängen kann, sodass ein remote Wacom-Tablet (oder ein anderes USB-Gerät) nicht als echtes Hardware präsentiert werden kann. Dieser Dongle wird in den Mac gesteckt und macht in Hardware, was <code>vhci-hcd</code> anderswo in Software macht — das exportierte Gerät wird mit seiner eigenen VID/PID aufgelistet, gebunden durch den eigenen Treiber von macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm Firmware & Dokumentation"></a>
    </td>
  </tr>
</table>

### NanoKVM mit USBridge-Firmware: 60 FPS statt Verzögerung

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Video: Sipeed NanoKVM mit USBridge-Firmware streamt bei 60 FPS mit fast keiner Eingabeverzögerung"></a>
  <br><br>
  <img src="https://img.shields.io/badge/1080p-60_FPS-2da44e?style=flat-square" alt="1080p 60 FPS">
  <img src="https://img.shields.io/badge/Tailscale-P2P-242424?style=flat-square&logo=tailscale&logoColor=white" alt="Tailscale P2P">
  <img src="https://img.shields.io/badge/Audio-sound_%2B_mic-007ec6?style=flat-square" alt="Audio: Ton und Mikrofon">
  <img src="https://img.shields.io/badge/FEC-packet_loss_recovery-f59e0b?style=flat-square" alt="FEC: Vorwärtsfehlerkorrektur">
</div>

Ein 2-minütiges Video: ein Sipeed NanoKVM mit starker Verzögerung in einem Heimlabor wird nach dem Flashen der USBridge-Firmware zu einem 60 FPS KVM über IP mit fast keiner Eingabeverzögerung, schnell genug, um Spiele mit einem Gamepad zu spielen. Tailscale funktioniert auf dem Board selbst, sodass es von überall ohne Portweiterleitung erreichbar ist; der Ton spielt in beide Richtungen, und FEC (Vorwärtsfehlerkorrektur) stellt verlorene Pakete wieder her, sodass das Bild über Wi-Fi und das Internet sauber bleibt. Firmware für das NanoKVM, Radxa Zero 3W und Radxa Cubie A7Z ist auf der [Hardware-Agent-Seite](https://www.usbridge.io/hardware-agent/) verfügbar.

## FAQ

<details>
<summary><b>Funktioniert USBridge mit Moonlight und Sunshine?</b></summary>

Ja. Der Agent liefert den Open-Source Sunshine-Streamer, sodass der Standard-Moonlight-Client sich damit verbindet. Vibepollo (ein Sunshine-Fork mit Apollo integriert) und punktfunk sind ebenfalls integriert und wechseln mit einem Klick.
</details>

<details>
<summary><b>Gibt es einen Hardware-Moonlight-Streamer?</b></summary>

Ja. Ein USBridge KVM 2.0 oder ein Radxa Zero 3W/3E, Radxa Cubie A7Z oder Sipeed NanoKVM, das mit USBridge-Firmware geflasht wurde, ist ein Sunshine-kompatibler GameStream-Host für alles, was an seinem HDMI-Eingang angeschlossen ist: ein PC, PS5, Xbox oder Switch. Es streamt 1080p 60 FPS mit Audio zum Standard-Moonlight-Client, ohne Software auf der Quelle. Siehe [Hardware Moonlight Streamer](https://www.usbridge.io/moonlight-hardware-streamer/).
</details>

<details>
<summary><b>Welcher Streamer ist am schnellsten auf meinem PC?</b></summary>

Das hängt von der GPU, dem Treiber und dem Netzwerk ab. Der integrierte Benchmark führt Sunshine, RustShine, Vibepollo und punktfunk auf deiner eigenen Hardware und deinem Netzwerk aus und vergleicht sie. Siehe [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/).
</details>

<details>
<summary><b>Welche GPUs werden für die Codierung unterstützt?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV und VAAPI unter Linux. Virtuelle Maschinen und PCs ohne GPU-Codierer verwenden Software H.264 (OpenH264).
</details>

<details>
<summary><b>Was ist kostenlos und was benötigt Pro?</b></summary>

4:4:4 Farben, HDR10, PyroWave, Gamepads, Mikrofon, MIDI und rohes USB-Passthrough sind kostenlos. Wacom-Tablets mit Druck und Neigung sowie die Hardware-KVM-Firmware (19 $ lebenslang pro Gerät, 24 h kostenlose Bewertung) sind Pro.
</details>

<details>
<summary><b>Kann ich ein Sipeed NanoKVM oder ein Radxa-Board in ein IP-KVM verwandeln?</b></summary>

Ja. Flash die USBridge-Firmware von der [Hardware-Agent-Seite](https://www.usbridge.io/hardware-agent/). Du erhältst BIOS/UEFI-Zugriff, ISO- und IMG-virtuelle Medien, unveränderbare Btrfs-Snapshots, 60 FPS Video, Audio und USB-Passthrough. Radxa-Boards benötigen einen HDMI-zu-USB-Capture-Dongle.
</details>

<details>
<summary><b>Brauche ich Portweiterleitung?</b></summary>

Nein. Verbindungen erfolgen über Tailscale Peer-to-Peer mit automatischer NAT-Traversierung.
</details>

<details>
<summary><b>Kann ich den Zugriff auf einen Computer mit jemand anderem teilen?</b></summary>

Ja. Lade Personen per E-Mail mit einer Rolle und einem Enddatum ein; Gäste treten über USBridge bei und benötigen kein Tailscale-Konto.
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
      Dieses Projekt ist unter der <b>GPLv3</b> lizenziert. Der plattformübergreifende Client enthält Code aus <code>moonlight-common-c</code> (auch GPLv3).<br><br>
      <a href="LICENSE">Lizenzdatei anzeigen</a>
    </td>
  </tr>
</table>