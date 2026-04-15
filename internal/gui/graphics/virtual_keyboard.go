package graphics

import (
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/input"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// Константы сетки клавиатуры (как у аппаратной): ширина/высота одной «единицы» клавиши
const (
	keyUnitW  = 30
	keyUnitH  = 28
	keyGap    = 2
	keyboardW = 600
	keyboardH = 180
	// Ширина контента самого длинного ряда в единицах (ряд 1 и 4: 15)
	keyboardContentUnits = 15
)

// Левый отступ: центрируем контент в сетке, чтобы кнопки не прижимались к левому краю
var keyboardLeftMargin = float32((keyboardW - keyboardContentUnits*keyUnitW) / 2)

func hasRunePrefix(value []rune, prefix []rune) bool {
	if len(prefix) > len(value) {
		return false
	}
	for i := range prefix {
		if value[i] != prefix[i] {
			return false
		}
	}
	return true
}

// centerKeyboardLayout центрирует содержимое клавиатуры при изменении размера области
type centerKeyboardLayout struct {
	width  float32
	height float32
}

func (c *centerKeyboardLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x := (size.Width - c.width) / 2
	if x < 0 {
		x = 0
	}
	y := (size.Height - c.height) / 2
	if y < 0 {
		y = 0
	}
	for _, o := range objects {
		o.Move(fyne.NewPos(x, y))
		o.Resize(fyne.NewSize(c.width, c.height))
	}
}

func (c *centerKeyboardLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(c.width, c.height)
}

// backspaceEntry — поле ввода для Android, которое позволяет ловить системные клавиши
type backspaceEntry struct {
	widget.Entry
	onKey       func(fyne.KeyName)
	onFocused   func() // вызывается когда поле получает фокус (IME откроется)
	onUnfocused func() // вызывается когда поле теряет фокус (IME закроется)
}

func (e *backspaceEntry) TypedKey(key *fyne.KeyEvent) {
	if e.onKey != nil {
		e.onKey(key.Name)
	}
	e.Entry.TypedKey(key)
}

func (e *backspaceEntry) TypedRune(r rune) {
	e.Entry.TypedRune(r)
}

func (e *backspaceEntry) FocusGained() {
	e.Entry.FocusGained()
	if e.onFocused != nil {
		e.onFocused()
	}
}

func (e *backspaceEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.onUnfocused != nil {
		e.onUnfocused()
	}
}

// imeSpacerLayout — layout с динамической высотой для отступа под Android IME
type imeSpacerLayout struct {
	height float32
}

func (l *imeSpacerLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

func (l *imeSpacerLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, l.height)
}

// VirtualKeyboard виртуальная клавиатура для полноэкранного режима
type VirtualKeyboard struct {
	container      *fyne.Container
	keyboard       *fyne.Container
	toggleBtn      *widget.Button
	isVisible      bool
	onKeyPress     func(keyCode int, modifiers int)
	onRuneTyped    func(r rune) // Отправка каждого символа на хост (Android: поле ввода)
	parentWindow   fyne.Window
	keyboardWindow fyne.Window

	// Состояние модификаторов
	ctrlPressed     bool
	altPressed      bool
	shiftPressed    bool
	capsLockPressed bool
	winPressed      bool

	// Кнопки модификаторов для обновления стиля
	ctrlBtn     *widget.Button
	altBtn      *widget.Button
	shiftBtn    *widget.Button
	capsLockBtn *widget.Button
	winBtn      *widget.Button
	mobileInput *backspaceEntry

	// Android IME — динамический отступ снизу, чтобы наша клавиатура оставалась над системной
	imeSpacer     *imeSpacerLayout
	imeSpacerCont *fyne.Container
	onIMEChanged  func(open bool) // callback: родитель должен обновить layout
}

// NewVirtualKeyboard создает новую виртуальную клавиатуру.
// onRuneTyped — опциональный колбэк для немедленной отправки каждого символа на хост (Android).
func NewVirtualKeyboard(parentWindow fyne.Window, onKeyPress func(int, int), onRuneTyped func(r rune)) *VirtualKeyboard {
	vk := &VirtualKeyboard{
		isVisible:    false,
		onKeyPress:   onKeyPress,
		onRuneTyped:  onRuneTyped,
		parentWindow: parentWindow,
	}

	vk.createKeyboard()
	return vk
}

// createKeyboard создает интерфейс клавиатуры
func (vk *VirtualKeyboard) createKeyboard() {
	// Создаем кнопку переключения видимости (всегда видимая в углу)
	vk.toggleBtn = widget.NewButton("⌨", vk.toggleVisibility)
	vk.toggleBtn.Importance = widget.HighImportance // Делаем кнопку заметной

	// Создаем основную клавиатуру
	vk.keyboard = vk.createKeyboardLayout()
	vk.keyboard.Hide() // Скрываем по умолчанию

	// Создаем контейнер для позиционирования
	vk.container = container.NewWithoutLayout()

	// Добавляем кнопку переключения в правый нижний угол
	vk.container.Add(vk.toggleBtn)

	// Добавляем клавиатуру по центру
	vk.container.Add(vk.keyboard)
}

// placeKey размещает кнопку в сетке: row/col в «единицах» клавиши, widthUnits — ширина в единицах (1 = обычная клавиша).
// keyboardLeftMargin центрирует контент в сетке, чтобы кнопки не прижимались к левому краю.
func (vk *VirtualKeyboard) placeKey(grid *fyne.Container, btn *widget.Button, row int, col float32, widthUnits float32) {
	x := keyboardLeftMargin + col*keyUnitW + keyGap/2
	y := float32(row)*keyUnitH + keyGap/2
	w := widthUnits*keyUnitW - keyGap
	h := keyUnitH - keyGap
	btn.Resize(fyne.NewSize(w, float32(h)))
	btn.Move(fyne.NewPos(x, y))
	grid.Add(btn)
}

// placeInvisiblePlaceholder добавляет невидимый прямоугольник (цвет фона) в сетку — для выравнивания столбцов (например, стрелка ↑ в столбик со стрелкой →).
func (vk *VirtualKeyboard) placeInvisiblePlaceholder(grid *fyne.Container, row int, col float32) {
	x := keyboardLeftMargin + col*keyUnitW + keyGap/2
	y := float32(row)*keyUnitH + keyGap/2
	w := keyUnitW - keyGap
	h := keyUnitH - keyGap
	rect := canvas.NewRectangle(theme.BackgroundColor())
	rect.Resize(fyne.NewSize(float32(w), float32(h)))
	rect.Move(fyne.NewPos(x, y))
	grid.Add(rect)
}

// createKeyboardLayout создает раскладку клавиатуры: на десктопе — фиксированная сетка как у аппаратной, на Android — компактная панель (системная клавиатура + Esc/Tab/Shift/Ctrl/Alt/Del/F).
func (vk *VirtualKeyboard) createKeyboardLayout() *fyne.Container {
	isMobile := fyne.CurrentDevice().IsMobile()
	if isMobile {
		return vk.createKeyboardLayoutAndroid()
	}
	return vk.createKeyboardLayoutDesktop()
}

func (vk *VirtualKeyboard) createKeyboardLayoutAndroid() *fyne.Container {
	// Поле ввода: при нажатии открывается системная клавиатура Android.
	textHint := &backspaceEntry{}
	textHint.Password = true // Отключает Т9 и автозамену
	textHint.ExtendBaseWidget(textHint)
	vk.mobileInput = textHint

	var prevText string
	var suppress bool

	textHint.onKey = func(keyName fyne.KeyName) {
		// Обрабатываем Enter и Tab напрямую, так как они не меняют текст в OnChanged
		if keyName == fyne.KeyReturn || keyName == fyne.KeyTab {
			code := input.GetKeyCode(keyName)
			if code != 0 && vk.onKeyPress != nil {
				vk.onKeyPress(code, 0)
			}
		} else if keyName == fyne.KeyBackspace {
			// Если поле пустое, OnChanged не сработает (текст не изменится), 
			// поэтому отправляем Backspace напрямую отсюда.
			if textHint.Text == "" && vk.onKeyPress != nil {
				vk.onKeyPress(42, 0) // HID Backspace
			}
		}
	}

	textHint.OnChanged = func(newText string) {
		if suppress {
			return
		}

		newRunes := []rune(newText)
		prevRunes := []rune(prevText)

		if len(newRunes) > len(prevRunes) {
			// Добавлены новые символы (в конец или при автозамене)
			// Отправляем всё, что добавилось в хвост
			for i := len(prevRunes); i < len(newRunes); i++ {
				if vk.onRuneTyped != nil {
					vk.onRuneTyped(newRunes[i])
				}
			}
		} else if len(newRunes) < len(prevRunes) {
			// Текст уменьшился -> нажали Backspace (или IME удалил слово)
			// Отправляем ровно столько Backspace, сколько символов исчезло
			backspaces := len(prevRunes) - len(newRunes)
			if vk.onKeyPress != nil {
				for i := 0; i < backspaces; i++ {
					vk.onKeyPress(42, 0) // HID Backspace
				}
			}
		}

		prevText = newText

		// Сбрасываем поле реже, чтобы не мешать IME при быстром наборе,
		// но достаточно часто, чтобы не переполнять буфер.
		if len(newRunes) > 100 {
			suppress = true
			textHint.SetText("")
			prevText = ""
			suppress = false
		}
	}

	textHint.SetPlaceHolder(i18n.Current.VirtualKeyboardClickToType)

	// Кнопка F: popup с двумя столбцами — F1–F12 слева, F13–F24 справа (при выборе — отправить клавишу и закрыть)
	f1_12_Codes := []int{58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69}
	f1_12_Labels := []string{"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12"}
	f13_24_Codes := []int{104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115}
	f13_24_Labels := []string{"F13", "F14", "F15", "F16", "F17", "F18", "F19", "F20", "F21", "F22", "F23", "F24"}
	makeFColumn := func(labels []string, codes []int) *fyne.Container {
		col := container.NewVBox()
		for i, label := range labels {
			code := codes[i]
			btn := widget.NewButton(label, func() {
				if vk.onKeyPress != nil {
					vk.onKeyPress(code, 0)
				}
			})
			col.Add(btn)
		}
		return col
	}
	fCol1 := makeFColumn(f1_12_Labels, f1_12_Codes)
	fCol2 := makeFColumn(f13_24_Labels, f13_24_Codes)
	fPopupContent := container.NewHBox(fCol1, fCol2)
	fBtn := widget.NewButton("Fx", nil)
	fBtn.OnTapped = func() {
		if vk.parentWindow == nil {
			return
		}
		popup := widget.NewPopUp(fPopupContent, vk.parentWindow.Canvas())
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(fBtn)
		// Показываем popup выше кнопки, чтобы не наползал на системный бар (назад/домой/меню)
		contentH := fPopupContent.MinSize().Height
		popup.ShowAtPosition(fyne.NewPos(pos.X, pos.Y-contentH))
		
		// Настраиваем закрытие попапа при нажатии
		for _, colObj := range fPopupContent.Objects {
			if col, ok := colObj.(*fyne.Container); ok {
				for _, btnObj := range col.Objects {
					if btn, ok := btnObj.(*widget.Button); ok {
						oldOnTapped := btn.OnTapped
						btn.OnTapped = func() {
							oldOnTapped()
							popup.Hide()
						}
					}
				}
			}
		}
	}

	// Верхний ряд: Esc, Tab, Shift, Fx; нижний: Ctrl, Win, Alt, Del
	row1Keys := container.NewHBox(
		vk.createKey("Esc", 41, 0),
		vk.createKey("Tab", 43, 0),
		vk.createModifierKey("Shift", 225),
		fBtn,
	)
	row2Keys := container.NewHBox(
		vk.createModifierKey("Ctrl", 224),
		vk.createModifierKey("Win", 227),
		vk.createModifierKey("Alt", 226),
		vk.createKey("Del", 76, 0),
	)
	vk.shiftBtn = row1Keys.Objects[2].(*widget.Button)
	vk.ctrlBtn = row2Keys.Objects[0].(*widget.Button)
	vk.winBtn = row2Keys.Objects[1].(*widget.Button)
	vk.altBtn = row2Keys.Objects[2].(*widget.Button)
	leftKeys := container.NewVBox(row1Keys, row2Keys)

	// Enter — на 2 строки, заполняет пространство между блоками клавиш и D-pad (как на ноутбуке)
	enterBtn := vk.createKey("Enter", 40, 0)

	// D-pad справа (без изменений)
	const dpadSize = 28
	ph := func() fyne.CanvasObject {
		r := canvas.NewRectangle(theme.BackgroundColor())
		r.Resize(fyne.NewSize(dpadSize, dpadSize))
		return r
	}
	upBtn := vk.createKey("↑", 82, 0)
	upBtn.Resize(fyne.NewSize(dpadSize, dpadSize))
	leftBtn := vk.createKey("←", 80, 0)
	leftBtn.Resize(fyne.NewSize(dpadSize, dpadSize))
	downBtn := vk.createKey("↓", 81, 0)
	downBtn.Resize(fyne.NewSize(dpadSize, dpadSize))
	rightBtn := vk.createKey("→", 79, 0)
	rightBtn.Resize(fyne.NewSize(dpadSize, dpadSize))
	dpad := container.NewGridWithColumns(3,
		ph(), upBtn, ph(),
		leftBtn, downBtn, rightBtn,
	)

	// Слева — 2 ряда кнопок, по центру — Enter (растягивается по высоте 2 рядов и по ширине до D-pad), справа — D-pad
	keysWithEnterAndDpad := container.NewBorder(nil, nil, leftKeys, dpad, enterBtn)

	// Строка с полем ввода: Entry (подстраивается под экран) + кнопка очистки справа
	clearBtn := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		suppress = true
		textHint.SetText("")
		prevText = ""
		suppress = false
	})
	clearBtn.Importance = widget.MediumImportance
	inputRow := container.NewBorder(nil, nil, nil, clearBtn, textHint)
	main := container.NewVBox(keysWithEnterAndDpad, inputRow)

	background := canvas.NewRectangle(theme.BackgroundColor())
	background.FillColor = theme.BackgroundColor()

	// IME spacer: динамический отступ снизу.
	// Когда поле ввода получает фокус (Android IME открывается), spacer вырастает
	// до ~42% высоты экрана, сдвигая нашу клавиатуру выше и освобождая видео.
	vk.imeSpacer = &imeSpacerLayout{}
	vk.imeSpacerCont = container.New(vk.imeSpacer)

	textHint.onFocused = func() {
		if fyne.CurrentDevice().IsMobile() {
			vk.adjustForIME(true)
		}
	}
	textHint.onUnfocused = func() {
		if fyne.CurrentDevice().IsMobile() {
			vk.adjustForIME(false)
		}
	}

	// Нижний отступ для навбара Android (40px) вшит в paddedMain.
	// IME spacer добавляется отдельно ниже, чтобы можно было его динамически менять.
	paddedMain := view.NewInset(main, 4, 4, 4, view.MobileFooterBottomInset(4))
	innerLayout := container.NewBorder(nil, vk.imeSpacerCont, nil, nil, paddedMain)
	return container.NewStack(background, innerLayout)
}

// createKeyboardLayoutDesktop — раскладка с фиксированной сеткой как у аппаратной клавиатуры (каждая кнопка на своём месте).
func (vk *VirtualKeyboard) createKeyboardLayoutDesktop() *fyne.Container {
	grid := container.NewWithoutLayout()
	background := canvas.NewRectangle(theme.BackgroundColor())
	background.FillColor = theme.BackgroundColor()
	grid.Add(background)

	var col float32

	// Ряд 0: Esc, F1–F12, Del (в конце строки F)
	col = 0
	vk.placeKey(grid, vk.createKey("Esc", 41, 0), 0, col, 1)
	col++
	fLabels := []string{"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12"}
	fCodes := []int{58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69}
	for i := 0; i < 12; i++ {
		vk.placeKey(grid, vk.createKey(fLabels[i], fCodes[i], 0), 0, col, 1)
		col++
	}
	vk.placeKey(grid, vk.createKey("Del", 76, 0), 0, col, 1)

	// Ряд 1: ` 1 2 3 4 5 6 7 8 9 0 - = Backspace(2) (без Ins, Home, PgUp)
	col = 0
	for _, pair := range []struct {
		l string
		c int
	}{
		{"`", 53}, {"1", 30}, {"2", 31}, {"3", 32}, {"4", 33}, {"5", 34}, {"6", 35}, {"7", 36}, {"8", 37}, {"9", 38}, {"0", 39}, {"-", 45}, {"=", 46},
	} {
		vk.placeKey(grid, vk.createKey(pair.l, pair.c, 0), 1, col, 1)
		col++
	}
	vk.placeKey(grid, vk.createKey("⌫", 42, 0), 1, col, 2)

	// Ряд 2: Tab(1.5) Q W E R T Y U I O P [ ] \ (без Del, End, PgDn)
	col = 0
	vk.placeKey(grid, vk.createKey("Tab", 43, 0), 2, col, 1.5)
	col += 1.5
	for _, pair := range []struct {
		l string
		c int
	}{
		{"Q", 20}, {"W", 26}, {"E", 8}, {"R", 21}, {"T", 23}, {"Y", 28}, {"U", 24}, {"I", 12}, {"O", 18}, {"P", 19}, {"[", 47}, {"]", 48}, {"\\", 49},
	} {
		vk.placeKey(grid, vk.createKey(pair.l, pair.c, 0), 2, col, 1)
		col++
	}

	// Ряд 3: Caps(1.75) A S D F G H J K L ; ' Enter(2.25)
	vk.capsLockBtn = vk.createModifierKey("Caps", 57)
	col = 0
	vk.placeKey(grid, vk.capsLockBtn, 3, col, 1.75)
	col += 1.75
	for _, pair := range []struct {
		l string
		c int
	}{
		{"A", 4}, {"S", 22}, {"D", 7}, {"F", 9}, {"G", 10}, {"H", 11}, {"J", 13}, {"K", 14}, {"L", 15}, {";", 51}, {"'", 52},
	} {
		vk.placeKey(grid, vk.createKey(pair.l, pair.c, 0), 3, col, 1)
		col++
	}
	vk.placeKey(grid, vk.createKey("Enter", 40, 0), 3, col, 2.25)

	// Ряд 4: Shift(1.5) Z X C V B N M , . / Shift(1.5) ↑ [невидимая] — левый и правый Shift одинаковой ширины
	vk.shiftBtn = vk.createModifierKey("Shift", 225)
	col = 0
	vk.placeKey(grid, vk.shiftBtn, 4, col, 1.5)
	col += 1.5
	for _, pair := range []struct {
		l string
		c int
	}{
		{"Z", 29}, {"X", 27}, {"C", 6}, {"V", 25}, {"B", 5}, {"N", 17}, {"M", 16}, {",", 54}, {".", 55}, {"/", 56},
	} {
		vk.placeKey(grid, vk.createKey(pair.l, pair.c, 0), 4, col, 1)
		col++
	}
	vk.placeKey(grid, vk.createModifierKey("Shift", 229), 4, col, 1.5)
	col += 1.5
	vk.placeKey(grid, vk.createKey("↑", 82, 0), 4, col, 1) // ↑ в col 13 — ровно над ↓
	col++
	// Невидимая клавиша в col 14 — ровно над →
	vk.placeInvisiblePlaceholder(grid, 4, col)

	// Ряд 5: Ctrl(1.25) Win(1.25) Alt(1.25) Space(3.5) Alt Win Menu Ctrl ← ↓ →
	// Пробел укорочен так, чтобы ↓ была ровно под ↑ (один столбец).
	vk.ctrlBtn = vk.createModifierKey("Ctrl", 224)
	vk.winBtn = vk.createModifierKey("⊞", 227)
	vk.altBtn = vk.createModifierKey("Alt", 226)
	col = 0
	vk.placeKey(grid, vk.ctrlBtn, 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.winBtn, 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.altBtn, 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.createKey("Space", 44, 0), 5, col, 3.5)
	col += 3.5
	vk.placeKey(grid, vk.createKey("Alt", 230, 0), 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.createModifierKey("⊞", 231), 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.createKey("☰", 232, 0), 5, col, 1)
	col++
	vk.placeKey(grid, vk.createModifierKey("Ctrl", 228), 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.createKey("←", 80, 0), 5, col, 1)
	col++
	vk.placeKey(grid, vk.createKey("↓", 81, 0), 5, col, 1) // ↓ ровно под ↑
	col++
	vk.placeKey(grid, vk.createKey("→", 79, 0), 5, col, 1)

	background.Move(fyne.NewPos(0, 0))
	background.Resize(fyne.NewSize(keyboardW, keyboardH))

	layout := &centerKeyboardLayout{width: keyboardW, height: keyboardH}
	return container.New(layout, grid)
}

// createKey создает кнопку клавиши
func (vk *VirtualKeyboard) createKey(label string, keyCode int, modifiers int) *widget.Button {
	btn := widget.NewButton(label, func() {
		vk.handleKeyPress(keyCode, modifiers)
	})

	// Устанавливаем размеры клавиш по умолчанию
	btn.Resize(fyne.NewSize(40, 35))

	// Делаем пробел шире
	if label == "Space" {
		btn.Resize(fyne.NewSize(200, 35))
	}

	// Делаем специальные клавиши шире
	if label == "Tab" || label == "Caps" || label == "Shift" || label == "Ctrl" || label == "Alt" {
		btn.Resize(fyne.NewSize(60, 35))
	}

	// Делаем Enter шире
	if label == "Enter" {
		btn.Resize(fyne.NewSize(80, 35))
	}

	// Делаем Backspace шире
	if label == "⌫" {
		btn.Resize(fyne.NewSize(80, 35))
	}

	// Делаем функциональные клавиши меньше
	if label == "F1" || label == "F2" || label == "F3" || label == "F4" ||
		label == "F5" || label == "F6" || label == "F7" || label == "F8" ||
		label == "F9" || label == "F10" || label == "F11" || label == "F12" {
		btn.Resize(fyne.NewSize(35, 30))
	}

	// Делаем Escape меньше
	if label == "Esc" {
		btn.Resize(fyne.NewSize(35, 30))
	}

	// Делаем Windows и Menu клавиши среднего размера
	if label == "Win" || label == "Menu" || label == "⊞" || label == "☰" {
		btn.Resize(fyne.NewSize(50, 35))
	}

	return btn
}

// createModifierKey создает кнопку модификатора с переключением
func (vk *VirtualKeyboard) createModifierKey(label string, keyCode int) *widget.Button {
	btn := widget.NewButton(label, func() {
		vk.toggleModifier(keyCode)
	})

	btn.Resize(fyne.NewSize(60, 35))
	return btn
}

// toggleModifier переключает состояние модификатора
func (vk *VirtualKeyboard) toggleModifier(keyCode int) {
	switch keyCode {
	case 224, 228: // Ctrl (Left/Right)
		vk.ctrlPressed = !vk.ctrlPressed
		vk.updateModifierButton(vk.ctrlBtn, "Ctrl", vk.ctrlPressed)
		logrus.Infof("⌨️ Ctrl toggled: %v", vk.ctrlPressed)
	case 226, 230: // Alt (Left/Right)
		vk.altPressed = !vk.altPressed
		vk.updateModifierButton(vk.altBtn, "Alt", vk.altPressed)
		logrus.Infof("⌨️ Alt toggled: %v", vk.altPressed)
	case 225, 229: // Shift (Left/Right)
		vk.shiftPressed = !vk.shiftPressed
		vk.updateModifierButton(vk.shiftBtn, "Shift", vk.shiftPressed)
		logrus.Infof("⌨️ Shift toggled: %v", vk.shiftPressed)
	case 227, 231: // Win/GUI (Left/Right)
		vk.winPressed = !vk.winPressed
		vk.updateModifierButton(vk.winBtn, "Win", vk.winPressed)
		logrus.Infof("⌨️ Win toggled: %v", vk.winPressed)
	case 57: // Caps Lock
		vk.capsLockPressed = !vk.capsLockPressed
		vk.updateModifierButton(vk.capsLockBtn, "Caps", vk.capsLockPressed)
		// Отправляем CapsLock как обычную клавишу (она работает как toggle на уровне ОС)
		if vk.onKeyPress != nil {
			vk.onKeyPress(57, 0)
		}
		logrus.Infof("⌨️ CapsLock toggled: %v", vk.capsLockPressed)
	}
}

// updateModifierButton обновляет внешний вид кнопки модификатора
func (vk *VirtualKeyboard) updateModifierButton(btn *widget.Button, label string, pressed bool) {
	if btn == nil {
		return
	}

	if pressed {
		// Нажата - синяя заливка (как CapsLock)
		btn.Importance = widget.HighImportance
	} else {
		// Отжата - обычный вид
		btn.Importance = widget.MediumImportance
	}
	btn.Refresh()
}

// handleKeyPress обрабатывает нажатие клавиши
func (vk *VirtualKeyboard) handleKeyPress(keyCode int, modifiers int) {
	// Вычисляем текущие активные модификаторы
	currentModifiers := modifiers
	if vk.ctrlPressed {
		currentModifiers |= 1 // Left Control
	}
	if vk.shiftPressed {
		currentModifiers |= 2 // Left Shift
	}
	if vk.altPressed {
		currentModifiers |= 4 // Left Alt
	}
	if vk.winPressed {
		currentModifiers |= 8 // Left GUI (Windows)
	}

	logrus.Infof("⌨️ Virtual keyboard: key %d pressed with modifiers %d (active: Ctrl=%v, Shift=%v, Alt=%v, Win=%v)",
		keyCode, currentModifiers, vk.ctrlPressed, vk.shiftPressed, vk.altPressed, vk.winPressed)

	if vk.onKeyPress != nil {
		vk.onKeyPress(keyCode, currentModifiers)
	}
}

// toggleVisibility переключает видимость клавиатуры
func (vk *VirtualKeyboard) toggleVisibility() {
	if vk.isVisible {
		vk.Hide()
	} else {
		vk.ShowInSeparateWindow()
	}
}

// Show показывает клавиатуру
func (vk *VirtualKeyboard) Show() {
	if vk.isVisible {
		return
	}

	vk.isVisible = true
	vk.keyboard.Show()

	// Позиционируем клавиатуру по центру экрана
	if vk.parentWindow != nil {
		size := vk.parentWindow.Canvas().Size()
		keyboardSize := fyne.NewSize(700, 250) // Размер для основной клавиатуры

		x := (size.Width - keyboardSize.Width) / 2
		y := (size.Height - keyboardSize.Height) / 2

		vk.keyboard.Move(fyne.NewPos(x, y))
		vk.keyboard.Resize(keyboardSize)
	}

	// Позиционируем кнопку переключения в правом нижнем углу
	if vk.parentWindow != nil {
		size := vk.parentWindow.Canvas().Size()
		btnSize := fyne.NewSize(50, 50)

		x := size.Width - btnSize.Width - 10
		y := size.Height - btnSize.Height - 10

		vk.toggleBtn.Move(fyne.NewPos(x, y))
		vk.toggleBtn.Resize(btnSize)
	}

	logrus.Info("⌨️ Virtual keyboard shown")
}

// ShowInSeparateWindow показывает клавиатуру в отдельном окне
func (vk *VirtualKeyboard) ShowInSeparateWindow() {
	if vk.isVisible {
		return
	}

	logrus.Info("⌨️ Opening virtual keyboard in a separate window")

	// Создаем отдельное окно для клавиатуры
	vk.keyboardWindow = fyne.CurrentApp().NewWindow(i18n.Current.VirtualKeyboard)
	vk.keyboardWindow.SetContent(vk.keyboard)
	vk.keyboardWindow.Resize(fyne.NewSize(600, 260)) // Размер для основной клавиатуры
	vk.keyboardWindow.CenterOnScreen()

	// Настраиваем окно клавиатуры
	vk.keyboardWindow.SetOnClosed(func() {
		logrus.Info("⌨️ Virtual keyboard window closed")
		vk.isVisible = false
		vk.keyboardWindow = nil
	})

	vk.isVisible = true
	vk.keyboard.Show()
	vk.keyboardWindow.Show()

	logrus.Info("⌨️ Virtual keyboard shown in a separate window")
}

// Hide скрывает клавиатуру
func (vk *VirtualKeyboard) Hide() {
	if !vk.isVisible {
		return
	}

	vk.isVisible = false

	// Закрываем отдельное окно если оно открыто
	if vk.keyboardWindow != nil {
		vk.keyboardWindow.Close()
		vk.keyboardWindow = nil
	}

	if vk.keyboard != nil {
		vk.keyboard.Hide()
	}
	vk.BlurInput()

	logrus.Info("⌨️ Virtual keyboard hidden")
}

// IsVisible возвращает состояние видимости
func (vk *VirtualKeyboard) IsVisible() bool {
	return vk.isVisible
}

// GetContainer возвращает контейнер клавиатуры
func (vk *VirtualKeyboard) GetContainer() *fyne.Container {
	return vk.container
}

// GetKeyboardLayout возвращает только layout клавиатуры без кнопки переключения
func (vk *VirtualKeyboard) GetKeyboardLayout() *fyne.Container {
	logrus.Infof("⌨️ [DEBUG] GetKeyboardLayout called, keyboard=%v, MinSize=%v, Visible=%v",
		vk.keyboard != nil, vk.keyboard.MinSize(), vk.keyboard.Visible())
	return vk.keyboard
}

// UpdatePosition обновляет позицию элементов клавиатуры
func (vk *VirtualKeyboard) UpdatePosition(windowSize fyne.Size) {
	// Позиционируем кнопку переключения в правом нижнем углу
	btnSize := fyne.NewSize(50, 50)
	x := windowSize.Width - btnSize.Width - 10
	y := windowSize.Height - btnSize.Height - 10

	vk.toggleBtn.Move(fyne.NewPos(x, y))
	vk.toggleBtn.Resize(btnSize)
}

// SetVisibleState устанавливает состояние видимости без показа отдельного окна
func (vk *VirtualKeyboard) SetVisibleState(visible bool) {
	vk.isVisible = visible
	if vk.keyboard == nil {
		return
	}
	if visible {
		vk.keyboard.Show()
		return
	}
	vk.keyboard.Hide()
	vk.BlurInput()
}

// FocusInput запрашивает фокус у строки ввода Android-клавиатуры, чтобы показать системную IME.
func (vk *VirtualKeyboard) FocusInput() {
	if vk.parentWindow == nil || vk.mobileInput == nil {
		return
	}
	vk.parentWindow.RequestFocus()
	vk.parentWindow.Canvas().Focus(vk.mobileInput)
}

// BlurInput снимает фокус со строки ввода Android-клавиатуры.
func (vk *VirtualKeyboard) BlurInput() {
	if vk.parentWindow == nil {
		return
	}
	vk.parentWindow.Canvas().Focus(nil)
}

// SetOnIMEChanged устанавливает callback, вызываемый при открытии/закрытии Android IME.
// Родительский контейнер должен обновить layout в этом callback.
func (vk *VirtualKeyboard) SetOnIMEChanged(fn func(open bool)) {
	vk.onIMEChanged = fn
}

// adjustForIME реагирует на открытие/закрытие системной клавиатуры Android.
// При открытии IME добавляет нижний отступ (~42% экрана), чтобы наша клавиатура
// оставалась видимой над системной клавиатурой, и видео сдвигалось вверх.
func (vk *VirtualKeyboard) adjustForIME(open bool) {
	if vk.imeSpacer == nil || vk.imeSpacerCont == nil {
		return
	}
	if open && vk.parentWindow != nil {
		// Типичная Android клавиатура занимает ~40-45% высоты экрана
		h := vk.parentWindow.Canvas().Size().Height * 0.42
		logrus.Infof("⌨️ [IME] открыта, добавляем отступ %.0fpx (canvasH=%.0f)", h, vk.parentWindow.Canvas().Size().Height)
		vk.imeSpacer.height = h
	} else {
		logrus.Info("⌨️ [IME] закрыта, убираем отступ")
		vk.imeSpacer.height = 0
	}
	// Обновляем внутренний layout — imeSpacerCont получит новый MinSize
	vk.imeSpacerCont.Refresh()
	// Уведомляем родителя чтобы он перечитал MinSize нашей клавиатуры и переложил контейнеры
	if vk.onIMEChanged != nil {
		vk.onIMEChanged(open)
	}
}
