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

**USBridge Remote** to zintegrowany klient o wysokiej wydajności do zarządzania zdalnymi maszynami. Zaprojektowany, aby połączyć **dostęp do BIOS-u na poziomie sprzętowym** (za pomocą urządzeń USBridge KVM) oraz **zdalny pulpit oparty na oprogramowaniu** w jednym, uproszczonym interfejsie.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote">
</div>


## Pobierz

### Klient
Klient to interfejs sterujący — zainstalowany na Twoim komputerze stacjonarnym lub laptopie (lub uruchamiany bezpośrednio w przeglądarce). Zarządza połączeniami, zdalnym pulpitem na żywo, przejściem wirtualnych urządzeń i rejestrem zrzutów.

| Architektura | Windows | macOS | Linux | Android | iOS | Przeglądarka internetowa |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Otwórz aplikację](https://web.usbridge.io) |
| **ARM64** | — | [Pobierz](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Otwórz aplikację](https://web.usbridge.io) |

Wolisz bezpośredni plik APK bez konta w Sklepie Play? Samoaktualizująca się wersja jest również publikowana w [najnowższym wydaniu](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Klient internetowy bez instalacji**: Nie wymaga instalacji. Po prostu otwórz [web.usbridge.io](https://web.usbridge.io), aby natychmiast się połączyć. *(Uwaga: Klient internetowy działa z pewnymi ograniczeniami funkcji i wydajności z powodu zabezpieczeń przeglądarki i ograniczeń WebRTC. Aby uzyskać pełne, nieograniczone doświadczenie, użyj aplikacji natywnych).* Klient internetowy łączy się z Agentem przez jego zaufaną nazwę `<label>.device.usbridge.io`: Agent uzyskuje ten certyfikat za pierwszym razem, gdy przeglądarka się z nim łączy, i tylko gdy jesteś zalogowany na swoje konto USBridge w Agencie — więc pierwsza próba może zająć kilka minut. Zobacz wiersz **Status → Certyfikat** Agenta lub [dokumentację Agenta](agent/docs/README.md#platform-notes-from-the-top-level-readme) po szczegóły. USBridge KVM również obsługuje sam klient internetowy pod adresem `https://<kvm>:9443/` i przechodzi do swojej własnej zaufanej nazwy, gdy to możliwe.

## Agent

Agent działa na docelowej maszynie — serwerze lub komputerze PC, do którego chcesz uzyskać dostęp zdalnie. Obsługuje przechwytywanie ekranu, wstrzykiwanie wejścia i sieciowanie Tailscale.

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
      <img src="./assets/agent-screenshot.svg" alt="Interfejs Agenta USBridge" width="100%">
    </td>
  </tr>
</table>

## Funkcje

**Kodowanie przyspieszone sprzętowo:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · plus oprogramowanie **H.264 (OpenH264)** dla VM i maszyn bez enkodera GPU

<table>
  <tr>
    <td width="33%" valign="top">
      <code>PODSTAWOWA FUNKCJONALNOŚĆ</code><br><br>
      <b>OTWARTY KOD SUNSHINE</b><br>
      <i>Standardowe przesyłanie gier</i><br><br>
      ✓ Wbudowany streamer open-source Sunshine<br>
      ✓ Alternatywne streamery: Vibepollo (fork Sunshine z Apollo wbudowanym, Windows) i punktfunk (kompatybilny z Moonlight, Windows i Linux), przełączaj jednym kliknięciem<br>
      ✓ Wbudowany benchmark do porównania Sunshine, RustShine, Vibepollo i punktfunk na własnym sprzęcie i sieci<br>
      ✓ Niskolatencyjny dostęp do pulpitu i gier<br>
      ✓ Dwukierunkowy schowek plików i tekstu<br>    
      ✓ Przełączanie wyświetlaczy wielomonitorowych<br>
      ✓ Zintegrowane sieciowanie P2P Tailscale<br>
      ✓ Dostęp gościa: zapraszaj ludzi e-mailem z rolą i datą końcową<br>
      ✓ Natywne wsparcie Wayland (przechwytywanie bez komunikatów)
    </td>
    <td width="33%" valign="top">
      <code>+ PODSTAWOWE FUNKCJE</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Silnik strumieniowy oparty na Rust</i><br><br>
      ✓ Niestandardowy protokół zdalny do natychmiastowego połączenia<br>
      ✓ Przejście surowych urządzeń peryferyjnych USB<br>
      ✓ Adaptacyjne przesyłanie strumieniowe zoptymalizowane pod kątem stabilności Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Bezstratna dokładność kolorów 4:4:4 chroma<br>
      ✓ Przejście mikrofonu (klient → host)<br>
      ✓ Przejście urządzenia MIDI (klient → host)<br>
      ✓ Kodowanie GPU: NVENC, AMF, QSV, VAAPI<br>
      ✓ Oprogramowanie H.264 (OpenH264) dla VM bez GPU<br>
      ✓ Niskolatencyjny kodek falowy PyroWave (od Valve)<br>
      ✓ Zarządzanie wirtualnym wyświetlaczem bez głowy<br>
      ✓ Niskolatencyjne przejście kontrolera gamepada<br>
      ✓ Dostęp do klienta przeglądarki internetowej (nie wymaga instalacji)<br>
      ✓ Dostęp przed logowaniem w systemie Windows (bezpieczne wprowadzanie danych uwierzytelniających)
    </td>
    <td width="33%" valign="top">
      <code>+ PODSTAWOWE FUNKCJE</code> <code>+ FUNKCJE BEZPŁATNE</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Profesjonalne przepływy pracy</i><br><br>
      ✓ Wsparcie dla tabletów Wacom z naciskiem i przechyłem<br><br>
    </td>
  </tr>
</table>

## Szybki start

1. **Zainstaluj Agenta** na maszynie, do której chcesz uzyskać dostęp zdalnie. Uruchom go — wyświetli token połączenia i adres Tailscale. Połącz się z Tailscale, jeśli potrzebujesz dostępu przez internet.

2. **Zainstaluj Klienta** na swoim komputerze stacjonarnym, laptopie lub telefonie.

3. **Dodaj połączenie** — wprowadź adres IP lub adres Tailscale wyświetlony w oknie Agenta. To wszystko.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## Integracja sprzętowa

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Kontrola BIOS na poziomie sprzętowym</b><br>
      USBridge Remote integruje się natywnie z urządzeniem USBridge-KVM 2.0 do zarządzania zdalnego, bare-metal przed uruchomieniem systemu operacyjnego.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Kup USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>Oprogramowanie IP-KVM DIY</b><br>
      Wdróż oficjalne oprogramowanie na kompatybilnym SBC (np. Radxa Zero 3W/3E, Cubie A7) z interfejsem przechwytywania USB, aby skonfigurować niestandardowy węzeł KVM.<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Pobierz oprogramowanie"></a>
    </td>
    <td width="33%" valign="top">
      <b>Dongle sprzętowy USB/IP (ESP32-S3)</b><br>
      Przejście USB w oprogramowaniu napotyka przeszkody w systemie macOS: nie ma sterownika VHCI, do którego można by się podłączyć, więc zdalny tablet Wacom (lub inne urządzenie USB) nie może być przedstawiony jako prawdziwy sprzęt. Ten dongle podłącza się do Maca i wykonuje w sprzęcie to, co <code>vhci-hcd</code> robi w oprogramowaniu gdzie indziej — eksportowane urządzenie jest enumerowane z własnym VID/PID, przypisanym przez sterownik systemu macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="oprogramowanie i dokumentacja esp32-acm"></a>
    </td>
  </tr>
</table>

## Zasoby i licencja

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
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (testowanie beta i błędy)</a></li>
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