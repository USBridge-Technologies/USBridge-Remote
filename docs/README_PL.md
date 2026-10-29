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

**USBridge Remote** to ekosystem **zdalnego pulpitu** o niskim opóźnieniu i **KVM-over-IP**: jeden klient dla Windows, macOS, Linux, Android, iOS oraz przeglądarki, agent hosta z wbudowanym streamerem gier **Sunshine / Moonlight** oraz oprogramowaniem IP-KVM dla płyt **Radxa Zero 3W/3E**, **Radxa Cubie A7Z** i **Sipeed NanoKVM**. Łączy **dostęp do BIOS-u na poziomie sprzętowym** oraz **zdalny pulpit** w jednym interfejsie, z przesyłaniem USB dla gamepadów, tabletów Wacom, MIDI i mikrofonów oraz siecią P2P Tailscale bez przekierowywania portów.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote features: remote desktop, game streaming, USB passthrough, hardware KVM">
</div>


## Pobierz

**Instalator all-in-one (Klient + Agent, wybierz jeden lub oba podczas instalacji):** [Windows Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) · [obraz dysku macOS (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-macOS-arm64.dmg)

Wszystkie pliki są w [najnowszym wydaniu](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

### Klient
Klient to interfejs sterujący — zainstalowany na Twoim komputerze stacjonarnym lub laptopie (lub uruchomiony bezpośrednio w przeglądarce). Zarządza połączeniami, zdalnym pulpitem na żywo, przesyłaniem wirtualnych urządzeń oraz rejestrem zrzutów.

| Platforma | Instalator | Portable | Pakiety / Sklep |
| :--- | :--- | :--- | :--- |
| **Windows** x86_64 | [Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) | [Portable (.zip)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — |
| **macOS** Apple Silicon | [Obraz dysku (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | — |
| **Linux** x86_64 | [Debian / Ubuntu (.deb)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.deb) · [Fedora / RHEL (.rpm)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.rpm) · [Arch (.pkg.tar.zst)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.pkg.tar.zst) | [AppImage](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | [APT · DNF · pacman repository](#install-from-the-terminal) · [Snap Store](https://snapcraft.io/usbridge-client) |
| **Android** ARM64 | [APK (samouaktualizujący)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) |
| **iOS / iPadOS** | — | — | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |
| **Przeglądarka internetowa** | — | [Otwórz web.usbridge.io](https://web.usbridge.io) | — |

### Instalacja z terminala

Jedno polecenie instaluje Klienta i Agenta. Na Linuxie wybiera natywny pakiet dla Twojej dystrybucji (apt na Debianie/Ubuntu, dnf na Fedora/RHEL, pacman na Archu), a pakiet dodaje podpisane repozytorium USBridge, więc aplikacje aktualizują się razem z resztą systemu:

```sh
# Linux i macOS
curl -fsSL https://www.usbridge.io/install.sh | sh
# Tylko Agent (maszyna, do której masz dostęp tylko zdalnie)
curl -fsSL https://www.usbridge.io/install.sh | sh -s -- --agent
```

```powershell
# Windows (PowerShell)
irm https://www.usbridge.io/install.ps1 | iex
```

```sh
# Klient z Snap Store (Ubuntu i Arch z snapd z AUR)
sudo snap install usbridge-client --edge
```

<details>
<summary><b>Dodaj repozytorium APT ręcznie (Debian / Ubuntu)</b></summary>

```sh
curl -fsSL https://raw.githubusercontent.com/USBridge-Technologies/USBridge-Remote/main/scripts/deb/usbridge-archive-keyring.gpg \
  | sudo tee /usr/share/keyrings/usbridge-archive-keyring.gpg >/dev/null
echo "deb [arch=amd64 signed-by=/usr/share/keyrings/usbridge-archive-keyring.gpg] https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download ./" \
  | sudo tee /etc/apt/sources.list.d/usbridge.list
sudo apt update && sudo apt install usbridge-client usbridge-agent
```

Pobrany plik `.deb`, `.rpm` lub pakiet Arch ustawia to samo repozytorium podczas instalacji.
</details>

🌐 **Klient internetowy bez instalacji**: Nie wymaga instalacji. Po prostu otwórz [web.usbridge.io](https://web.usbridge.io), aby połączyć się natychmiast. *(Uwaga: Klient internetowy działa z pewnymi ograniczeniami funkcji i wydajności z powodu zabezpieczeń przeglądarki i ograniczeń WebRTC. Aby uzyskać pełne, nieograniczone doświadczenie, użyj aplikacji natywnych).* Klient internetowy łączy się z Agentem przez jego zaufaną nazwę `<label>.device.usbridge.io`: Agent otrzymuje ten certyfikat za pierwszym razem, gdy przeglądarka się z nim łączy, i tylko podczas logowania do swojego konta USBridge w Agencie — więc pierwsza próba może zająć kilka minut. Zobacz wiersz **Status → Certyfikat** Agenta lub [dokumentację Agenta](agent/docs/README.md#platform-notes-from-the-top-level-readme) po szczegóły. USBridge KVM również obsługuje sam klient internetowy pod `https://<kvm>:9443/` i przechodzi do swojej własnej zaufanej nazwy, gdy to możliwe.

## Agent

Agent działa na docelowej maszynie — serwerze lub komputerze, do którego chcesz uzyskać dostęp zdalnie. Przechwytuje ekran i dźwięk, przesyła Twój klawiaturę, mysz i gamepad do tej maszyny oraz łączy ją przez Tailscale.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Platforma</th>
          <th>Pobrania</th>
        </tr>
        <tr>
          <td><b>Windows</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64-Setup.exe">Setup (.exe)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Portable (.zip)</a></td>
        </tr>
        <tr>
          <td><b>macOS</b><br>Apple Silicon</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Obraz dysku (.dmg)</a></td>
        </tr>
        <tr>
          <td><b>Linux</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.deb">Debian / Ubuntu (.deb)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.rpm">Fedora / RHEL (.rpm)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.pkg.tar.zst">Arch (.pkg.tar.zst)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">AppImage (portable)</a></td>
        </tr>
      </table>
    </td>
    <!-- Right Column: Image -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="USBridge Agent window: connection token, Tailscale address and stream status" width="100%">
    </td>
  </tr>
</table>

## Funkcje

**Sprzętowe kodowanie przyspieszone:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · plus oprogramowanie **H.264 (OpenH264)** dla VM i maszyn bez kodera GPU

<table>
  <tr>
    <td width="33%" valign="top">
      <code>BASIC FUNCTIONALITY</code><br><br>
      <b>SUNSHINE OPEN-SOURCE</b><br>
      <i>Standardowe przesyłanie gier</i><br><br>
      ✓ Wbudowany open-source streamer Sunshine<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">Alternatywne streamery</a>: Vibepollo (fork Sunshine z wbudowanym Apollon, Windows) i punktfunk (kompatybilny z Moonlight, Windows i Linux), przełączaj w jednym kliknięciu<br>
      ✓ Wbudowany benchmark do porównania Sunshine, RustShine, Vibepollo i punktfunk na własnym sprzęcie i sieci<br>
      ✓ Niskolatencyjny dostęp do pulpitu i gier<br>
      ✓ Dwukierunkowy schowek plików i tekstów<br>    
      ✓ Przełączanie wyświetlaczy na wielu monitorach<br>
      ✓ Zintegrowana sieć P2P Tailscale<br>
      ✓ Dostęp gościa: zapraszaj ludzi e-mailem z rolą i datą końcową<br>
      ✓ Natywne wsparcie Wayland (przechwytywanie bez komunikatów)
    </td>
    <td width="33%" valign="top">
      <code>+ BASIC FEATURES</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Silnik przesyłania oparty na Rust</i><br><br>
      ✓ Natychmiastowe połączenie z niestandardowym protokołem zdalnym<br>
      ✓ Surowe <a href="https://www.usbridge.io/usb-passthrough/">przesyłanie urządzeń peryferyjnych USB</a><br>
      ✓ Adaptacyjne przesyłanie zoptymalizowane pod kątem stabilności Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Bezstratna dokładność kolorów 4:4:4<br>
      ✓ Przesyłanie mikrofonu (klient → host)<br>
      ✓ Przesyłanie urządzeń MIDI (klient → host)<br>
      ✓ Kodowanie GPU: NVENC, AMF, QSV, VAAPI<br>
      ✓ Oprogramowanie H.264 (OpenH264) dla VM bez GPU<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> niskolatencyjny kodek falowy (od Valve)<br>
      ✓ Zarządzanie wirtualnym wyświetlaczem bez głowy<br>
      ✓ Niskolatencyjne przesyłanie kontrolera gamepada<br>
      ✓ Dostęp do klienta przeglądarki internetowej (nie wymaga instalacji)<br>
      ✓ Dostęp przed logowaniem w Windows (bezpieczne wprowadzanie poświadczeń)
    </td>
    <td width="33%" valign="top">
      <code>+ BASIC FEATURES</code> <code>+ FREE FEATURES</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Profesjonalne przepływy pracy</i><br><br>
      ✓ Wsparcie dla tabletów Wacom z ciśnieniem i nachyleniem (<a href="https://www.usbridge.io/macos-wacom-support/">macOS również</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">Oprogramowanie KVM sprzętowe</a> dla Radxa Zero 3W/3E, Radxa Cubie A7Z i Sipeed NanoKVM: dostęp do BIOS/UEFI, ISO wirtualne media, zrzuty Btrfs (19 USD na urządzenie na całe życie, 24 h bezpłatnej oceny)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">Sprzętowy streamer Moonlight</a> na tych płytach: każde źródło HDMI (PC, PS5, Xbox, Switch) do Moonlight w 1080p 60 FPS, bez oprogramowania na źródle<br><br>
    </td>
  </tr>
</table>

## Szybki start

1. **Zainstaluj Agenta** na maszynie, do której chcesz uzyskać dostęp zdalnie. Uruchom go — wyświetli token połączenia i adres Tailscale. Połącz Tailscale, jeśli potrzebujesz dostępu przez internet.

2. **Zainstaluj Klienta** na swoim komputerze stacjonarnym, laptopie lub telefonie.

3. **Dodaj połączenie** — wprowadź adres IP lub adres Tailscale wyświetlony w oknie Agenta. To wszystko.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote quick start: install the agent, install the client, connect">
</div>

## Integracja sprzętowa

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Kontrola BIOS na poziomie sprzętowym</b><br>
      USBridge Remote integruje się natywnie z urządzeniem USBridge-KVM 2.0 do zarządzania zdalnego, bare-metal przed uruchomieniem systemu operacyjnego. Jest również pierwszym sprzętowym streamerem Moonlight: standardowy klient Moonlight łączy się z KVM i przesyła każde źródło HDMI (PC lub konsola do gier) bez oprogramowania na nim.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Kup USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>Oprogramowanie IP-KVM DIY</b><br>
      Zainstaluj oficjalne oprogramowanie na kompatybilnej płycie (Radxa Zero 3W/3E, Radxa Cubie A7Z) z donglem do przechwytywania HDMI-USB, aby zbudować własne IP-KVM. Na <b>Sipeed NanoKVM</b> zastępuje standardowe oprogramowanie: płynne 60 FPS zamiast zacinającego obrazu, dźwięku i mikrofonu, gamepadów, przesyłania USB i wsparcia Moonlight (<a href="https://www.usbridge.io/hardware-agent/">pobierz oprogramowanie</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Pobierz oprogramowanie"></a>
    </td>
    <td width="33%" valign="top">
      <b>Dongle sprzętowy USB/IP (ESP32-S3)</b><br>
      Oprogramowanie przesyłania USB napotyka ograniczenia na macOS: nie ma sterownika VHCI, do którego można się podłączyć, więc zdalny tablet Wacom (lub inne urządzenie USB) nie może być przedstawiony jako prawdziwy sprzęt. Ten dongle podłącza się do Maca i wykonuje w sprzęcie to, co <code>vhci-hcd</code> robi w oprogramowaniu gdzie indziej — eksportowane urządzenie jest enumerowane z własnym VID/PID, związanym z własnym sterownikiem macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="oprogramowanie i dokumentacja esp32-acm"></a>
    </td>
  </tr>
</table>

### NanoKVM z oprogramowaniem USBridge: 60 FPS zamiast opóźnienia

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Wideo: Sipeed NanoKVM z oprogramowaniem USBridge przesyła w 60 FPS z prawie żadnym opóźnieniem"></a>
  <br><br>
  <img src="https://img.shields.io/badge/1080p-60_FPS-2da44e?style=flat-square" alt="1080p 60 FPS">
  <img src="https://img.shields.io/badge/Tailscale-P2P-242424?style=flat-square&logo=tailscale&logoColor=white" alt="Tailscale P2P">
  <img src="https://img.shields.io/badge/Audio-sound_%2B_mic-007ec6?style=flat-square" alt="Audio: dźwięk i mikrofon">
  <img src="https://img.shields.io/badge/FEC-packet_loss_recovery-f59e0b?style=flat-square" alt="FEC: korekcja błędów w przód">
</div>

2-minutowe wideo: Sipeed NanoKVM z dużym opóźnieniem w domowym laboratorium staje się KVM 60 FPS przez IP z prawie żadnym opóźnieniem po zainstalowaniu oprogramowania USBridge, wystarczająco szybkim, aby grać w gry z gamepadem. Tailscale działa na samej płycie, więc jest osiągalny z dowolnego miejsca bez przekierowywania portów; dźwięk działa w obie strony, a FEC (korekcja błędów w przód) odbudowuje utracone pakiety, więc obraz pozostaje czysty na Wi-Fi i przez internet. Oprogramowanie dla NanoKVM, Radxa Zero 3W i Radxa Cubie A7Z znajduje się na [stronie Agenta Sprzętowego](https://www.usbridge.io/hardware-agent/).

## FAQ

<details>
<summary><b>Czy USBridge działa z Moonlight i Sunshine?</b></summary>

Tak. Agent dostarcza open-source streamer Sunshine, więc standardowy klient Moonlight łączy się z nim. Vibepollo (fork Sunshine z wbudowanym Apollonem) i punktfunk są również wbudowane i przełączają się w jednym kliknięciu.
</details>

<details>
<summary><b>Czy istnieje sprzętowy streamer Moonlight?</b></summary>

Tak. USBridge KVM 2.0, lub Radxa Zero 3W/3E, Radxa Cubie A7Z lub Sipeed NanoKVM z zainstalowanym oprogramowaniem USBridge, to zgodny z Sunshine host GameStream dla czegokolwiek, co jest na jego wejściu HDMI: PC, PS5, Xbox lub Switch. Przesyła 1080p 60 FPS z dźwiękiem do standardowego klienta Moonlight, bez oprogramowania na źródle. Zobacz [Sprzętowy streamer Moonlight](https://www.usbridge.io/moonlight-hardware-streamer/).
</details>

<details>
<summary><b>Który streamer jest najszybszy na moim PC?</b></summary>

To zależy od GPU, sterownika i sieci. Wbudowany benchmark uruchamia Sunshine, RustShine, Vibepollo i punktfunk na Twoim własnym sprzęcie i sieci i porównuje je. Zobacz [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/).
</details>

<details>
<summary><b>Jakie GPU są wspierane do kodowania?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV i VAAPI na Linuxie. Wirtualne maszyny i komputery bez kodera GPU używają oprogramowania H.264 (OpenH264).
</details>

<details>
<summary><b>Co jest darmowe, a co wymaga Pro?</b></summary>

Kolor 4:4:4, HDR10, PyroWave, gamepady, mikrofon, MIDI i surowe przesyłanie USB są darmowe. Tablety Wacom z ciśnieniem i nachyleniem oraz oprogramowanie KVM sprzętowe (19 USD na urządzenie na całe życie, 24 h bezpłatnej oceny) są Pro.
</details>

<details>
<summary><b>Czy mogę przekształcić Sipeed NanoKVM lub płytę Radxa w IP-KVM?</b></summary>

Tak. Zainstaluj oprogramowanie USBridge z [strony Agenta Sprzętowego](https://www.usbridge.io/hardware-agent/). Uzyskujesz dostęp do BIOS/UEFI, wirtualne media ISO i IMG, niezmienne zrzuty Btrfs, wideo 60 FPS, dźwięk i przesyłanie USB. Płyty Radxa potrzebują dongla do przechwytywania HDMI-USB.
</details>

<details>
<summary><b>Czy potrzebuję przekierowywania portów?</b></summary>

Nie. Połączenia odbywają się przez Tailscale peer-to-peer, z automatycznym przechodzeniem przez NAT.
</details>

<details>
<summary><b>Czy mogę dzielić dostęp do komputera z kimś innym?</b></summary>

Tak. Zapraszaj ludzi e-mailem z rolą i datą końcową; goście dołączają przez USBridge i nie potrzebują konta Tailscale.
</details>

## Zasoby i Licencja

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Rozwój i plan działania</b><br>
      Utrzymuję publiczny pulpit nawigacyjny do śledzenia wszystkich planowanych funkcji, aktualizacji architektonicznych i harmonogramów wydania.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Zobacz na żywo plan działania</a>
    </td>
    <td width="33%" valign="top">
      <b>Linki do projektu</b><br>
      <ul>
        <li><a href="https://usbridge.io">Oficjalna strona</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Beta Testing & Bugs)</a></li>
        <li><a href="https://www.reddit.com/r/USBridge/">Reddit</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">YouTube</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Wsparcie Patreon</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Licencja (GPLv3)</b><br>
      Ten projekt jest licencjonowany na podstawie <b>GPLv3</b>. Klient wieloplatformowy zawiera kod z <code>moonlight-common-c</code> (również GPLv3).<br><br>
      <a href="LICENSE">Zobacz plik LICENCJA</a>
    </td>
  </tr>
</table>