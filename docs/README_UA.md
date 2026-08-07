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

**USBridge Remote** — це єдиний високопродуктивний клієнт для управління віддаленими машинами. Я розробив його для поєднання **доступу до BIOS на апаратному рівні** (через пристрої USBridge KVM) та **програмного забезпечення для віддаленого робочого столу** в одному, зручному інтерфейсі.

 🖥️ **Потрібен контроль за BIOS на апаратному рівні перед завантаженням ОС?**  
 USBridge Remote інтегрується нативно з **USBridge-KVM 2.0** для управління поза межами каналу, на металевому рівні.

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)


> ⚠️ **Бета-програмне забезпечення** — Це ранній реліз. Очікуйте помилок. Будь ласка, повідомляйте про проблеми через [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues) або приєднуйтесь до нашого [Discord](https://discord.com/invite/xqQ6ybkfWS) для підтримки.
> 
> ℹ️ **Примітка щодо помилкових сповіщень Windows Defender / Антивірусів:**  
> Windows Defender може неправильно позначити `libva.dll` як загрозу (`Trojan:Win32/Wacatac.B!ml`) через евристичне/машинне навчання для виявлення непідписаних бінарних файлів. **Це помилкове сповіщення.**  
> Ми подали файл до Microsoft Security Intelligence для офіційного перегляду та внесення до білого списку. Тим часом, якщо ваш антивірус видаляє `libva.dll`, будь ласка, відновіть його з карантину або додайте папку USBridge до списку виключень вашого антивірусу.

---

## Завантажити

### Клієнт
Клієнт — це інтерфейс управління, встановлений на вашому робочому місці або ноутбуці. Він керує з'єднаннями, живим віддаленим робочим столом, проходженням віртуальних пристроїв та реєстрацією знімків.

| Архітектура | Windows | macOS | Linux | Android | iOS |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — |
| **ARM64** | — | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Завантажити](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |

## Агент

Агент працює на цільовій машині — сервері або ПК, до якого ви хочете отримати віддалений доступ. Він обробляє захоплення екрану, введення даних та мережу Tailscale.

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

## У медіа

| Медіа | Висвітлення | Посилання |
| :--- | :--- | :---: |
| **IlSoftware.it** (Італія) | *"USBridge Remote кидає виклик RustDesk та AnyDesk..."* — незалежний глибокий огляд, що хвалить синергію програмного агента та апаратного KVM, нативну підтримку Wayland та P2P архітектуру. | [Читати статтю](https://www.ilsoftware.it/alternativa-rustdesk-anydesk-usbridge/) |

---

## Особливості

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**Одне місце для всього** — я об'єднав робочий процес. Керуйте апаратним забезпеченням USBridge KVM та програмними агентами з єдиної панелі управління. Додайте машину, підключіться, і ви всередині.

**Без обмежень, без підписок** — абсолютно безкоштовно. Немає обмежень на час сесії, немає обмежень на з'єднання, і не потрібно створювати обліковий запис на цільовій машині.

**Відео з низькою затримкою та інтеграція Moonlight** — насолоджуйтесь роздільною здатністю до 2K з надзвичайно плавними 240 FPS та нульовою помітною затримкою. Мій адаптивний стрімінговий движок використовує нативну інтеграцію Moonlight для забезпечення безпрецедентної продуктивності віддаленого робочого столу з ультра-низькою затримкою.

**Інтеграція Tailscale** — вбудоване зашифроване P2P тунелювання. Підключайтеся до будь-якої машини у світі без необхідності налаштування переадресації портів або правил брандмауера. Це працює в локальній мережі та через інтернет автоматично.

**Спільний буфер обміну** — копіюйте та вставляйте безперешкодно між вашою локальною машиною та віддаленими цілями. Він повністю підтримує текст, зображення та передачу файлів з коробки.

**Підтримка кількох моніторів** — я додав можливість перемикатися між кількома дисплеями. Якщо цільова машина має кілька моніторів, ви тепер можете легко вибрати, який з них переглядати безпосередньо з налаштувань з'єднання. 

<img width="2080" height="1170" alt="Screenshot 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />


---

## Підтримка Wayland (без запитів)

Більшість агентів віддаленого робочого столу на Linux мають проблеми з Wayland або постійно спамлять вас запитами на дозвіл та спливаючими вікнами підтвердження щоразу, коли починається сесія. 

Я розробив USBridge Agent для нативної підтримки Wayland. Він обробляє повне захоплення екрану та введення даних з коробки **без будь-яких набридливих запитів на дозвіл** або ручних підтверджень. Він просто працює.

---

## Швидкий старт

1. **Встановіть Агент** на машині, до якої ви хочете отримати віддалений доступ. Запустіть його — він відобразить токен з'єднання та адресу Tailscale. Підключіть Tailscale, якщо вам потрібен доступ через інтернет.

2. **Встановіть Клієнт** на вашому робочому місці, ноутбуці або телефоні.

3. **Додайте з'єднання** — введіть IP-адресу або адресу Tailscale, показану у вікні Агента. Ось і все.

---

## Дорожня карта проекту

Я управляю планами розробки програмного забезпечення та майбутніми функціями в відкритій панелі управління. Якщо ви хочете побачити, що наразі розробляється, що заплановано або відстежити статус майбутніх функцій, перегляньте живу дорожню карту:

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