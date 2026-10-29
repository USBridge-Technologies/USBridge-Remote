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

**USBridge Remote** — це екосистема **віддаленого робочого столу** з низькою затримкою та **KVM-over-IP**: один клієнт для Windows, macOS, Linux, Android, iOS та браузера, агент хоста з вбудованим **Sunshine / Moonlight** ігровим стримером, а також IP-KVM прошивка для плат **Radxa Zero 3W/3E**, **Radxa Cubie A7Z** та **Sipeed NanoKVM**. Вона поєднує **доступ до BIOS на апаратному рівні** та **програмний віддалений робочий стіл** в одному інтерфейсі, з USB-проходом для ігрових контролерів, планшетів Wacom, MIDI та мікрофонів, а також P2P-мережу Tailscale без переадресації портів.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote features: remote desktop, game streaming, USB passthrough, hardware KVM">
</div>


## Завантажити

**Універсальний інсталятор (Клієнт + Агент, виберіть один або обидва під час налаштування):** [Windows Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) · [macOS disk image (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-macOS-arm64.dmg)

Всі файли доступні на [останній версії](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

### Клієнт
Клієнт — це інтерфейс управління — встановлюється на вашому робочому місці або ноутбуці (або запускається безпосередньо у вашому браузері). Він керує з'єднаннями, живим віддаленим робочим столом, проходом віртуальних пристроїв та реєстром знімків.

| Платформа | Інсталятор | Портативний | Пакети / Магазин |
| :--- | :--- | :--- | :--- |
| **Windows** x86_64 | [Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) | [Portable (.zip)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — |
| **macOS** Apple Silicon | [Disk image (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | — |
| **Linux** x86_64 | [Debian / Ubuntu (.deb)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.deb) · [Fedora / RHEL (.rpm)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.rpm) · [Arch (.pkg.tar.zst)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.pkg.tar.zst) | [AppImage](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | [APT · DNF · pacman repository](#install-from-the-terminal) · [Snap Store](https://snapcraft.io/usbridge-client) |
| **Android** ARM64 | [APK (самооновлювальний)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) |
| **iOS / iPadOS** | — | — | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |
| **Веб-браузер** | — | [Відкрити web.usbridge.io](https://web.usbridge.io) | — |

### Встановлення з терміналу

Одна команда встановлює Клієнт та Агент. На Linux вона вибирає рідний пакет для вашої дистрибуції (apt на Debian/Ubuntu, dnf на Fedora/RHEL, pacman на Arch), і пакет додає підписаний репозиторій USBridge, тому програми потім оновлюються разом з рештою системи:

```sh
# Linux та macOS
curl -fsSL https://www.usbridge.io/install.sh | sh
# Тільки Агент (машина, до якої ви отримуєте доступ лише віддалено)
curl -fsSL https://www.usbridge.io/install.sh | sh -s -- --agent
```

```powershell
# Windows (PowerShell)
irm https://www.usbridge.io/install.ps1 | iex
```

```sh
# Клієнт з Snap Store (Ubuntu та Arch з snapd з AUR)
sudo snap install usbridge-client
```

<details>
<summary><b>Додати APT репозиторій вручну (Debian / Ubuntu)</b></summary>

```sh
curl -fsSL https://raw.githubusercontent.com/USBridge-Technologies/USBridge-Remote/main/scripts/deb/usbridge-archive-keyring.gpg \
  | sudo tee /usr/share/keyrings/usbridge-archive-keyring.gpg >/dev/null
echo "deb [arch=amd64 signed-by=/usr/share/keyrings/usbridge-archive-keyring.gpg] https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download ./" \
  | sudo tee /etc/apt/sources.list.d/usbridge.list
sudo apt update && sudo apt install usbridge-client usbridge-agent
```

Завантажений `.deb`, `.rpm` або пакет Arch налаштовує той самий репозиторій, коли ви його встановлюєте.
</details>

🌐 **Zero-Install Web Client**: Встановлення не потрібне. Просто відкрийте [web.usbridge.io](https://web.usbridge.io), щоб миттєво підключитися. *(Примітка: Веб-клієнт працює з деякими обмеженнями функцій та продуктивності через безпековий пісочницю браузера та обмеження WebRTC. Для повного досвіду без компромісів використовуйте рідні програми).* Веб-клієнт досягає Агента через його довірене `<label>.device.usbridge.io` ім'я: Агент отримує цей сертифікат перший раз, коли браузер до нього підключається, і лише поки ви увійшли у свій обліковий запис USBridge в Агента — тому перша спроба може зайняти кілька хвилин. Дивіться рядок **Статус → Сертифікат** Агента або [документацію Агента](agent/docs/README.md#platform-notes-from-the-top-level-readme) для деталей. USBridge KVM також обслуговує сам веб-клієнт за адресою `https://<kvm>:9443/` і переходить на своє власне довірене ім'я, коли це можливо.

## Агент

Агент працює на цільовій машині — сервері або ПК, до якого ви хочете отримати віддалений доступ. Він захоплює екран і звук, передає вашу клавіатуру, мишу та ігровий контролер на цю машину і підключає її через Tailscale.

<table>
  <tr>
    <!-- Ліва колонка: таблиця завантажень -->
    <td valign="middle">
      <table>
        <tr>
          <th>Платформа</th>
          <th>Завантаження</th>
        </tr>
        <tr>
          <td><b>Windows</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64-Setup.exe">Setup (.exe)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Portable (.zip)</a></td>
        </tr>
        <tr>
          <td><b>macOS</b><br>Apple Silicon</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Disk image (.dmg)</a></td>
        </tr>
        <tr>
          <td><b>Linux</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.deb">Debian / Ubuntu (.deb)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.rpm">Fedora / RHEL (.rpm)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.pkg.tar.zst">Arch (.pkg.tar.zst)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">AppImage (портативний)</a></td>
        </tr>
      </table>
    </td>
    <!-- Права колонка: зображення -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="USBridge Agent window: connection token, Tailscale address and stream status" width="100%">
    </td>
  </tr>
</table>

## Функції

**Апаратне прискорення кодування:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · плюс програмне **H.264 (OpenH264)** для ВМ та машин без GPU кодувальника

<table>
  <tr>
    <td width="33%" valign="top">
      <code>BASIC FUNCTIONALITY</code><br><br>
      <b>SUNSHINE OPEN-SOURCE</b><br>
      <i>Стандартний ігровий стрімінг</i><br><br>
      ✓ Вбудований open-source Sunshine стример<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">Альтернативні стримери</a>: Vibepollo (форк Sunshine з вбудованим Apollo, Windows) та punktfunk (сумісний з Moonlight, Windows & Linux), перемикання в один клік<br>
      ✓ Вбудований бенчмарк для порівняння Sunshine, RustShine, Vibepollo та punktfunk на вашому власному обладнанні та мережі<br>
      ✓ Низькозатримуваний доступ до робочого столу та ігор<br>
      ✓ Двосторонній обмін файлами та текстовим буфером<br>    
      ✓ Перемикання між кількома моніторами<br>
      ✓ Інтегрована P2P-мережа Tailscale<br>
      ✓ Доступ для гостей: запрошуйте людей електронною поштою з роллю та кінцевою датою<br>
      ✓ Підтримка Wayland (захоплення без запитів)
    </td>
    <td width="33%" valign="top">
      <code>+ BASIC FEATURES</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Rust-based Stream Engine</i><br><br>
      ✓ Миттєве підключення через власний віддалений протокол<br>
      ✓ Сировинний <a href="https://www.usbridge.io/usb-passthrough/">USB-периферійний пристрій</a> проходу<br>
      ✓ Адаптивний стрімінг, оптимізований для стабільності Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Безвтратна точність кольору 4:4:4<br>
      ✓ Прохід мікрофона (клієнт → хост)<br>
      ✓ Прохід MIDI-пристрою (клієнт → хост)<br>
      ✓ Кодування GPU: NVENC, AMF, QSV, VAAPI<br>
      ✓ Програмне H.264 (OpenH264) для ВМ без GPU<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> низькозатримуваний вейвлет-кодек (від Valve)<br>
      ✓ Управління безголовим віртуальним дисплеєм<br>
      ✓ Низькозатримуваний прохід контролера ігрового контролера<br>
      ✓ Доступ до веб-браузера клієнта (встановлення не потрібно)<br>
      ✓ Доступ до Windows перед входом (безпечний ввід облікових даних)
    </td>
    <td width="33%" valign="top">
      <code>+ BASIC FEATURES</code> <code>+ FREE FEATURES</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Професійні робочі процеси</i><br><br>
      ✓ Підтримка планшетів Wacom з тиском та нахилом (<a href="https://www.usbridge.io/macos-wacom-support/">macOS також</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">Апаратна KVM прошивка</a> для Radxa Zero 3W/3E, Radxa Cubie A7Z та Sipeed NanoKVM: доступ до BIOS/UEFI, ISO віртуальні медіа, знімки Btrfs ($19 за все життя на пристрій, 24 години безкоштовної оцінки)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">Апаратний Moonlight стример</a> на цих платах: будь-яке HDMI джерело (ПК, PS5, Xbox, Switch) до Moonlight на 1080p 60 FPS, без програмного забезпечення на джерелі<br><br>
    </td>
  </tr>
</table>

## Швидкий старт

1. **Встановіть Агент** на машині, до якої ви хочете отримати віддалений доступ. Запустіть його — він відобразить токен підключення та адресу Tailscale. Підключіть Tailscale, якщо вам потрібен доступ через Інтернет.

2. **Встановіть Клієнт** на вашому робочому місці, ноутбуці або телефоні.

3. **Додайте з'єднання** — введіть IP або адресу Tailscale, показану у вікні Агента. Ось і все.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote quick start: install the agent, install the client, connect">
</div>

## Інтеграція апаратного забезпечення

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Контроль BIOS на апаратному рівні</b><br>
      USBridge Remote інтегрується нативно з пристроєм USBridge-KVM 2.0 для управління безперервно, на чистому металі перед завантаженням ОС. Це також перший апаратний Moonlight стример: стандартний клієнт Moonlight з'єднується з KVM і стрімить будь-яке HDMI джерело (ПК або ігрова консоль) без програмного забезпечення на ньому.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVM прошивка</b><br>
      Прошивка офіційної прошивки на сумісній платі (Radxa Zero 3W/3E, Radxa Cubie A7Z) з HDMI-to-USB захоплювальним адаптером, щоб створити власний IP-KVM. На <b>Sipeed NanoKVM</b> вона замінює стандартну прошивку: плавні 60 FPS замість ривків зображення, звук та мікрофон, ігрові контролери, USB-прохід та підтримка Moonlight (<a href="https://www.usbridge.io/hardware-agent/">завантажити прошивку</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IP апаратний адаптер (ESP32-S3)</b><br>
      Програмний USB-прохід стикається з обмеженнями на macOS: немає драйвера VHCI, до якого можна підключитися, тому віддалений планшет Wacom (або інший USB-пристрій) не може бути представленим як реальне апаратне забезпечення. Цей адаптер підключається до Mac і виконує в апаратному забезпеченні те, що <code>vhci-hcd</code> робить в програмному забезпеченні в інших місцях — експортований пристрій перераховується з його власним VID/PID, прив'язаним до драйвера macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm firmware & docs"></a>
    </td>
  </tr>
</table>

### NanoKVM з прошивкою USBridge: 60 FPS замість затримки

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Video: Sipeed NanoKVM flashed with USBridge firmware streams at 60 FPS with almost no input lag"></a>
  <br><br>
  <img src="https://img.shields.io/badge/1080p-60_FPS-2da44e?style=flat-square" alt="1080p 60 FPS">
  <img src="https://img.shields.io/badge/Tailscale-P2P-242424?style=flat-square&logo=tailscale&logoColor=white" alt="Tailscale P2P">
  <img src="https://img.shields.io/badge/Audio-sound_%2B_mic-007ec6?style=flat-square" alt="Audio: sound and microphone">
  <img src="https://img.shields.io/badge/FEC-packet_loss_recovery-f59e0b?style=flat-square" alt="FEC: forward error correction">
</div>

Двохвилинне відео: Sipeed NanoKVM з великою затримкою у домашній лабораторії стає KVM через IP з 60 FPS з майже нульовою затримкою після прошивки USBridge, достатньо швидким для ігор з ігровим контролером. Tailscale працює на самій платі, тому вона доступна з будь-якого місця без переадресації портів; звук відтворюється в обидві сторони, а FEC (виправлення помилок вперед) відновлює втрачені пакети, тому зображення залишається чистим на Wi-Fi та через Інтернет. Прошивка для NanoKVM, Radxa Zero 3W та Radxa Cubie A7Z доступна на [сторінці Апаратного Агента](https://www.usbridge.io/hardware-agent/).

## FAQ

<details>
<summary><b>Чи працює USBridge з Moonlight та Sunshine?</b></summary>

Так. Агент постачає open-source Sunshine стример, тому стандартний клієнт Moonlight підключається до нього. Vibepollo (форк Sunshine з вбудованим Apollo) та punktfunk також вбудовані та перемикаються в один клік.
</details>

<details>
<summary><b>Чи є апаратний Moonlight стример?</b></summary>

Так. USBridge KVM 2.0, або Radxa Zero 3W/3E, Radxa Cubie A7Z або Sipeed NanoKVM, прошиті прошивкою USBridge, є сумісним з Sunshine GameStream хостом для всього, що на його HDMI вході: ПК, PS5, Xbox або Switch. Він стрімить 1080p 60 FPS зі звуком до стандартного клієнта Moonlight, без програмного забезпечення на джерелі. Дивіться [Апаратний Moonlight стример](https://www.usbridge.io/moonlight-hardware-streamer/).
</details>

<details>
<summary><b>Який стример найшвидший на моєму ПК?</b></summary>

Це залежить від GPU, драйвера та мережі. Вбудований бенчмарк запускає Sunshine, RustShine, Vibepollo та punktfunk на вашому власному обладнанні та мережі та порівнює їх. Дивіться [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/).
</details>

<details>
<summary><b>Які GPU підтримуються для кодування?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV та VAAPI на Linux. Віртуальні машини та ПК без GPU кодувальника використовують програмне H.264 (OpenH264).
</details>

<details>
<summary><b>Що є безкоштовним, а що потребує Pro?</b></summary>

4:4:4 колір, HDR10, PyroWave, ігрові контролери, мікрофон, MIDI та сировинний USB-прохід є безкоштовними. Планшети Wacom з тиском та нахилом, а також апаратна KVM прошивка ($19 за все життя на пристрій, 24 години безкоштовної оцінки) є Pro.
</details>

<details>
<summary><b>Чи можу я перетворити Sipeed NanoKVM або плату Radxa в IP-KVM?</b></summary>

Так. Прошивка USBridge з [сторінки Апаратного Агента](https://www.usbridge.io/hardware-agent/). Ви отримуєте доступ до BIOS/UEFI, ISO та IMG віртуальні медіа, незмінні знімки Btrfs, 60 FPS відео, звук та USB-прохід. Плати Radxa потребують HDMI-to-USB захоплювального адаптера.
</details>

<details>
<summary><b>Чи потрібна мені переадресація портів?</b></summary>

Ні. З'єднання проходять через Tailscale peer-to-peer, з автоматичним обходом NAT.
</details>

<details>
<summary><b>Чи можу я поділитися доступом до комп'ютера з кимось іншим?</b></summary>

Так. Запрошуйте людей електронною поштою з роллю та кінцевою датою; гості приєднуються через USBridge і не потребують облікового запису Tailscale.
</details>

## Ресурси та ліцензія

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Розробка та дорожня карта</b><br>
      Я підтримую публічну панель для відстеження всіх запланованих функцій, архітектурних оновлень та графіків випуску.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Переглянути живу дорожню карту</a>
    </td>
    <td width="33%" valign="top">
      <b>Посилання на проект</b><br>
      <ul>
        <li><a href="https://usbridge.io">Офіційний веб-сайт</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (бета-тестування та помилки)</a></li>
        <li><a href="https://www.reddit.com/r/USBridge/">Reddit</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">YouTube</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Підтримка Patreon</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Ліцензія (GPLv3)</b><br>
      Цей проект ліцензований під <b>GPLv3</b>. Багатоплатформений клієнт включає кодову базу з <code>moonlight-common-c</code> (також GPLv3).<br><br>
      <a href="LICENSE">Переглянути файл LICENSE</a>
    </td>
  </tr>
</table>