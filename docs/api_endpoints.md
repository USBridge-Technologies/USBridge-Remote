# Актуальные API Endpoints USBridge 2

## Системные endpoints

### Статус системы
```http
GET /api/status
```
**Описание**: Получить общий статус всех компонентов системы
**Ответ**:
```json
{
  "success": true,
  "message": "Статус системы получен",
  "data": {
    "service": {
      "status": "running",
      "timestamp": "2024-01-15T10:30:00Z",
      "uptime": "2h 15m 30s"
    },
    "nbd": {
      "connected": true,
      "device": "/dev/nbd2",
      "server": "192.168.1.107",
      "port": 10809,
      "export": "test_system"
    },
    "usb": {
      "connected": true,
      "gadget_name": "radxa-cdrom",
      "udc_name": "fcc00000.dwc3",
      "vendor_id": "0x1d6b",
      "product_id": "0x0104",
      "keyboard_enabled": true
    },
    "kernel": {
      "modules_loaded": true,
      "modules": ["configfs", "libcomposite", "nbd"]
    },
    "timestamp": "2024-01-15T10:30:00Z"
  }
}
```

### Статус сервиса
```http
GET /api/service/status
```
**Описание**: Получить детальный статус USB gadget сервиса
**Ответ**:
```json
{
  "success": true,
  "message": "Статус сервиса получен",
  "data": {
    "status": "running",
    "timestamp": "2024-01-15T10:30:00Z",
    "uptime": "2h 15m 30s",
    "gadget_connected": true,
    "nbd_connected": true,
    "video_streaming": true
  }
}
```

## Управление сервисом

### Запуск сервиса
```http
POST /api/service/start
```
**Описание**: Запустить USB gadget и подключить все компоненты
**Ответ**:
```json
{
  "success": true,
  "message": "Сервис успешно запущен"
}
```

### Остановка сервиса
```http
POST /api/service/stop
```
**Описание**: Остановить USB gadget и отключить все компоненты
**Ответ**:
```json
{
  "success": true,
  "message": "Сервис успешно остановлен"
}
```

### Перезапуск сервиса
```http
POST /api/service/restart
```
**Описание**: Перезапустить USB gadget
**Ответ**:
```json
{
  "success": true,
  "message": "Сервис успешно перезапущен"
}
```

## Управление устройствами

### Список устройств
```http
GET /api/devices
```
**Описание**: Получить список всех подключенных устройств
**Ответ**:
```json
{
  "success": true,
  "message": "Список устройств получен",
  "data": [
    {
      "name": "NBD Device",
      "path": "/dev/nbd2",
      "connected": true,
      "description": "NBD подключение к 192.168.1.107:10809"
    },
    {
      "name": "Video Device",
      "path": "/dev/video0",
      "connected": true,
      "description": "UVC камера"
    },
    {
      "name": "USB Gadget",
      "path": "/sys/kernel/config/usb_gadget/radxa-cdrom",
      "connected": true,
      "description": "USB OTG gadget"
    }
  ]
}
```

## Конфигурация

### Получить конфигурацию
```http
GET /api/config
```
**Ответ**:
```json
{
  "success": true,
  "message": "Конфигурация получена",
  "data": {
    "nbd_device": "/dev/nbd2",
    "nbd_server": "192.168.1.107",
    "nbd_port": 10809,
    "export_name": "test_system",
    "gadget_name": "radxa-cdrom",
    "udc_name": "fcc00000.dwc3",
    "vendor_id": "0x1d6b",
    "product_id": "0x0104",
    "product_name": "Radxa NBD CD-ROM + Keyboard + Video (Hardware)",
    "manufacturer": "Radxa",
    "keyboard_enabled": true,
    "video_enabled": true,
    "video_device": "/dev/video0",
    "video_width": 640,
    "video_height": 480,
    "video_fps": 30,
    "video_quality": 80,
    "video_codec": "hevc_v4l2m2m",
    "video_bitrate": "2M",
    "video_pixel_format": "yuyv422",
    "video_buffer_size": 2,
    "video_stream_format": "mjpeg",
    "video_low_latency": true,
    "web_server": {
      "enabled": true,
      "host": "0.0.0.0",
      "port": 8080
    },
    "check_interval": 30
  }
}
```

### Обновить конфигурацию
```http
POST /api/config
```
**Тело запроса**:
```json
{
  "nbd_server": "192.168.1.108",
  "video_width": 1280,
  "video_height": 720,
  "video_fps": 15
}
```
**Ответ**:
```json
{
  "success": true,
  "message": "Конфигурация обновлена"
}
```

## Управление клавиатурой

### Отправка клавиш
```http
POST /api/keyboard
```
**Тело запроса**:

**Одна клавиша**:
```json
{
  "action": "key",
  "key_code": 40
}
```

**Комбинация клавиш**:
```json
{
  "action": "combo",
  "modifiers": 5,
  "key_code": 76
}
```

**Отправка текста**:
```json
{
  "action": "text",
  "text": "Hello World!"
}
```

**Ответ**:
```json
{
  "success": true,
  "message": "Команда отправлена"
}
```

## Управление мышью / тачем (HID)

### Единый endpoint

```http
POST /api/mouse
Content-Type: application/json
```

Сервер принимает JSON-объект формата `MouseRequest` (см. `internal/models/usb.go`).

#### Поля запроса

- **`action`** *(string, обязателен)*:
  - **`"move"`** — относительное перемещение мыши (тачпад/обычная мышь)
  - **`"click"`** — клик мыши
  - **`"scroll"`** — прокрутка колеса
  - **`"action"`** — комплексное действие (обычно используется для drag: кнопка + dx/dy + scroll)
  - **`"touch"`** — тачскрин: касание/отпускание в абсолютных координатах
  - **`"touch_position"`** — установка абсолютной позиции указателя без касания (используется для правого клика в режиме тача и для режима **absolute**)

- **`dx`, `dy`** *(int)*: используются для `action="move"` / `action="action"`.
  - Диапазон: **-127..127** (HID)

- **`x`, `y`** *(int)*: используются для `action="touch"` / `action="touch_position"`.
  - Диапазон: **0..4095** (как реализовано в клиенте сейчас; см. `VideoWidget.PositionToAbsolute`)

- **`tip`** *(bool, обязателен в JSON как поле; в модели без `omitempty`)*:
  - Для `action="touch"`: **`true` = касание (down)**, **`false` = отпускание (up)**
  - Для `action="touch_position"`: клиент обычно шлёт `false` (позиция без касания)

- **`button`** *(int)*: используется для `action="click"` / `action="action"`.
  - 1 = левая, 2 = правая, 3 = средняя

- **`scroll`** *(int)*: используется для `action="scroll"` / `action="action"`.
  - Диапазон: **-127..127**

#### Ответ

```json
{
  "success": true,
  "message": "Команда отправлена"
}
```

### Примеры

#### 1) Тачпад/обычная мышь (относительное перемещение)

```json
{
  "action": "move",
  "dx": 12,
  "dy": -4,
  "tip": false
}
```

#### 2) Клик

```json
{
  "action": "click",
  "button": 1,
  "tip": false
}
```

#### 3) Тачскрин (касание)

```json
{
  "action": "touch",
  "x": 2048,
  "y": 1024,
  "tip": true
}
```

Отпускание:

```json
{
  "action": "touch",
  "x": 2048,
  "y": 1024,
  "tip": false
}
```

#### 4) Абсолютное позиционирование без касания (режим `absolute`)

```json
{
  "action": "touch_position",
  "x": 3500,
  "y": 500,
  "tip": false
}
```

### Режимы манипулятора на стороне клиента (важно для `/api/device/start`)

При запуске устройства `"mouse"` через `/api/device/start` клиент может передать `type`:

- **`"mouse"`** — тачпад/обычная мышь (сервер ожидает `action="move"` с `dx/dy`)
- **`"touchscreen"`** — тачскрин (сервер ожидает `action="touch"` с `x/y` и `tip`)
- **`"absolute"`** — абсолютный режим (сервер ожидает `action="touch_position"` с `x/y`, клики идут отдельными `action="click"`)

#### Рекомендация для сервера (чтобы было «как VirtualBox»)

Чтобы поведение было максимально близким к VirtualBox USB Tablet / absolute pointer, серверу стоит принимать **атомарное абсолютное событие**,
в котором вместе приходят:

- абсолютные координаты `x/y`
- состояние кнопок (`buttonState` битмаской или отдельные down/up)
- колесо (`dz`/`dw` или хотя бы `scroll`)

Варианты:

1) **Расширить существующий `/api/mouse`**:
   - добавить новый `action: "absolute_event"` с полями `x,y,button_state,scroll`
   - или расширить `touch_position`, добавив `button_state` и `scroll` (и интерпретировать их только в режиме `type="absolute"`)

2) **Диапазон координат**:
   - принять нормализацию `0..65535` (0..0xffff) для `absolute` (часто используемый диапазон для tablet/absolute HID)
   - либо чётко документировать другой диапазон и выставить соответствующие логические максимумы в HID-репорте

С текущим API, где позиция (`touch_position`) и кнопки/колесо (`click`/`scroll`) приходят раздельно, возможна микроскопическая рассинхронизация
между положением и кнопкой/колесом (VirtualBox исторически упоминает такую проблему при разделении позиции и кнопок).

## Видео управление

### Информация о видео
```http
GET /api/video/info
```
**Ответ**:
```json
{
  "success": true,
  "message": "Информация о видео получена",
  "data": {
    "enabled": true,
    "device": "/dev/video0",
    "width": 640,
    "height": 480,
    "fps": 30,
    "quality": 80,
    "codec": "hevc_v4l2m2m",
    "bitrate": "2M",
    "pixel_format": "yuyv422",
    "buffer_size": 2,
    "stream_format": "mjpeg",
    "low_latency": true,
    "streaming": true,
    "clients_count": 2
  }
}
```

### Запуск видео стриминга
```http
POST /api/video/start
```
**Описание**: Запустить видео стриминг
**Ответ**:
```json
{
  "success": true,
  "message": "Видео стриминг запущен"
}
```

### Остановка видео стриминга
```http
POST /api/video/stop
```
**Описание**: Остановить видео стриминг
**Ответ**:
```json
{
  "success": true,
  "message": "Видео стриминг остановлен"
}
```

## Веб-интерфейс

### Главная страница
```http
GET /
```
**Описание**: Веб-интерфейс для управления устройством
**Ответ**: HTML страница с интерфейсом управления

## Коды клавиш (HID)

### Основные клавиши
- `a-z`: 4-29
- `0-9`: 30-39
- `Space`: 44
- `Enter`: 40
- `Escape`: 41
- `Backspace`: 42
- `Tab`: 43

### Функциональные клавиши
- `F1-F12`: 58-69

### Модификаторы
- `Left Ctrl`: 1
- `Left Shift`: 2
- `Left Alt`: 4
- `Left GUI`: 8

### Примеры комбинаций
- `Ctrl+C`: `{"action": "combo", "modifiers": 1, "key_code": 6}`
- `Ctrl+Alt+Del`: `{"action": "combo", "modifiers": 5, "key_code": 76}`

## Формат ошибок

```json
{
  "success": false,
  "error": "service_error",
  "message": "Ошибка запуска сервиса",
  "details": "USB gadget уже подключен"
}
```

## Коды ответов

- `200` - Успешно
- `400` - Неверный запрос
- `405` - Метод не поддерживается
- `500` - Внутренняя ошибка сервера

Это актуальные endpoints из реальной реализации USBridge 2.
