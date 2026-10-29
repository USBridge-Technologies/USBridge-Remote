# USBridge Remote

<div align="center">
  <img src="./assets/banner6.svg" width="1400" alt="USBridge Remote: niskolatencyjny zdalny pulpit i KVM-over-IP">
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

**USBridge Remote** to niskolatencyjny **zdalny pulpit** i **KVM-over-IP** ekosystem: jeden klient dla Windows, macOS, Linux, Android, iOS oraz przeglądarki, agent hosta z wbudowanym streamerem gier **Sunshine / Moonlight**, oraz firmware IP-KVM dla płyt **Radxa Zero 3W**, **Radxa Cubie A7Z** i **Sipeed NanoKVM**. Łączy **dostęp do BIOS-u na poziomie sprzętowym** i **zdalny pulpit** w jednym interfejsie, z przejściem USB dla gamepadów, tabletów Wacom, MIDI i mikrofonów oraz siecią P2P Tailscale bez przekierowywania portów.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="Funkcje USBridge Remote: zdalny pulpit, strumieniowanie gier, przejście USB, sprzętowy KVM">
</div>


## Pobierz

**Instalator all-in-one (Klient + Agent, wybierz jeden lub oba podczas instalacji):** [Windows Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) · [obraz dysku macOS (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-macOS-arm64.dmg)

Pakiety Linux (.deb, .rpm, Arch) dodają repozytorium pakietów USBridge, dzięki czemu aplikacje aktualizują się wraz z resztą systemu. Wszystkie pliki są dostępne w [najnowszym wydaniu](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

### Klient
Klient to interfejs sterujący — zainstalowany na stacji roboczej lub laptopie (lub uruchomiony bezpośrednio w przeglądarce). Zarządza połączeniami, zdalnym pulpitem na żywo, przejściem wirtualnych urządzeń oraz rejestrem zrzutów.

| Platforma | Instalator | Portable | Pakiety / Sklep |
| :--- | :--- | :--- | :--- |
| **Windows** x86_64 | [Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) | [Portable (.zip)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — |
| **macOS** Apple Silicon | [Obraz dysku (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | — |
| **Linux** x86_64 | [Debian / Ubuntu (.deb)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.deb) | [AppImage](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | [Fedora / RHEL (.rpm)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.rpm) · [Arch (.pkg.tar.zst)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.pkg.tar.zst) |
| **Android** ARM64 | [APK (samouaktualizujący)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) |
| **iOS / iPadOS** | — | — | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |
| **Przeglądarka internetowa** | — | [Otwórz web.usbridge.io](https://web.usbridge.io) | — |

🌐 **Klient internetowy bez instalacji**: Nie wymaga instalacji. Po prostu otwórz [web.usbridge.io](https://web.usbridge.io), aby natychmiast się połączyć. *(Uwaga: Klient internetowy działa z pewnymi ograniczeniami funkcji i wydajności z powodu zabezpieczeń przeglądarki i ograniczeń WebRTC. Aby uzyskać pełne, nieograniczone doświadczenie, użyj aplikacji natywnych).* Klient internetowy łączy się z Agentem przez jego zaufaną nazwę `<label>.device.usbridge.io`: Agent otrzymuje ten certyfikat za pierwszym razem, gdy przeglądarka się z nim łączy, i tylko podczas logowania do swojego konta USBridge w Agencie — więc pierwsza próba może zająć kilka minut. Zobacz wiersz **Status → Certyfikat** Agenta lub [dokumentację Agenta](agent/docs/README.md#platform-notes-from-the-top-level-readme) po szczegóły. USBridge KVM również obsługuje sam klient internetowy pod adresem `https://<kvm>:9443/` i przechodzi na swoją własną zaufaną nazwę, gdy jest to możliwe.

## Agent

Agent działa na docelowej maszynie — serwerze lub komputerze, do którego chcesz uzyskać dostęp zdalnie. Przechwytuje ekran i dźwięk, przekazuje twoją klawiaturę, mysz i gamepad do tej maszyny oraz łączy ją przez Tailscale.

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
      <img src="./assets/agent-screenshot.svg" alt="Okno USBridge Agent: token połączenia, adres Tailscale i status strumienia" width="100%">
    </td>
  </tr>
</table>

## Funkcje

**Sprzętowe kodowanie przyspieszone:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · plus oprogramowanie **H.264 (OpenH264)** dla VM i maszyn bez kodera GPU

<table>
  <tr>
    <td width="33%" valign="top">
      <code>PODSTAWOWA FUNKCJONALNOŚĆ</code><br><br>
      <b>OTWARTY KOD SUNSHINE</b><br>
      <i>Standardowe strumieniowanie gier</i><br><br>
      ✓ Wbudowany open-source streamer Sunshine<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">Alternatywne streamery</a>: Vibepollo (fork Sunshine z Apollo wbudowanym, Windows) i punktfunk (kompatybilny z Moonlight, Windows i Linux), przełączanie jednym kliknięciem<br>
      ✓ Wbudowany benchmark do porównania Sunshine, RustShine, Vibepollo i punktfunk na własnym sprzęcie i sieci<br>
      ✓ Niskolatencyjny dostęp do pulpitu i gier<br>
      ✓ Dwukierunkowy schowek plików i tekstów<br>    
      ✓ Przełączanie wyświetlaczy wielomonitorowych<br>
      ✓ Zintegrowane P2P networking Tailscale<br>
      ✓ Dostęp gości: zapraszaj osoby przez e-mail z rolą i datą końcową<br>
      ✓ Natywne wsparcie Wayland (przechwytywanie bez komunikatów)
    </td>
    <td width="33%" valign="top">
      <code>+ PODSTAWOWE FUNKCJE</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Silnik strumieniowy oparty na Rust</i><br><br>
      ✓ Natychmiastowe połączenie za pomocą niestandardowego protokołu zdalnego<br>
      ✓ Surowe <a href="https://www.usbridge.io/usb-passthrough/">przejście urządzeń peryferyjnych USB</a><br>
      ✓ Adaptacyjne strumieniowanie zoptymalizowane pod kątem stabilności Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Bezstratna dokładność kolorów 4:4:4<br>
      ✓ Przejście mikrofonu (klient → host)<br>
      ✓ Przejście urządzenia MIDI (klient → host)<br>
      ✓ Kodowanie GPU: NVENC, AMF, QSV, VAAPI<br>
      ✓ Oprogramowanie H.264 (OpenH264) dla VM bez GPU<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> niskolatencyjny kodek falowy (od Valve)<br>
      ✓ Zarządzanie wirtualnym wyświetlaczem bez głowy<br>
      ✓ Niskolatencyjne przejście kontrolera gamepada<br>
      ✓ Dostęp do klienta przeglądarki internetowej (nie wymaga instalacji)<br>
      ✓ Dostęp przed logowaniem w Windows (bezpieczne wprowadzanie danych uwierzytelniających)
    </td>
    <td width="33%" valign="top">
      <code>+ PODSTAWOWE FUNKCJE</code> <code>+ BEZPŁATNE FUNKCJE</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Profesjonalne przepływy pracy</i><br><br>
      ✓ Wsparcie dla tabletów Wacom z ciśnieniem i nachyleniem (<a href="https://www.usbridge.io/macos-wacom-support/">macOS również</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">Sprzętowe firmware KVM</a> dla Radxa Zero 3W, Radxa Cubie A7Z i Sipeed NanoKVM: dostęp do BIOS/UEFI, ISO wirtualne media, zrzuty Btrfs (19 USD za urządzenie na całe życie, 24 h darmowa ocena)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">Sprzętowy streamer Moonlight</a> na tych płytach: każdy źródło HDMI (PC, PS5, Xbox, Switch) do Moonlight w 1080p 60 FPS, bez oprogramowania na źródle<br><br>
    </td>
  </tr>
</table>

## Szybki start

1. **Zainstaluj Agenta** na maszynie, do której chcesz uzyskać zdalny dostęp. Uruchom go — wyświetli token połączenia i adres Tailscale. Połącz Tailscale, jeśli potrzebujesz dostępu przez internet.

2. **Zainstaluj Klienta** na swojej stacji roboczej, laptopie lub telefonie.

3. **Dodaj połączenie** — wprowadź adres IP lub adres Tailscale pokazany w oknie Agenta. To wszystko.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="Szybki start USBridge Remote: zainstaluj agenta, zainstaluj klienta, połącz się">
</div>

## Integracja sprzętowa

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Kontrola BIOS na poziomie sprzętowym</b><br>
      USBridge Remote integruje się natywnie z urządzeniem USBridge-KVM 2.0 do zarządzania poza pasmem, na poziomie sprzętowym przed uruchomieniem systemu operacyjnego. Jest również pierwszym sprzętowym streamerem Moonlight: standardowy klient Moonlight łączy się z KVM i strumieniuje każde źródło HDMI (PC lub konsola do gier) bez oprogramowania na nim.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Kup USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY Firmware IP-KVM</b><br>
      Wgraj oficjalne firmware na kompatybilną płytę (Radxa Zero 3W, Radxa Cubie A7Z) z donglem do przechwytywania HDMI na USB, aby zbudować własny IP-KVM. Na <b>Sipeed NanoKVM</b> zastępuje standardowe firmware: płynne 60 FPS zamiast zacinającego obrazu, dźwięku i mikrofonu, gamepady, przejście USB i wsparcie Moonlight (<a href="https://www.usbridge.io/hardware-agent/">pobierz firmware</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Pobierz firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>Dongle sprzętowy USB/IP (ESP32-S3)</b><br>
      Oprogramowanie przejścia USB napotyka przeszkody na macOS: nie ma sterownika VHCI, do którego można by się podłączyć, więc zdalny tablet Wacom (lub inne urządzenie USB) nie może być przedstawiony jako prawdziwy sprzęt. Ten dongle podłącza się do Maca i wykonuje w sprzęcie to, co <code>vhci-hcd</code> robi w oprogramowaniu gdzie indziej — eksportowane urządzenie wyświetla się z własnym VID/PID, powiązanym z własnym sterownikiem macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="firmware & dokumentacja esp32-acm"></a>
    </td>
  </tr>
</table>

### NanoKVM z firmware USBridge: 60 FPS zamiast opóźnienia

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Wideo: Sipeed NanoKVM wgrany z firmware USBridge strumieniuje w 60 FPS z prawie żadnym opóźnieniem wejściowym"></a>
  <br><br>
  <img src="https://img.shields.io/badge/1080p-60_FPS-2da44e?style=flat-square" alt="1080p 60 FPS">
  <img src="https://img.shields.io/badge/Tailscale-P2P-242424?style=flat-square&logo=tailscale&logoColor=white" alt="Tailscale P2P">
  <img src="https://img.shields.io/badge/Dźwięk-dźwięk_%2B_mikrofon-007ec6?style=flat-square" alt="Dźwięk: dźwięk i mikrofon">
  <img src="https://img.shields.io/badge/FEC-odzyskiwanie_zgubionych_pakietów-f59e0b?style=flat-square" alt="FEC: korekcja błędów w przód">
</div>

Dwuminutowe wideo: Sipeed NanoKVM z dużym opóźnieniem w domowym laboratorium staje się KVM over IP z 60 FPS i prawie żadnym opóźnieniem wejściowym po wgraniu firmware USBridge, wystarczająco szybkim do grania w gry z gamepadem. Tailscale działa na samej płycie, więc jest dostępny z dowolnego miejsca bez przekierowywania portów; dźwięk działa w obie strony, a FEC (korekcja błędów w przód) odbudowuje zgubione pakiety, dzięki czemu obraz pozostaje czysty w Wi-Fi i w internecie. Firmware dla NanoKVM, Radxa Zero 3W i Radxa Cubie A7Z jest dostępne na [stronie Agenta Sprzętowego](https://www.usbridge.io/hardware-agent/).

## FAQ

<details>
<summary><b>Czy USBridge działa z Moonlight i Sunshine?</b></summary>

Tak. Agent dostarcza open-source streamer Sunshine, więc standardowy klient Moonlight łączy się z nim. Vibepollo (fork Sunshine z Apollo wbudowanym) i punktfunk są również wbudowane i przełączają się jednym kliknięciem.
</details>

<details>
<summary><b>Czy istnieje sprzętowy streamer Moonlight?</b></summary>

Tak. USBridge KVM 2.0, lub Radxa Zero 3W, Radxa Cubie A7Z lub Sipeed NanoKVM wgrany z firmware USBridge, to host GameStream kompatybilny z Sunshine dla wszystkiego, co jest na jego wejściu HDMI: PC, PS5, Xbox lub Switch. Strumieniuje 1080p 60 FPS z dźwiękiem do standardowego klienta Moonlight, bez oprogramowania na źródle. Zobacz [Sprzętowy streamer Moonlight](https://www.usbridge.io/moonlight-hardware-streamer/).
</details>

<details>
<summary><b>Który streamer jest najszybszy na moim PC?</b></summary>

To zależy od GPU, sterownika i sieci. Wbudowany benchmark uruchamia Sunshine, RustShine, Vibepollo i punktfunk na twoim własnym sprzęcie i sieci i porównuje je. Zobacz [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/).
</details>

<details>
<summary><b>Jakie GPU są wspierane do kodowania?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV i VAAPI na Linuxie. Wirtualne maszyny i komputery bez kodera GPU używają oprogramowania H.264 (OpenH264).
</details>

<details>
<summary><b>Co jest darmowe, a co wymaga Pro?</b></summary>

Kolor 4:4:4, HDR10, PyroWave, gamepady, mikrofon, MIDI i surowe przejście USB są darmowe. Tablety Wacom z ciśnieniem i nachyleniem oraz sprzętowe firmware KVM (19 USD za urządzenie na całe życie, 24 h darmowa ocena) są Pro.
</details>

<details>
<summary><b>Czy mogę przekształcić Sipeed NanoKVM lub płytę Radxa w IP-KVM?</b></summary>

Tak. Wgraj firmware USBridge z [strony Agenta Sprzętowego](https://www.usbridge.io/hardware-agent/). Uzyskujesz dostęp do BIOS/UEFI, ISO i IMG wirtualne media, niezmienne zrzuty Btrfs, wideo 60 FPS, dźwięk i przejście USB. Płyty Radxa potrzebują dongla HDMI-to-USB do przechwytywania.
</details>

<details>
<summary><b>Czy potrzebuję przekierowywania portów?</b></summary>

Nie. Połączenia odbywają się przez Tailscale peer-to-peer, z automatycznym przechodzeniem NAT.
</details>

<details>
<summary><b>Czy mogę dzielić dostęp do komputera z kimś innym?</b></summary>

Tak. Zapraszaj osoby przez e-mail z rolą i datą końcową; goście dołączają przez USBridge i nie potrzebują konta Tailscale.
</details>

## Zasoby i Licencja

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Rozwój i Plan działania</b><br>
      Utrzymuję publiczny pulpit nawigacyjny do śledzenia wszystkich planowanych funkcji, aktualizacji architektonicznych i harmonogramów wydania.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Zobacz na żywo Plan działania</a>
    </td>
    <td width="33%" valign="top">
      <b>Linki do projektu</b><br>
      <ul>
        <li><a href="https://usbridge.io">Oficjalna strona</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Beta Testy i Błędy)</a></li>
        <li><a href="https://www.reddit.com/r/USBridge/">Reddit</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">YouTube</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Wsparcie na Patreon</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Licencja (GPLv3)</b><br>
      Ten projekt jest licencjonowany na mocy <b>GPLv3</b>. Klient wieloplatformowy zawiera kod z <code>moonlight-common-c</code> (również GPLv3).<br><br>
      <a href="LICENSE">Zobacz plik LICENCJA</a>
    </td>
  </tr>
</table>