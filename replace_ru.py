import os

files = [
    "internal/gui/graphics/virtual_keyboard.go",
    "internal/gui/graphics/virtual_keyboard_desktop.go",
    "internal/gui/graphics/virtual_keyboard_mobile.go"
]

replacements = {
    # virtual_keyboard.go
    "// VirtualKeyboard виртуальная клавиатура для полноэкранного режима": "// VirtualKeyboard virtual keyboard for fullscreen mode",
    "// Отправка каждого символа на хост": "// Sending each character to the host",
    "// Состояние модификаторов": "// Modifier state",
    "// Кнопки модификаторов для обновления стиля": "// Modifier buttons for style updates",
    "// Платформозависимые поля: на десктопе используются заглушки типов (определены в _desktop.go),": "// Platform-dependent fields: dummy types are used on desktop (defined in _desktop.go),",
    "// на мобилках — реальные реализации (определены в _mobile.go)": "// real implementations on mobile (defined in _mobile.go)",
    "// Динамический отступ снизу для мобильного IME": "// Dynamic bottom padding for mobile IME",
    "// NewVirtualKeyboard создает новую виртуальную клавиатуру.": "// NewVirtualKeyboard creates a new virtual keyboard.",
    "// onRuneTyped — опциональный колбэк для немедленной отправки каждого символа на хост (Android/iOS).": "// onRuneTyped is an optional callback for sending each character to the host immediately (Android/iOS).",
    "// createKeyboard создает интерфейс клавиатуры": "// createKeyboard creates the keyboard interface",
    "// Создаем кнопку переключения видимости (всегда видимая в углу)": "// Create the visibility toggle button (always visible in the corner)",
    "// Делаем кнопку заметной": "// Make the button noticeable",
    "// Вызываем платформозависимую реализацию раскладки (через build tags)": "// Call the platform-dependent layout implementation (via build tags)",
    "// Скрываем по умолчанию": "// Hide by default",
    "// Создаем контейнер для позиционирования": "// Create container for positioning",
    "// Добавляем кнопку переключения в правый нижний угол": "// Add toggle button to the bottom right corner",
    "// Добавляем клавиатуру по центру": "// Add keyboard to the center",
    "// createKey создает кнопку клавиши (общее для мобилки и десктопа)": "// createKey creates a key button (common for mobile and desktop)",
    "// Устанавливаем размеры клавиш по умолчанию": "// Set default key sizes",
    "// Делаем пробел шире": "// Make spacebar wider",
    "// Делаем специальные клавиши шире": "// Make special keys wider",
    "// Делаем Enter шире": "// Make Enter wider",
    "// Делаем Backspace шире": "// Make Backspace wider",
    "// Делаем функциональные клавиши меньше": "// Make function keys smaller",
    "// Делаем Escape меньше": "// Make Escape smaller",
    "// Делаем Windows и Menu клавиши среднего размера": "// Make Windows and Menu keys medium size",
    "// createModifierKey создает кнопку модификатора с переключением": "// createModifierKey creates a modifier button with toggle",
    "// toggleModifier переключает состояние модификатора": "// toggleModifier toggles modifier state",
    "// updateModifierButton обновляет внешний вид кнопки модификатора": "// updateModifierButton updates the modifier button appearance",
    "// handleKeyPress обрабатывает нажатие клавиши": "// handleKeyPress handles key press",
    "// toggleVisibility переключает видимость клавиатуры": "// toggleVisibility toggles keyboard visibility",
    "// Show показывает клавиатуру": "// Show shows the keyboard",
    "// На мобильных платформах (Android/iOS) клавиатура обычно встроена в BorderLayout": "// On mobile platforms (Android/iOS), the keyboard is usually embedded in the BorderLayout",
    "// основного окна через FullscreenUI. Ручное позиционирование здесь приведет к наложению на видео.": "// of the main window via FullscreenUI. Manual positioning here will lead to overlapping with the video.",
    "// Поэтому выполняем ручной Move/Resize только если мы НЕ на мобилке или если окно отдельное.": "// Therefore, we perform manual Move/Resize only if we are NOT on mobile or if the window is separate.",
    "// ShowInSeparateWindow показывает клавиатуру в отдельном окне": "// ShowInSeparateWindow shows the keyboard in a separate window",
    "// Hide скрывает клавиатуру": "// Hide hides the keyboard",
    "// Сбрасываем IME-отступ (метод-заглушка на десктопе, реальный на мобилках)": "// Reset IME padding (dummy method on desktop, real one on mobile)",
    "// IsVisible возвращает состояние видимости": "// IsVisible returns visibility state",
    "// GetContainer возвращает контейнер клавиатуры": "// GetContainer returns keyboard container",
    "// GetKeyboardLayout возвращает только layout клавиатуры": "// GetKeyboardLayout returns only keyboard layout",
    "// UpdatePosition обновляет позицию элементов клавиатуры": "// UpdatePosition updates keyboard elements position",
    "// SetVisibleState устанавливает состояние видимости без показа отдельного окна": "// SetVisibleState sets visibility state without showing a separate window",

    # virtual_keyboard_desktop.go
    "// Константы сетки клавиатуры (как у аппаратной): ширина/высота одной «единицы» клавиши": "// Keyboard grid constants (like hardware): width/height of one key \"unit\"",
    "// Ширина контента самого длинного ряда в единицах (ряд 1 и 4: 15)": "// Content width of the longest row in units (row 1 and 4: 15)",
    "// Левый отступ: центрируем контент в сетке, чтобы кнопки не прижимались к левому краю": "// Left margin: center content in the grid so buttons do not press against the left edge",
    "// centerKeyboardLayout центрирует содержимое клавиатуры при изменении размера области": "// centerKeyboardLayout centers the keyboard content when the area size changes",
    "// Заглушки типов для десктопа": "// Dummy types for desktop",
    "// Заглушки методов для десктопа": "// Dummy methods for desktop",
    "// placeKey размещает кнопку в сетке: row/col в «единицах» клавиши, widthUnits — ширина в единицах (1 = обычная клавиша).": "// placeKey places a button in the grid: row/col in key \"units\", widthUnits is the width in units (1 = normal key).",
    "// placeInvisiblePlaceholder добавляет невидимый прямоугольник (цвет фона) в сетку — для выравнивания столбцов": "// placeInvisiblePlaceholder adds an invisible rectangle (background color) to the grid - for column alignment",
    "// createKeyboardLayout создает раскладку клавиатуры для десктопа": "// createKeyboardLayout creates the desktop keyboard layout",
    "// GetLastIMEH заглушка для десктопа": "// GetLastIMEH dummy for desktop",
    "// Ряд 0: Esc, F1–F12, Del": "// Row 0: Esc, F1-F12, Del",
    "// Ряд 1: ` 1 2 3 4 5 6 7 8 9 0 - = Backspace(2)": "// Row 1: ` 1 2 3 4 5 6 7 8 9 0 - = Backspace(2)",
    "// Ряд 2: Tab(1.5) Q W E R T Y U I O P [ ] \ ": "// Row 2: Tab(1.5) Q W E R T Y U I O P [ ] \ ",
    "// Ряд 3: Caps(1.75) A S D F G H J K L ; ' Enter(2.25)": "// Row 3: Caps(1.75) A S D F G H J K L ; ' Enter(2.25)",
    "// Ряд 4: Shift(1.5) Z X C V B N M , . / Shift(1.5) ↑ [невидимая]": "// Row 4: Shift(1.5) Z X C V B N M , . / Shift(1.5) ↑ [invisible]",
    "// Ряд 5: Ctrl(1.25) Win(1.25) Alt(1.25) Space(3.5) Alt Win Menu Ctrl ← ↓ →": "// Row 5: Ctrl(1.25) Win(1.25) Alt(1.25) Space(3.5) Alt Win Menu Ctrl ← ↓ →",

    # virtual_keyboard_mobile.go
    "// activeIMEKeyboardMu защищает activeIMEKeyboardTarget от гонок.": "// activeIMEKeyboardMu protects activeIMEKeyboardTarget from races.",
    "// RegisterAsIMETarget регистрируется в keyboard_ime_android.go": "// RegisterAsIMETarget registers in keyboard_ime_android.go",
    "// backspaceEntry — поле ввода для Android/iOS, которое позволяет ловить системные клавиши": "// backspaceEntry is an input field for Android/iOS that allows catching system keys",
    "// вызывается когда поле получает фокус (IME откроется)": "// called when the field gains focus (IME will open)",
    "// вызывается когда поле теряет фокус (IME закроется)": "// called when the field loses focus (IME will close)",
    "// imeSpacerLayout — layout с динамической высотой для отступа под IME": "// imeSpacerLayout is a layout with dynamic height for IME padding",
    "// createKeyboardLayout создает раскладку клавиатуры для мобильных устройств": "// createKeyboardLayout creates keyboard layout for mobile devices",
    "// Всё состояние под одним мьютексом — OnChanged вызывается из разных goroutine на Android.": "// All state under one mutex - OnChanged is called from different goroutines on Android.",
    "// Фоновый worker: все сетевые вызовы здесь, UI-поток никогда не блокируется.": "// Background worker: all network calls here, UI thread is never blocked.",
    "// enqueueDiff вычисляет diff от prevText к target и кладёт задачу в netChan.": "// enqueueDiff calculates the diff from prevText to target and puts a task into netChan.",
    "// Вызывать строго под mu; сам освобождает mu перед отправкой в канал.": "// Call strictly under mu; it releases mu itself before sending to the channel.",
    "// commitChanges сбрасывает буфер: берёт mu, вычисляет diff, отпускает.": "// commitChanges flushes the buffer: acquires mu, calculates diff, releases.",
    "// Быстрый путь: простой набор — шлём diff сразу без таймера.": "// Fast path: simple typing - send diff immediately without timer.",
    "// Медленный путь: IME заменяет слово (автозамена, autocomplete).": "// Slow path: IME replaces a word (autocorrect, autocomplete).",
    "// Ждём стабилизации 20ms.": "// Waiting 20ms for stabilization.",
    "// Очистка буфера при переполнении (100+ рун).": "// Buffer cleanup on overflow (100+ runes).",
    "// Мы НЕ сбрасываем отступ в onUnfocused (adjustForIME(false)),": "// We do NOT reset the padding in onUnfocused (adjustForIME(false)),",
    "// так как на Android системный навигационный бар все еще занимает место.": "// because on Android the system navigation bar still takes up space.",
    "// Мы доверяем событиям KeyboardBridge.onIMEHeightChanged, которые приходят": "// We rely on KeyboardBridge.onIMEHeightChanged events that come",
    "// от Android при скрытии клавиатуры и содержат актуальную высоту (например, только NavBar).": "// from Android when hiding the keyboard and contain the actual height (e.g. just NavBar).",
    "// FocusInput запрашивает фокус у строки ввода Android-клавиатуры": "// FocusInput requests focus on the Android keyboard input field",
    "// BlurInput снимает фокус со строки ввода": "// BlurInput removes focus from the input field",
    "// SetOnIMEChanged устанавливает callback": "// SetOnIMEChanged sets the callback",
    "// setIMEOffset выставляет точный нижний отступ": "// setIMEOffset sets the exact bottom padding",
    "Fyne-единиц": "Fyne units",
    "// adjustForIME — запасной путь": "// adjustForIME - fallback path",
    "// Устанавливаем минимальный начальный отступ, пока не пришло реальное значение от JNI": "// Set minimal initial padding until the real value comes from JNI",
    "// ResetIMEState сбрасывает отступ IME": "// ResetIMEState resets the IME padding",
    "принудительный сброс отступа (canvas вырос — IME закрыта)": "forced padding reset (canvas grew - IME is closed)",
}

for file_path in files:
    with open(file_path, "r", encoding="utf-8") as f:
        content = f.read()
    
    for ru, en in replacements.items():
        content = content.replace(ru, en)
        
    with open(file_path, "w", encoding="utf-8") as f:
        f.write(content)

