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

**USBridge Remote** — це єдиний високопродуктивний клієнт для управління віддаленими машинами. Розроблений для поєднання **доступу на рівні апаратного BIOS** (через пристрої USBridge KVM) та **програмного віддаленого робочого столу** в одному, спрощеному інтерфейсі.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote">
</div>


## Завантаження

### Клієнт
Клієнт — це інтерфейс управління, який встановлюється на вашій робочій станції або ноутбуці (або запускається безпосередньо у вашому браузері). Він керує з'єднаннями, живим віддаленим робочим столом, передачею віртуальних пристроїв та реєстром знімків.

| Архітектура | Windows | macOS | Linux | Android | iOS | Веб-браузер |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Відкрити додаток](https://web.usbridge.io) |
| **ARM64** | — | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Відкрити додаток](https://web.usbridge.io) |

Вам потрібен прямий APK без облікового запису Play Store? Також опубліковано самовідновлювальну збірку на [останньому випуску](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Веб-клієнт без установки**: Установка не потрібна. Просто відкрийте [web.usbridge.io](https://web.usbridge.io), щоб підключитися миттєво. *(Примітка: веб-клієнт працює з деякими обмеженнями функцій та продуктивності через безпеку браузера та обмеження WebRTC. Для повного досвіду без компромісів використовуйте рідні додатки).* Веб-клієнт досягає Агенту через його довірене `<label>.device.usbridge.io` ім'я: Агент отримує цей сертифікат перший раз, коли браузер до нього підходить, і тільки поки ви увійшли у свій обліковий запис USBridge в Агенті — тому перша спроба може зайняти кілька хвилин. Дивіться рядок **Статус → Сертифікат** Агенту або [документацію Агенту](agent/docs/README.md#platform-notes-from-the-top-level-readme) для деталей. USBridge KVM також обслуговує сам веб-клієнт за адресою `https://<kvm>:9443/` і переходить на своє власне довірене ім'я, коли може.

## Агент

Агент працює на цільовій машині — сервері або ПК, до якого ви хочете отримати віддалений доступ. Він обробляє захоплення екрану, ін'єкцію вводу та мережі Tailscale.

<table>
  <tr>
    <!-- Ліва колонка: таблиця завантажень -->
    <td valign="middle">
      <table>
        <tr>
          <th>Архітектура</th>
          <th>Windows</th>
          <th>macOS</th>
          <th>Linux</th>
        </tr>
        <tr>
          <td><b>x86_64</b></td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Завантажити</a></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">Завантажити</a></td>
        </tr>
        <tr>
          <td><b>ARM64</b></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Завантажити</a></td>
          <td>—</td>
        </tr>
      </table>
    </td>
    <!-- Права колонка: зображення -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="USBridge Agent Interface" width="100%">
    </td>
  </tr>
</table>

## Особливості

**Апаратура з апаратним прискоренням:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · плюс програмне забезпечення **H.264 (OpenH264)** для ВМ і машин без GPU-кодувальника

<table>
  <tr>
    <td width="33%" valign="top">
      <code>БАЗОВА ФУНКЦІОНАЛЬНІСТЬ</code><br><br>
      <b>OPEN-SOURCE SUNSHINE</b><br>
      <i>Стандартний геймерський стрімінг</i><br><br>
      ✓ Вбудований open-source стример Sunshine<br>
      ✓ Доступ до робочого столу та ігор з низькою затримкою<br>
      ✓ Двостороння передача файлів та текстового буфера обміну<br>    
      ✓ Перемикання між кількома моніторами<br>
      ✓ Інтегрована мережа Tailscale P2P<br>
      ✓ Доступ для гостей: запрошуйте людей електронною поштою з роллю та кінцевою датою<br>
      ✓ Підтримка Wayland (захоплення без запитів)
    </td>
    <td width="33%" valign="top">
      <code>+ БАЗОВІ ФУНКЦІЇ</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Стрімінг на базі Rust</i><br><br>
      ✓ Миттєве підключення через власний віддалений протокол<br>
      ✓ Передача сирих USB-периферійних пристроїв<br>
      ✓ Адаптивний стрімінг, оптимізований для стабільності Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Безвтратна точність кольору 4:4:4<br>
      ✓ Передача мікрофона (клієнт → хост)<br>
      ✓ Передача MIDI-пристроїв (клієнт → хост)<br>
      ✓ Кодування GPU: NVENC, AMF, QSV, VAAPI<br>
      ✓ Програмне забезпечення H.264 (OpenH264) для ВМ без GPU<br>
      ✓ Кодек низької затримки PyroWave (від Valve)<br>
      ✓ Управління безголовими віртуальними дисплеями<br>
      ✓ Низька затримка при передачі контролера ігор<br>
      ✓ Доступ до веб-браузера клієнта (установка не потрібна)<br>
      ✓ Доступ до Windows перед входом (безпечний ввід облікових даних)
    </td>
    <td width="33%" valign="top">
      <code>+ БАЗОВІ ФУНКЦІЇ</code> <code>+ БЕЗКОШТОВНІ ФУНКЦІЇ</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Професійні робочі процеси</i><br><br>
      ✓ Підтримка планшетів Wacom з тиском та нахилом<br><br>
    </td>
  </tr>
</table>

## Швидкий старт

1. **Встановіть Агент** на машині, до якої ви хочете отримати віддалений доступ. Запустіть його — він відобразить токен підключення та адресу Tailscale. Підключіть Tailscale, якщо вам потрібен доступ через інтернет.

2. **Встановіть Клієнт** на вашій робочій станції, ноутбуці або телефоні.

3. **Додайте з'єднання** — введіть IP-адресу або адресу Tailscale, показану у вікні Агенту. Ось і все.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## Інтеграція апаратного забезпечення

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Контроль BIOS на рівні апаратного забезпечення</b><br>
      USBridge Remote інтегрується нативно з пристроєм USBridge-KVM 2.0 для управління безпосередньо перед завантаженням ОС.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Купити USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVM ПЗ</b><br>
      Встановіть офіційне ПЗ на сумісний SBC (наприклад, Radxa Zero 3W/3E, Cubie A7) з USB-інтерфейсом захоплення, щоб налаштувати власний KVM-вузол.<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Отримати ПЗ"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IP Апаратний донгл (ESP32-S3)</b><br>
      Програмна передача USB стикається з проблемами на macOS: немає драйвера VHCI, до якого можна підключитися, тому віддалений планшет Wacom (або інший USB-пристрій) не може бути представленим як реальне апаратне забезпечення. Цей донгл підключається до Mac і виконує в апаратному забезпеченні те, що <code>vhci-hcd</code> робить в програмному забезпеченні в інших місцях — експортований пристрій перераховується з власним VID/PID, прив'язаним до драйвера macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm ПЗ та документація"></a>
    </td>
  </tr>
</table>

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
        <li><a href="https://www.patreon.com/USBridge_Technologies">Підтримка на Patreon</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Ліцензія (GPLv3)</b><br>
      Цей проект ліцензований під <b>GPLv3</b>. Мультиплатформений клієнт включає кодову базу з <code>moonlight-common-c</code> (також GPLv3).<br><br>
      <a href="LICENSE">Переглянути файл LICENSE</a>
    </td>
  </tr>
</table>