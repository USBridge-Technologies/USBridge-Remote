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

**USBridge Remote** to ekosystem **zdalnego pulpitu** o niskim opóźnieniu i **KVM-over-IP**: jeden klient dla Windows, macOS, Linux, Android, iOS oraz przeglądarki, agent hosta z wbudowanym streamerem gier **Sunshine / Moonlight** oraz firmware IP-KVM dla płyt **Radxa Zero 3W**, **Radxa Cubie A7Z** i **Sipeed NanoKVM**. Łączy **dostęp do BIOS-u na poziomie sprzętowym** oraz **zdalny pulpit** w jednym interfejsie, z przejściem USB dla kontrolerów gier, tabletów Wacom, MIDI i mikrofonów oraz siecią P2P Tailscale bez przekierowywania portów.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote features: remote desktop, game streaming, USB passthrough, hardware KVM">
</div>


## Pobierz

### Klient
Klient to interfejs sterujący — zainstalowany na Twoim komputerze stacjonarnym lub laptopie (lub uruchomiony bezpośrednio w przeglądarce). Zarządza połączeniami, zdalnym pulpitem na żywo, przejściem wirtualnych urządzeń oraz rejestrem zrzutów.

| Architektura | Windows | macOS | Linux | Android | iOS | Przeglądarka internetowa |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Otwórz aplikację](https://web.usbridge.io) |
| **ARM64** | — | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Otwórz aplikację](https://web.usbridge.io) |

Preferujesz bezpośrednie APK bez konta w sklepie Play? Samoaktualizująca wersja jest również publikowana w [najnowszym wydaniu](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Klient internetowy bez instalacji**: Nie wymaga instalacji. Po prostu otwórz [web.usbridge.io](https://web.usbridge.io), aby natychmiast połączyć się. *(Uwaga: Klient internetowy działa z pewnymi ograniczeniami funkcji i wydajności z powodu zabezpieczeń przeglądarki i ograniczeń WebRTC. Aby uzyskać pełne, nieograniczone doświadczenie, użyj aplikacji natywnych).* Klient internetowy łączy się z Agentem przez jego zaufaną nazwę `<label>.device.usbridge.io`: Agent otrzymuje ten certyfikat za pierwszym razem, gdy przeglądarka się z nim łączy, i tylko podczas logowania do swojego konta USBridge w Agencie — więc pierwsza próba może zająć kilka minut. Zobacz wiersz **Status → Certyfikat** Agenta lub [dokumentację Agenta](agent/docs/README.md#platform-notes-from-the-top-level-readme) po szczegóły. USBridge KVM również obsługuje sam klient internetowy pod adresem `https://<kvm>:9443/` i przechodzi do swojej własnej zaufanej nazwy, gdy to możliwe.

## Agent

Agent działa na docelowej maszynie — serwerze lub komputerze, do którego chcesz uzyskać zdalny dostęp. Obsługuje przechwytywanie ekranu, wstrzykiwanie wejścia i sieć Tailscale.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Architektura</th>
          <th>Windows</th>
          <th>macOS</th>
          <th>Linux</th>
        </tr>
        <tr>
          <td><b>x86_64</b></td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Pobierz</a></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">Pobierz</a></td>
        </tr>
        <tr>
          <td><b>ARM64</b></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Pobierz</a></td>
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

## Funkcje

**Kodowanie przyspieszone sprzętowo:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · plus oprogramowanie **H.264 (OpenH264)** dla VM i maszyn bez kodera GPU

<table>
  <tr>
    <td width="33%" valign="top">
      <code>PODSTAWOWA FUNKCJONALNOŚĆ</code><br><br>
      <b>OTWARTY KOD SUNSHINE</b><br>
      <i>Standardowe przesyłanie gier</i><br><br>
      ✓ Wbudowany open-source streamer Sunshine<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">Alternatywne streamery</a>: Vibepollo (fork Sunshine z wbudowanym Apollon, Windows) i punktfunk (kompatybilny z Moonlight, Windows i Linux), przełączaj jednym kliknięciem<br>
      ✓ Wbudowany benchmark do porównania Sunshine, RustShine, Vibepollo i punktfunk na własnym sprzęcie i sieci<br>
      ✓ Niskolatencyjny dostęp do pulpitu i gier<br>
      ✓ Dwukierunkowy schowek plików i tekstów<br>    
      ✓ Przełączanie wyświetlaczy wielomonitorowych<br>
      ✓ Zintegrowana sieć P2P Tailscale<br>
      ✓ Dostęp gości: zapraszaj ludzi przez e-mail z rolą i datą końcową<br>
      ✓ Natywne wsparcie Wayland (przechwytywanie bez komunikatów)
    </td>
    <td width="33%" valign="top">
      <code>+ PODSTAWOWE FUNKCJE</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Silnik przesyłania oparty na Rust</i><br><br>
      ✓ Natychmiastowe połączenie z niestandardowym protokołem zdalnym<br>
      ✓ Surowe <a href="https://www.usbridge.io/usb-passthrough/">przejście urządzeń peryferyjnych USB</a><br>
      ✓ Adaptacyjne przesyłanie zoptymalizowane pod kątem stabilności Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Bezstratna dokładność kolorów 4:4:4<br>
      ✓ Przejście mikrofonu (klient → host)<br>
      ✓ Przejście urządzenia MIDI (klient → host)<br>
      ✓ Kodowanie GPU: NVENC, AMF, QSV, VAAPI<br>
      ✓ Oprogramowanie H.264 (OpenH264) dla VM bez GPU<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> niskolatencyjny kodek falowy (od Valve)<br>
      ✓ Zarządzanie wirtualnym wyświetlaczem bez głowy<br>
      ✓ Niskolatencyjne przejście kontrolera gier<br>
      ✓ Dostęp do klienta przeglądarki internetowej (nie wymaga instalacji)<br>
      ✓ Dostęp przedlogowania w systemie Windows (bezpieczne wprowadzanie danych uwierzytelniających)
    </td>
    <td width="33%" valign="top">
      <code>+ PODSTAWOWE FUNKCJE</code> <code>+ BEZPŁATNE FUNKCJE</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Profesjonalne przepływy pracy</i><br><br>
      ✓ Wsparcie dla tabletów Wacom z ciśnieniem i nachyleniem (<a href="https://www.usbridge.io/macos-wacom-support/">macOS również</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">Firmware KVM sprzętowego</a> dla Radxa Zero 3W, Radxa Cubie A7Z i Sipeed NanoKVM: dostęp do BIOS/UEFI, ISO wirtualne media, migawki Btrfs (19 USD za urządzenie na całe życie, 24 h darmowa ocena)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">Sprzętowy streamer Moonlight</a> na tych płytach: każde źródło HDMI (PC, PS5, Xbox, Switch) do Moonlight w 1080p 60 FPS, bez oprogramowania na źródle<br><br>
    </td>
  </tr>
</table>

## Szybki start

1. **Zainstaluj Agenta** na maszynie, do której chcesz uzyskać zdalny dostęp. Uruchom go — wyświetli token połączenia i adres Tailscale. Połącz się z Tailscale, jeśli potrzebujesz dostępu przez internet.

2. **Zainstaluj Klienta** na swoim komputerze stacjonarnym, laptopie lub telefonie.

3. **Dodaj połączenie** — wprowadź adres IP lub adres Tailscale pokazany w oknie Agenta. To wszystko.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote quick start: install the agent, install the client, connect">
</div>

## Integracja sprzętowa

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Kontrola BIOS na poziomie sprzętowym</b><br>
      USBridge Remote integruje się natywnie z urządzeniem USBridge-KVM 2.0 do zarządzania poza pasmem, na poziomie sprzętowym przed uruchomieniem systemu operacyjnego. Jest również pierwszym sprzętowym streamerem Moonlight: standardowy klient Moonlight łączy się z KVM i przesyła dowolne źródło HDMI (komputer lub konsolę do gier) bez oprogramowania na nim.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Kup USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>Firmware IP-KVM DIY</b><br>
      Zainstaluj oficjalny firmware na kompatybilnej płycie (Radxa Zero 3W, Radxa Cubie A7Z) z donglem HDMI-do-USB, aby zbudować własny IP-KVM. Na <b>Sipeed NanoKVM</b> zastępuje standardowy firmware: płynne 60 FPS zamiast zacinającego obrazu, dźwięku i mikrofonu, kontrolerów gier, przejścia USB i wsparcia Moonlight (<a href="https://www.usbridge.io/hardware-agent/">pobierz firmware</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Pobierz firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>Dongle sprzętowy USB/IP (ESP32-S3)</b><br>
      Oprogramowanie przejścia USB napotyka przeszkody na macOS: nie ma sterownika VHCI, do którego można się podłączyć, więc zdalny tablet Wacom (lub inne urządzenie USB) nie może być przedstawiony jako prawdziwy sprzęt. Ten dongle podłącza się do Maca i wykonuje w sprzęcie to, co <code>vhci-hcd</code> robi w oprogramowaniu gdzie indziej — eksportowane urządzenie jest enumerowane z własnym VID/PID, związanym z własnym sterownikiem macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="firmware i dokumentacja esp32-acm"></a>
    </td>
  </tr>
</table>

### NanoKVM z firmwarem USBridge: 60 FPS zamiast opóźnienia

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Wideo: Sipeed NanoKVM z wgranym firmwarem USBridge przesyła w 60 FPS z prawie zerowym opóźnieniem"></a>
</div>

Dwuminutowe wideo: Sipeed NanoKVM z dużym opóźnieniem w domowym laboratorium staje się KVM 60 FPS przez IP z prawie zerowym opóźnieniem po wgraniu firmware USBridge, wystarczająco szybkim do grania w gry z kontrolerem. Firmware dla NanoKVM, Radxa Zero 3W i Radxa Cubie A7Z jest dostępny na [stronie Agenta Sprzętowego](https://www.usbridge.io/hardware-agent/).

## FAQ

<details>
<summary><b>Czy USBridge działa z Moonlight i Sunshine?</b></summary>

Tak. Agent dostarcza open-source streamer Sunshine, więc standardowy klient Moonlight łączy się z nim. Vibepollo (fork Sunshine z wbudowanym Apollon) i punktfunk są również wbudowane i przełączają się jednym kliknięciem.
</details>

<details>
<summary><b>Czy istnieje sprzętowy streamer Moonlight?</b></summary>

Tak. USBridge KVM 2.0, lub Radxa Zero 3W, Radxa Cubie A7Z lub Sipeed NanoKVM z wgranym firmwarem USBridge, jest hostem GameStream kompatybilnym z Sunshine dla wszystkiego, co jest na jego wejściu HDMI: PC, PS5, Xbox lub Switch. Przesyła 1080p 60 FPS z dźwiękiem do standardowego klienta Moonlight, bez oprogramowania na źródle. Zobacz [Sprzętowy streamer Moonlight](https://www.usbridge.io/moonlight-hardware-streamer/).
</details>

<details>
<summary><b>Który streamer jest najszybszy na moim PC?</b></summary>

To zależy od GPU, sterownika i sieci. Wbudowany benchmark uruchamia Sunshine, RustShine, Vibepollo i punktfunk na Twoim własnym sprzęcie i sieci i porównuje je. Zobacz [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/).
</details>

<details>
<summary><b>Jakie GPU są obsługiwane do kodowania?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV i VAAPI na Linuxie. Wirtualne maszyny i komputery bez kodera GPU używają oprogramowania H.264 (OpenH264).
</details>

<details>
<summary><b>Co jest darmowe, a co wymaga Pro?</b></summary>

Kolor 4:4:4, HDR10, PyroWave, kontrolery gier, mikrofon, MIDI i surowe przejście USB są darmowe. Tablety Wacom z ciśnieniem i nachyleniem oraz firmware KVM sprzętowego (19 USD za urządzenie na całe życie, 24 h darmowa ocena) są Pro.
</details>

<details>
<summary><b>Czy mogę przekształcić Sipeed NanoKVM lub płytę Radxa w IP-KVM?</b></summary>

Tak. Wgraj firmware USBridge z [strony Agenta Sprzętowego](https://www.usbridge.io/hardware-agent/). Uzyskasz dostęp do BIOS/UEFI, ISO i IMG wirtualne media, niezmienne migawki Btrfs, wideo 60 FPS, dźwięk i przejście USB. Płyty Radxa potrzebują dongla HDMI-do-USB.
</details>

<details>
<summary><b>Czy potrzebuję przekierowywania portów?</b></summary>

Nie. Połączenia odbywają się przez Tailscale peer-to-peer, z automatycznym przechodzeniem przez NAT.
</details>

<details>
<summary><b>Czy mogę dzielić dostęp do komputera z inną osobą?</b></summary>

Tak. Zapraszaj ludzi przez e-mail z rolą i datą końcową; goście dołączają przez USBridge i nie potrzebują konta Tailscale.
</details>

## Zasoby i Licencja

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Rozwój i Plan działania</b><br>
      Utrzymuję publiczny panel do śledzenia wszystkich planowanych funkcji, aktualizacji architektury i harmonogramów wydania.<br><br>
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
      Ten projekt jest licencjonowany na podstawie <b>GPLv3</b>. Klient wieloplatformowy zawiera kod z <code>moonlight-common-c</code> (również GPLv3).<br><br>
      <a href="LICENSE">Zobacz plik LICENCJA</a>
    </td>
  </tr>
</table>