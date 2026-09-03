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

**USBridge Remote** to zjednoczony, wysokowydajny klient do zarządzania zdalnymi maszynami. Został zaprojektowany, aby połączyć **dostęp do BIOS-u na poziomie sprzętowym** (za pomocą urządzeń USBridge KVM) i **zdalny pulpit oparty na oprogramowaniu** w jednym, uproszczonym interfejsie.

 🖥️ **Potrzebujesz kontroli BIOS-u na poziomie sprzętowym przed uruchomieniem systemu operacyjnego?**  
 USBridge Remote integruje się natywnie z **USBridge-KVM 2.0** w celu zarządzania poza pasmem, na poziomie metalu. 

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)

> 
🔥 **Zbuduj własny IP-KVM!**
Oficjalnie wydałem oprogramowanie układowe do samodzielnego montażu. Możesz teraz przekształcić swoją własną kompatybilną SBC (taką jak Radxa Zero 3W/3E lub Cubie A7) oraz kartę przechwytującą USB w pełnoprawne urządzenie USBridge KVM 2.0.

[![Get the Firmware](https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge)](https://www.usbridge.io/kvm-software)

> ⚠️ **Oprogramowanie beta** — To wczesna wersja. Spodziewaj się błędów. Proszę zgłaszać problemy za pośrednictwem [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues) lub dołącz do naszego [Discord](https://discord.com/invite/xqQ6ybkfWS) w celu uzyskania wsparcia.

---

## Pobierz

### Klient
Klient to interfejs sterujący — zainstalowany na twoim komputerze stacjonarnym lub laptopie (lub uruchamiany bezpośrednio w przeglądarce). Zarządza połączeniami, zdalnym pulpitem na żywo, przejściem wirtualnych urządzeń i rejestrem migawkowym.

| Architektura | Windows | macOS | Linux | Android | iOS | Przeglądarka internetowa |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Otwórz aplikację](https://web.usbridge.io) |
| **ARM64** | — | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Otwórz aplikację](https://web.usbridge.io) |

Preferujesz bezpośrednie APK bez konta w sklepie Play? Samoaktualizująca wersja jest również publikowana w [najowszym wydaniu](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Klient internetowy bez instalacji**: Nie wymaga instalacji. Po prostu otwórz [web.usbridge.io](https://web.usbridge.io), aby połączyć się natychmiast. *(Uwaga: Klient internetowy działa z pewnymi ograniczeniami funkcji i wydajności z powodu zabezpieczeń przeglądarki i ograniczeń WebRTC. Aby uzyskać pełne, nieograniczone doświadczenie, użyj aplikacji natywnych).*

## Agent

Agent działa na docelowej maszynie — serwerze lub komputerze, do którego chcesz uzyskać zdalny dostęp. Obsługuje przechwytywanie ekranu, wstrzykiwanie wejścia i sieciowanie Tailscale.

| Architektura | Windows | macOS | Linux |
| :--- | :---: | :---: | :---: |
| **x86_64** | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip) | — | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage) |
| **ARM64** | — | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg) | — |

---

## Demo

<div align="center">
  <a href="https://youtu.be/1pV9PJeBr7M">
    <img src="https://img.youtube.com/vi/1pV9PJeBr7M/maxresdefault.jpg" alt="USBridge Remote Demo" style="max-width: 100%; border-radius: 8px;">
  </a>
</div>

---

## Funkcje

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**Jedno miejsce na wszystko** — Zjednoczyłem workflow. Zarządzaj sprzętem USBridge KVM i agentami oprogramowania z jednego pulpitu. Dodaj maszynę, połącz się i już jesteś w środku.

**Brak ograniczeń, brak subskrypcji** — Całkowicie za darmo. Brak limitów czasu sesji, brak ograniczeń połączeń i brak konta wymagane na docelowej maszynie.

**Wideo o niskiej latencji i integracja z Moonlight** — Ciesz się rozdzielczością do 2K z płynnością 120 FPS i zerowym opóźnieniem. Mój adaptacyjny silnik strumieniowy wykorzystuje natywną integrację z Moonlight, aby zapewnić niezrównaną wydajność zdalnego pulpitu o ultra-niskiej latencji.

**Integracja z Tailscale** — Wbudowane szyfrowane tunelowanie P2P. Połącz się z dowolną maszyną na całym świecie bez zabawy w przekierowywanie portów czy zasady zapory. Działa automatycznie w LAN i przez internet.

**Wspólny schowek** — Kopiuj i wklej bezproblemowo między swoją lokalną maszyną a zdalnymi celami. W pełni obsługuje tekst, obrazy i transfery plików od razu po wyjęciu z pudełka.

**Wsparcie dla wielu monitorów** — Dodałem możliwość przełączania się między wieloma wyświetlaczami. Jeśli docelowa maszyna ma kilka monitorów, możesz teraz łatwo wybrać, który z nich chcesz oglądać bezpośrednio z ustawień połączenia. 

<img width="2080" height="1170" alt="Screenshot 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />


---

## Wsparcie dla Wayland (bez komunikatów)

Większość agentów zdalnego pulpitu na Linuxie ma problemy z Waylandem lub ciągle bombarduje cię komunikatami o pozwolenie i potwierdzeniami za każdym razem, gdy sesja się rozpoczyna. 

Zaprojektowałem Agenta USBridge, aby natywnie wspierał Wayland. Obsługuje pełne przechwytywanie ekranu i wstrzykiwanie wejścia od razu po wyjęciu z pudełka **bez irytujących komunikatów o pozwolenie** lub ręcznych potwierdzeń. Po prostu działa.

---

## Szybki start

1. **Zainstaluj Agenta** na maszynie, do której chcesz uzyskać zdalny dostęp. Uruchom go — wyświetli token połączenia i adres Tailscale. Połącz Tailscale, jeśli potrzebujesz dostępu przez internet.

2. **Zainstaluj Klienta** na swoim komputerze stacjonarnym, laptopie lub telefonie.

3. **Dodaj połączenie** — wprowadź adres IP lub adres Tailscale wyświetlony w oknie Agenta. To wszystko.

---

##  Plan projektu

Zarządzam planami rozwoju oprogramowania i nadchodzącymi funkcjami w otwartym pulpicie. Jeśli chcesz zobaczyć, co jest obecnie rozwijane, co jest planowane lub śledzić status nadchodzących funkcji, sprawdź na żywo plan:

 **[Zobacz plan USBridge Remote](https://github.com/orgs/USBridge-Technologies/projects/3)**

---

## Społeczność i testowanie beta

Dołącz do naszego Discord, aby uzyskać rolę **Beta Testera**, zgłaszać błędy i pomóc mi kształtować plan:

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## Linki

- 🌐 [Oficjalna strona](https://usbridge.io)
- ❤️ [Strona Patreon](https://www.patreon.com/USBridge_Technologies)
- 🛒 [USBridge KVM 2.0 na Crowd Supply](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)

---

## 📜 Licencja

Ten projekt jest licencjonowany na podstawie **GPLv3** (zobacz [`LICENSE`](LICENSE)). Klient Android/Windows/macOS/Linux zawiera kod z `moonlight-common-c` (również GPLv3).