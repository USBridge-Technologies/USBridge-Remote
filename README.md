# USBridge Client

Нативный кросс‑платформенный клиент на Go (Fyne) для управления USBridge 2.

Проект объединяет управление устройствами, NBD‑экспорты образов и видео‑стриминг с минимальной задержкой. Основной режим подключения — через защищённый QUIC‑туннель FRP, с опцией прямого подключения в локальной сети.

## Возможности

- Подключение к Bridge по адресу и токену, менеджер сохранённых подключений, QR/deep‑link.
- Управление устройствами: клавиатура, мышь/тачскрин, RNDIS (несколько режимов), CD‑ROM образы.
- NBD‑экспорт образов (`.iso`, `.img`, `.vmdk`, `.vdi`, `.qcow2`, `.raw` и др.).
- Безопасный режим по умолчанию: экспорт только чтение; RW — через overlay (базовый образ не портится).
- Загрузка образов на устройство с прогрессом.
- Видео с минимальной задержкой: RTP H.264 по UDP + GStreamer (аппаратные декодеры, fallback на software).
- Полноэкранный режим, виртуальная клавиатура, тачпад‑режим управления.
- Снапшоты/backup‑flash (MTP), подключение снапшотов как устройств.
- Поддержка Android (NBD через SAF, сборка через gomobile/Gradle, GStreamer dynamic).

## Как подключается клиент

1. В адресной строке вводится **хост Bridge** и **токен**.
2. Если `frp_enabled: true` (по умолчанию), приложение поднимает **FRP QUIC‑туннель**:
   - HTTP API Bridge проксируется на `127.0.0.1:<port>`.
   - Видео идёт через SUDP proxy на локальный UDP‑порт.
   - NBD‑порты проброшены на `localhost:10809..10824`.
3. Если `frp_enabled: false`, используется прямое подключение к Bridge.

Токен берётся из поля ввода; если поле пустое — используется `frp_auth_token` из конфигурации.

## Видео

- Протокол: **RTP H.264 по UDP**.
- Приём: **GStreamer** (`udpsrc → rtph264depay → decoder → appsink`).
- Порт по умолчанию: `video_udp_port: 55000`.

Подробности и отладка:
- `docs/FRP_VIDEO_CLIENT.md`
- `docs/FRP_VIDEO_VERIFICATION.md`
- `docs/VIDEO_UDP_DEBUG.md`

## NBD / образы дисков

- Экспорт образов через встроенный NBD (go‑nbd) и/или `qemu-nbd` для контейнерных форматов (VMDK/QCOW2/VDI).
- В режиме RW используется overlay‑диск; базовый файл остаётся неизменным.
- NBD‑сервер слушает **localhost** (безопасно). При FRP подключении Bridge цепляется к локальным портам через туннель.

Документация:
- `docs/NBD_ANDROID_USAGE.md` (Android NBD)

## Сборка и зависимости

### Общие требования

- Go 1.21+
- GStreamer 1.0+ (для видео)
- `qemu-nbd` (опционально, для VMDK/QCOW2/VDI)

### Linux

Зависимости Fyne:

```bash
sudo apt-get install gcc libgl1-mesa-dev xorg-dev
```

Сборка:

```bash
scripts/build_linux.sh
```

Результат: `dist/linux/USBridgeClient.bin` (+ `config.yaml` при наличии).

### macOS

Установка GStreamer (Homebrew):

```bash
brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad
```

Сборка:

```bash
scripts/build_macos.sh
```

Результат: `dist/macos/USBridgeClient/` (бинарник + `run.sh` с правильным окружением).

### Windows (кросс-сборка на Linux)

Зависимости:
- `mingw-w64`
- `pkg-config` и `x86_64-w64-mingw32-pkg-config` (или `GSTREAMER_ROOT` с MinGW GStreamer SDK)

Сборка:

```bash
scripts/build_windows.sh
```

Опционально для портативного пакета с GStreamer:

```bash
export GSTREAMER_ROOT="C:/gstreamer/1.0/mingw_x86_64"
scripts/build_windows.sh
```

Результат: `dist/windows/USBridge_Client.exe` + копия `config.yaml` и, при наличии, библиотеки GStreamer.

### Android (Gradle APK с камерой/QR/SAF)

Зависимости:
- JDK (скрипт проверяет `java`, рекомендуем `openjdk-21-jdk`)
- Android SDK + NDK (`ANDROID_HOME`, `ANDROID_NDK_HOME`)
- `gomobile` (`go install golang.org/x/mobile/cmd/gomobile@latest`)

Сборка:

```bash
scripts/build_android_gradle.sh
```

Скрипт сам:
- собирает GStreamer dynamic (`.so`) или использует prebuilt,
- делает `gomobile bind` для `nbdbridge`,
- собирает Fyne APK и переносит `.so`,
- делает Gradle `assembleRelease` и подписывает debug‑ключом.

Результат: `dist/android/USBridge_Client_gradle.apk`

### iOS

Сборка возможна только на macOS (нужен Xcode). Скрипт‑обёртка:

```bash
scripts/build_ios.sh
```

## Запуск

```bash
./usbridge-client
# или
./usbridge-client -config /path/to/config.yaml
```

CLI‑аргументы:
- `-config` — путь к конфигурации
- `-log-level` — уровень логирования (`debug`, `info`, `warn`, `error`)
- `-version` — показать версию

## Конфигурация

Файл ищется в порядке:
- `./config.yaml|yml|json|toml`
- `~/.config/usbridge-client/config.yaml`
- `/etc/usbridge-client/config.yaml`

Пример (`config.yaml`):

```yaml
usb_port: 8080
api_timeout: 30

nbd_port: 10809
max_clients: 5
scan_paths: ["./iso", "/home/user/iso", "/mnt/iso"]
supported_types: [".iso", ".img", ".vmdk", ".vdi", ".qcow", ".qcow2", ".raw", ".vmi"]
nbd_export_read_only: true

video_udp_port: 55000

video_bitrate: 2000
video_width: 640
video_height: 480
video_fps: 30

audio_codec: "Opus"
audio_bitrate: 128
audio_sample_rate: 48000
audio_channels: 2

window_width: 1200
window_height: 800
theme: "light"
log_level: "debug"

frp_server_port: 443
frp_auth_token: "usbridge-secret-token"
frp_enabled: true
frp_tls_cert: "./certs/server.crt"
frp_tls_key: "./certs/server.key"
frp_tls_ca: "./certs/ca.crt"
```

### Переменные окружения

Все параметры можно переопределить через `USBRIDGE_*` (Viper `AutomaticEnv`).
Пример:

```bash
export USBRIDGE_NBD_PORT=10809
export USBRIDGE_FRP_ENABLED=true
```

## Интерфейс

Основные вкладки:
- **Устройства** — диски/образы, клавиатура, мышь/тачскрин, RNDIS, загрузка образов.
- **Управление** — видео, полноэкранный режим, виртуальная клавиатура, тачпад.
- **Снапшоты** — backup‑flash и список снапшотов.

Дополнительно:
- Адресная строка с хостом и токеном.
- Кнопки Power/Reset (PC Panel).
- Панель статусов подключённых устройств.

## Логи

По умолчанию пишутся в `./logs/app.log`. Можно задать директорию через `USBRIDGE_LOG_DIR`.

## Документация

- `docs/api_endpoints.md` — API Bridge.
- `docs/MOUSE_TOUCHPAD.md` — режимы мыши/тачскрина.
- `docs/FRP_VIDEO_CLIENT.md`, `docs/FRP_VIDEO_VERIFICATION.md` — видео + FRP.
- `docs/VIDEO_UDP_DEBUG.md` — диагностика UDP видео.
- `docs/NBD_ANDROID_USAGE.md` — NBD на Android.
- `docs/ANDROID_GSTREAMER_DYNAMIC.md`, `docs/ANDROID_LOGCAT.md` — Android сборка и логи.

## Структура проекта

```
usbridge-client/
├── cmd/                  # Точка входа
├── internal/
│   ├── api/              # HTTP/WebSocket клиент Bridge
│   ├── models/           # Модели и конфиг
│   └── ui/               # Fyne UI
├── nbdbridge/            # NBD backend для Android (gomobile)
├── android/              # Android оболочка/Gradle проект
├── scripts/              # Скрипты сборки/пакетирования
└── docs/                 # Доп. документация
```

## Лицензия

Проект для внутреннего использования.
