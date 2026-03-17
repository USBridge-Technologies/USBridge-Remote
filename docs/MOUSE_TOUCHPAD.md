# Управление указателем: touchpad / touchscreen / absolute

## Описание

Клиент умеет управлять указателем на удалённой машине поверх видео тремя режимами:

- **`mouse` (touchpad / относительный)** — движение передаётся как `dx/dy` (HID mouse).
- **`touchscreen` (touchscreen / абсолютный + касание)** — движение и клики передаются как `x/y` + `tip` (HID touchscreen).
- **`absolute` (absolute / абсолютный без касания)** — позиция передаётся как `x/y` без касания; клики — отдельными мышиными кликами.

Выбор режима делается в строке **Manipulator** в списке устройств (экран `Devices`) и уходит в `/api/device/start` в поле `type`.

## Возможности

### Desktop (мышь)
- **Перемещение курсора**:
  - `mouse`: относительное (`dx/dy`)
  - `absolute`: абсолютное позиционирование (`x/y`, без касания)
- **Клик левой кнопкой**: обычный клик мыши
- **Клик правой кнопкой**: правый клик для контекстного меню
- **Drag & Drop**: зажатие кнопки и перемещение
- **Прокрутка**: колесико мыши

### Mobile (сенсорный экран)
- **Перемещение курсора**:
  - `touchscreen`: движение пальцем = касание/перетаскивание (`touch` + `tip`)
  - `mouse`: свайп = относительное перемещение (`dx/dy`)
  - `absolute`: движение пальцем = абсолютная позиция (`touch_position`, без касания)
- **Клик**: короткое касание (тап)
- **Drag**: долгое касание и перемещение

## Архитектура

### Файлы

1. **internal/models/usb.go**
   - `MouseRequest` - модель для запросов к API мыши
   - Действия: `move`, `click`, `scroll`, `action`, `touch`, `touch_position`

2. **internal/api/usb_client.go**
   - `SendMouseMove(dx, dy)` - перемещение курсора
   - `SendMouseClick(button)` - клик кнопкой (1=левая, 2=правая, 3=средняя)
   - `SendMouseScroll(scroll)` - прокрутка колесика
   - `SendMouseAction(button, dx, dy, scroll)` - комплексное действие
   - `SendTouch(x, y, tip)` - касание тачскрина (down/up)
   - `SendTouchPositionOnly(x, y, tip)` - абсолютная позиция без касания

3. **internal/ui/video_mouse_handler.go**
   - `TouchpadWrapper` - обертка для video canvas
   - Реализует интерфейсы:
     - `fyne.Tappable` - для кликов
     - `desktop.Hoverable` - для mouse events
     - `desktop.Mouseable` - для drag & drop
     - `fyne.Scrollable` - для прокрутки
     - `mobile.Touchable` - для touch events

4. **internal/ui/video_widget.go**
   - Добавлено поле `isMouseConnected` - флаг подключенной мыши
   - `checkMouseConnected()` - проверка статуса мыши через API
   - Интеграция TouchpadWrapper в videoContainer
   - `UpdateTouchpadAndContentRect` + `PositionToAbsolute` — перевод координат из области виджета в абсолютные `0..4095` с учётом `ImageFillContain` (letterbox)

## Логика работы

## Режимы и что уходит в API

Ниже — **реальное поведение клиента** (см. `internal/ui/video_mouse_handler.go` и `internal/api/usb_client.go`).

### 1) `mouse` (touchpad, относительный)

- **Движение**: `POST /api/mouse` `{ "action":"move", "dx":..., "dy":... }`
- **Клики**: `{ "action":"click", "button":1|2|3 }`
- **Скролл**: `{ "action":"scroll", "scroll":... }`
- **Drag**: на desktop может идти через polling и серию `move` при зажатой кнопке (клик/удержание реализованы на клиенте отдельной логикой).

### 2) `touchscreen` (touchscreen, абсолютный + касание)

- **Короткий тап**: серия `touch`:
  - down: `{ "action":"touch", "x":..., "y":..., "tip":true }`
  - up: `{ "action":"touch", "x":..., "y":..., "tip":false }`
- **Перетаскивание**: во время движения шлются `touch` с `tip:true`, на отпускании — `tip:false`.
- **Правый клик**: чтобы не получить «двойной левый», клиент делает:
  - сначала позицию: `{ "action":"touch_position", "x":..., "y":..., "tip":false }`
  - потом клик: `{ "action":"click", "button":2 }`

### 3) `absolute` (absolute, абсолютный без касания)

- **Движение**: `POST /api/mouse` `{ "action":"touch_position", "x":..., "y":..., "tip":false }`
  - Это **абсолютное позиционирование указателя** без “касания” (аналог “absolute mouse/tablet”).
- **Клики**: как мышь — `{ "action":"click", "button":... }`
- **Скролл**: как мышь — `{ "action":"scroll", "scroll":... }`

Важно: в `absolute` **не отправляется** `action:"touch"` (нет `tip:true/false` как на тачскрине).

### Проверка подключения мыши

```go
func (vw *VideoWidget) checkMouseConnected() {
    deviceInfo := vw.usbClient.GetDeviceInfo()
    for device in deviceInfo.Devices {
        if device.Type == "mouse" && device.Status == "connected" {
            vw.isMouseConnected = true
        }
    }
}
```

Проверка выполняется при каждом обновлении виджета (Refresh).

### Обработка событий мыши (Desktop)

1. **MouseMoved** - вычисляет смещение dx/dy относительно последней позиции
2. **MouseDown** - устанавливает флаг isDragging и отправляет нажатие кнопки
3. **MouseUp** - сбрасывает isDragging и отправляет отпускание кнопки
4. **Scrolled** - нормализует значение прокрутки и отправляет на сервер

### Обработка touch событий (Mobile)

1. **TouchDown** - сохраняет начальную позицию и время
2. **TouchMove** - вычисляет смещение и отправляет как перемещение мыши
3. **TouchUp** - определяет тип жеста:
   - Если малое перемещение (<10px) и быстро (<300ms) = клик
   - Иначе = завершение свайпа

### Ограничения значений

Все значения смещения и прокрутки ограничены диапазоном **-127..127** согласно спецификации HID.

```go
func clamp(value, min, max int) int {
    if value < min { return min }
    if value > max { return max }
    return value
}
```

## API запросы

### Примеры

**Перемещение мыши:**
```json
POST /api/mouse
{
  "action": "move",
  "dx": 10,
  "dy": -5
}
```

**Абсолютное позиционирование (без касания):**
```json
POST /api/mouse
{
  "action": "touch_position",
  "x": 3500,
  "y": 500,
  "tip": false
}
```

**Клик левой кнопкой:**
```json
POST /api/mouse
{
  "action": "click",
  "button": 1
}
```

**Прокрутка:**
```json
POST /api/mouse
{
  "action": "scroll",
  "scroll": -5
}
```

**Drag (перемещение с зажатой кнопкой):**
```json
POST /api/mouse
{
  "action": "action",
  "button": 1,
  "dx": 50,
  "dy": 20,
  "scroll": 0
}
```

## Настройка чувствительности

Чувствительность можно настроить изменением коэффициентов:

```go
// Для desktop мыши - коэффициент 1:1
dx := int(ev.Position.X - t.videoWidget.lastMouseX)
dy := int(ev.Position.Y - t.videoWidget.lastMouseY)

// Для touch можно добавить коэффициент, например 0.5 для меньшей чувствительности:
dx := int((ev.Position.X - t.videoWidget.lastMouseX) * 0.5)
dy := int((ev.Position.Y - t.videoWidget.lastMouseY) * 0.5)
```

## Отладка

Логирование включено для всех событий:
- `🖱️ MouseMoved: dx=10, dy=-5` - перемещение мыши
- `🖱️ Tapped at: (100, 200)` - тап на экране
- `🖱️ Мышь подключена: USBridge Mouse` - статус подключения
- `🖱️ Тачпад активирован` - включение тачпада

Уровень логирования: Debug

## Тестирование

### Desktop
1. Запустить клиент
2. Подключить мышь через DiskWidget
3. Запустить видео
4. Двигать мышь над областью видео - курсор должен двигаться на удаленной машине
5. Кликать - должны выполняться клики
6. Прокручивать колесико - должна работать прокрутка

### Android
1. Собрать APK
2. Установить на устройство
3. Подключить мышь
4. Запустить видео
5. Свайпить пальцем - курсор должен двигаться
6. Тапать - должны выполняться клики

## Возможные улучшения

1. **Настройки чувствительности** - UI для изменения коэффициентов
2. **Жесты** - двойной тап, two-finger scroll
3. **Акселерация** - изменение скорости в зависимости от скорости движения
4. **Визуальный курсор** - отображение позиции курсора на видео
5. **Калибровка** - автоматическая подстройка чувствительности
