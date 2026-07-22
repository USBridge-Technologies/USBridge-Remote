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

**USBridge Remote** ist ein einheitlicher Hochleistungs-Client zur Verwaltung von Remote-Maschinen. Ich habe es entwickelt, um **Hardware-Level BIOS-Zugriff** (über USBridge KVM-Geräte) und **softwarebasierten Remote-Desktop** in einer einzigen, optimierten Benutzeroberfläche zu kombinieren.

 🖥️ **Benötigen Sie Hardware-Level BIOS-Kontrolle, bevor das Betriebssystem bootet?**  
 USBridge Remote integriert sich nativ mit **USBridge-KVM 2.0** für Out-of-Band-Management auf Metal-Ebene.

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)


> ⚠️ **Beta-Software** — Dies ist eine frühe Version. Erwarten Sie Fehler. Bitte melden Sie Probleme über [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues) oder treten Sie unserem [Discord](https://discord.com/invite/xqQ6ybkfWS) für Unterstützung bei.
> 
> ℹ️ **Hinweis zu Windows Defender / Antivirus-Falschmeldungen:**  
> Windows Defender kann `libva.dll` fälschlicherweise als Bedrohung (`Trojan:Win32/Wacatac.B!ml`) kennzeichnen, aufgrund von Heuristik-/Maschinenlern-Erkennung bei nicht signierten Binärdateien. **Dies ist eine Falschmeldung.**  
> Wir haben die Datei zur offiziellen Überprüfung und Whitelistung an Microsoft Security Intelligence übermittelt. In der Zwischenzeit, wenn Ihr Antivirus `libva.dll` entfernt, stellen Sie es bitte aus der Quarantäne wieder her oder fügen Sie den USBridge-Ordner zur Ausschlussliste Ihres Antivirus hinzu.

---

## Download

### Client
Der Client ist die Steueroberfläche — installiert auf Ihrem Arbeitsplatz oder Laptop. Er verwaltet Verbindungen, Live-Remote-Desktop, virtuelle Geräte-Passthrough und Snapshot-Registry.

| Architektur | Windows | macOS | Linux | Android | iOS |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64.apk) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |

## Agent

Der Agent läuft auf der Zielmaschine — dem Server oder PC, auf den Sie remote zugreifen möchten. Er kümmert sich um Bildschirmaufnahme, Eingabeinjektion und Tailscale-Netzwerk.

| Architektur | Windows | macOS | Linux |
| :--- | :---: | :---: | :---: |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg) | — |

---

## Demo

<div align="center">
  <a href="https://youtu.be/1pV9PJeBr7M">
    <img src="https://img.youtube.com/vi/1pV9PJeBr7M/maxresdefault.jpg" alt="USBridge Remote Demo" style="max-width: 100%; border-radius: 8px;">
  </a>
</div>

---

## In den Medien

| Medien | Höhepunkt | Link |
| :--- | :--- | :---: |
| **IlSoftware.it** (Italien) | *"USBridge Remote fordert RustDesk und AnyDesk heraus..."* — Unabhängige tiefgehende Bewertung, die den Software-Agenten & die Hardware-KVM-Synergie, native Wayland-Unterstützung und P2P-Architektur lobt. | [Artikel lesen](https://www.ilsoftware.it/alternativa-rustdesk-anydesk-usbridge/) |

---

## Funktionen

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**Ein Ort für alles** — Ich habe den Workflow vereinheitlicht. Verwalten Sie USBridge KVM-Hardware und Software-Agenten von einem einzigen Dashboard aus. Fügen Sie eine Maschine hinzu, verbinden Sie sich, und Sie sind drin.

**Keine Grenzen, keine Abonnements** — Vollständig kostenlos. Keine Sitzungszeitlimits, keine Verbindungsobergrenzen und kein Konto erforderlich auf der Zielmaschine.

**Niedriglatente Videos & Moonlight-Integration** — Genießen Sie bis zu 2K Auflösung mit butterweichen 240 FPS und null wahrnehmbarer Verzögerung. Mein adaptiver Streaming-Engine nutzt die native Moonlight-Integration, um unübertroffene, ultra-niedriglatente Remote-Desktop-Leistung zu bieten.

**Tailscale-Integration** — Eingebautes verschlüsseltes P2P-Tunneling. Verbinden Sie sich global mit jeder Maschine, ohne sich um Portweiterleitungen oder Firewall-Regeln kümmern zu müssen. Es funktioniert automatisch im LAN und über das Internet.

**Geteilte Zwischenablage** — Kopieren und Einfügen nahtlos zwischen Ihrem lokalen Gerät und den Remote-Zielen. Es unterstützt vollständig Text, Bilder und Dateiübertragungen sofort.

**Multi-Monitor-Unterstützung** — Ich habe die Möglichkeit hinzugefügt, zwischen mehreren Displays zu wechseln. Wenn die Zielmaschine mehrere Monitore hat, können Sie jetzt einfach auswählen, welchen Sie direkt aus den Verbindungseinstellungen anzeigen möchten. 
> **Hinweis:** Nach der Auswahl eines anderen Monitors müssen Sie die Sitzung erneut verbinden, um den Video-Stream neu zu initialisieren und den neuen Bildschirm anzuzeigen.

<img width="2080" height="1170" alt="Screenshot 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />


---

## Wayland-Unterstützung (Keine Aufforderungen)

Die meisten Remote-Desktop-Agenten unter Linux haben Probleme mit Wayland oder spammen Sie ständig mit Berechtigungsaufforderungen und Bestätigungs-Popups, jedes Mal wenn eine Sitzung startet. 

Ich habe den USBridge-Agenten so konzipiert, dass er Wayland nativ unterstützt. Er verarbeitet die vollständige Bildschirmaufnahme und Eingabeinjektion sofort **ohne lästige Berechtigungsaufforderungen** oder manuelle Bestätigungen. Es funktioniert einfach.

---

## Schnellstart

1. **Installieren Sie den Agenten** auf der Maschine, auf die Sie remote zugreifen möchten. Starten Sie ihn — er zeigt ein Verbindungstoken und eine Tailscale-Adresse an. Verbinden Sie Tailscale, wenn Sie über das Internet zugreifen müssen.

2. **Installieren Sie den Client** auf Ihrem Arbeitsplatz, Laptop oder Telefon.

3. **Fügen Sie eine Verbindung hinzu** — geben Sie die IP oder die Tailscale-Adresse ein, die im Agentenfenster angezeigt wird. Das ist alles.

---

## Projekt-Roadmap

Ich verwalte die Softwareentwicklungspläne und bevorstehenden Funktionen in einem offenen Dashboard. Wenn Sie sehen möchten, was derzeit entwickelt wird, was geplant ist oder den Status bevorstehender Funktionen verfolgen möchten, schauen Sie sich die Live-Roadmap an:

 **[USBridge Remote Roadmap anzeigen](https://github.com/orgs/USBridge-Technologies/projects/3)**

---

## Community & Beta-Testing

Treten Sie unserem Discord bei, um die **Beta-Tester**-Rolle zu erhalten, Fehler zu melden und mir zu helfen, die Roadmap zu gestalten:

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## Links

- 🌐 [Offizielle Website](https://usbridge.io)
- ❤️ [Patreon-Seite](https://www.patreon.com/USBridge_Technologies)
- 🛒 [USBridge KVM 2.0 auf Crowd Supply](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)