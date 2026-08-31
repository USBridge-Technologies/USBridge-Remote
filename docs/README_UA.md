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

**USBridge Remote** — це єдиний високопродуктивний клієнт для управління віддаленими машинами. Я розробив його, щоб об'єднати **доступ до BIOS на апаратному рівні** (через пристрої USBridge KVM) та **десктоп віддаленого доступу на програмному рівні** в одному, зручному інтерфейсі.

 🖥️ **Потрібен контроль BIOS на апаратному рівні до завантаження ОС?**  
 USBridge Remote інтегрується нативно з **USBridge-KVM 2.0** для управління на рівні металу.

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)


> ⚠️ **Бета-програмне забезпечення** — Це ранній реліз. Очікуйте помилок. Будь ласка, повідомляйте про проблеми через [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues) або приєднуйтесь до нашого [Discord](https://discord.com/invite/xqQ6ybkfWS) для підтримки.
> 
> ℹ️ **Примітка щодо помилкових сповіщень Windows Defender / антивірусів:**  
> Windows Defender може неправильно позначати `libva.dll` як загрозу (`Trojan:Win32/Wacatac.B!ml`) через евристичне/машинне навчання для виявлення непідписаних бінарних файлів. **Це помилкове сповіщення.**  
> Ми подали файл до Microsoft Security Intelligence для офіційного перегляду та внесення до білого списку. Тим часом, якщо ваш антивірус видалить `libva.dll`, будь ласка, відновіть його з карантину або додайте папку USBridge до списку виключень вашого антивірусу.

---

## Завантажити

### Клієнт
Клієнт — це інтерфейс управління, встановлений на вашій робочій станції або ноутбуці (або запущений безпосередньо у вашому браузері). Він керує з'єднаннями, живим віддаленим робочим столом, проходженням віртуальних пристроїв і реєстрацією знімків.

| Архітектура | Windows | macOS | Linux | Android | iOS | Веб-браузер |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Відкрити додаток](https://web.usbridge.io) |
| **ARM64** | — | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Відкрити додаток](https://web.usbridge.io) |

Вибираєте прямий APK без облікового запису в Play Store? Самооновлювальна версія також опублікована на [останній версії](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Веб-клієнт без установки**: Установка не потрібна. Просто відкрийте [web.usbridge.io](https://web.usbridge.io), щоб підключитися миттєво. *(Примітка: веб-клієнт працює з деякими обмеженнями функцій та продуктивності через безпеку браузера та обмеження WebRTC. Для повного досвіду без компромісів використовуйте рідні додатки).*

## Агент

Агент працює на цільовій машині — сервері або ПК, до якого ви хочете отримати віддалений доступ. Він обробляє захоплення екрану, ін'єкцію введення та мережеве з'єднання Tailscale.

| Архітектура | Windows | macOS | Linux |
| :--- | :---: | :---: | :---: |
| **x86_64** | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip) | — | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage) |
| **ARM64** | — | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg) | — |

---

## Демонстрація

<div align="center">
  <a href="https://youtu.be/1pV9PJeBr7M">
    <img src="https://img.youtube.com/vi/1pV9PJeBr7M/maxresdefault.jpg" alt="USBridge Remote Demo" style="max-width: 100%; border-radius: 8px;">
  </a>
</div>

---

## Особливості

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**Одне місце для всього** — Я об'єднав робочий процес. Керуйте апаратним забезпеченням USBridge KVM та програмними агентами з одного інформаційного панелі. Додайте машину, підключіться, і ви в справі.

**Без обмежень, без підписок** — Абсолютно безкоштовно. Немає обмежень на час сесії, немає обмежень на з'єднання, і не потрібен обліковий запис на цільовій машині.

**Відео з низькою затримкою та інтеграція Moonlight** — Насолоджуйтесь роздільною здатністю до 2K з плавними 240 FPS і нульовою помітною затримкою. Мій адаптивний стрімінговий двигун використовує нативну інтеграцію Moonlight для забезпечення неперевершеної продуктивності віддаленого робочого столу з ультра-низькою затримкою.

**Інтеграція Tailscale** — Вбудоване зашифроване P2P тунелювання. Підключайтеся до будь-якої машини по всьому світу без налаштування переадресації портів або правил брандмауера. Це працює в локальній мережі та через інтернет автоматично.

**Спільний буфер обміну** — Копіюйте та вставляйте безперешкодно між вашою локальною машиною та віддаленими цілями. Він повністю підтримує текст, зображення та передачу файлів з коробки.

**Підтримка кількох моніторів** — Я додав можливість перемикатися між кількома дисплеями. Якщо цільова машина має кілька моніторів, ви тепер можете легко вибрати, який з них переглядати безпосередньо з налаштувань з'єднання. 

<img width="2080" height="1170" alt="Screenshot 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />


---

## Підтримка Wayland (без запитів)

Більшість агентів віддаленого робочого столу на Linux мають проблеми з Wayland або постійно спамлять вас запитами на дозвіл і спливаючими вікнами підтвердження щоразу, коли починається сесія. 

Я розробив USBridge Agent для нативної підтримки Wayland. Він обробляє повне захоплення екрану та ін'єкцію введення з коробки **без будь-яких набридливих запитів на дозвіл** або ручних підтверджень. Він просто працює.

---

## Швидкий старт

1. **Встановіть Агент** на машині, до якої ви хочете отримати віддалений доступ. Запустіть його — він відобразить токен з'єднання та адресу Tailscale. Підключіть Tailscale, якщо вам потрібен доступ через інтернет.

2. **Встановіть Клієнт** на вашій робочій станції, ноутбуці або телефоні.

3. **Додайте з'єднання** — введіть IP або адресу Tailscale, показану у вікні Агента. Ось і все.

---

## Дорожня карта проекту

Я керую планами розробки програмного забезпечення та майбутніми функціями в відкритій інформаційній панелі. Якщо ви хочете побачити, що наразі розробляється, що заплановано або відстежити статус майбутніх функцій, перегляньте живу дорожню карту:

 **[Переглянути дорожню карту USBridge Remote](https://github.com/orgs/USBridge-Technologies/projects/3)**

---

## Спільнота та бета-тестування

Приєднуйтесь до нашого Discord, щоб отримати роль **Бета-тестера**, повідомляти про помилки та допомогти мені сформувати дорожню карту:

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## Посилання

- 🌐 [Офіційний вебсайт](https://usbridge.io)
- ❤️ [Сторінка Patreon](https://www.patreon.com/USBridge_Technologies)
- 🛒 [USBridge KVM 2.0 на Crowd Supply](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)

---

## 📜 Ліцензія

Цей проект ліцензований під **GPLv3** (див. [`LICENSE`](LICENSE)). Клієнт для Android/Windows/macOS/Linux містить код з `moonlight-common-c` (також GPLv3).