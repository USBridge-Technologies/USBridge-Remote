//go:build android || ios

package graphics

import (
	"strings"
	"sync"
	"time"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/input"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// activeIMEKeyboardMu защищает activeIMEKeyboardTarget от гонок.
var (
	activeIMEKeyboardMu     sync.RWMutex
	activeIMEKeyboardTarget *VirtualKeyboard
)

// RegisterAsIMETarget регистрируется в keyboard_ime_android.go

func activeIMEKeyboard() *VirtualKeyboard {
	activeIMEKeyboardMu.RLock()
	defer activeIMEKeyboardMu.RUnlock()
	return activeIMEKeyboardTarget
}

// backspaceEntry — поле ввода для Android/iOS, которое позволяет ловить системные клавиши
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

// imeSpacerLayout — layout с динамической высотой для отступа под IME
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

// createKeyboardLayout создает раскладку клавиатуры для мобильных устройств
func (vk *VirtualKeyboard) createKeyboardLayout() *fyne.Container {
	textHint := &backspaceEntry{}
	textHint.Password = false
	textHint.ExtendBaseWidget(textHint)
	vk.mobileInput = textHint

	// Всё состояние под одним мьютексом — OnChanged вызывается из разных goroutine на Android.
	var (
		mu          sync.Mutex
		prevText    string
		pendingText string
		suppress    bool
		timer       *time.Timer
	)

	// Фоновый worker: все сетевые вызовы здесь, UI-поток никогда не блокируется.
	type netTask struct {
		backspaces int
		runes      []rune
		extraKey   int
	}
	netChan := make(chan netTask, 500)
	go func() {
		for task := range netChan {
			if vk.onKeyPress != nil {
				for i := 0; i < task.backspaces; i++ {
					vk.onKeyPress(42, 0)
				}
			}
			if vk.onRuneTyped != nil {
				for _, r := range task.runes {
					vk.onRuneTyped(r)
				}
			}
			if task.extraKey != 0 && vk.onKeyPress != nil {
				vk.onKeyPress(task.extraKey, 0)
			}
		}
	}()

	// enqueueDiff вычисляет diff от prevText к target и кладёт задачу в netChan.
	// Вызывать строго под mu; сам освобождает mu перед отправкой в канал.
	enqueueDiff := func(target string, extraKey int) {
		if target == prevText && extraKey == 0 {
			mu.Unlock()
			return
		}
		newRunes := []rune(target)
		oldRunes := []rune(prevText)
		commonLen := 0
		minLen := len(oldRunes)
		if len(newRunes) < minLen {
			minLen = len(newRunes)
		}
		for i := 0; i < minLen; i++ {
			if oldRunes[i] == newRunes[i] {
				commonLen++
			} else {
				break
			}
		}
		bs := len(oldRunes) - commonLen
		added := append([]rune(nil), newRunes[commonLen:]...)
		prevText = target
		logrus.Infof("⌨️ [DIFF] bs=%d added=%q extraKey=%d", bs, string(added), extraKey)
		mu.Unlock()
		netChan <- netTask{backspaces: bs, runes: added, extraKey: extraKey}
	}

	// commitChanges сбрасывает буфер: берёт mu, вычисляет diff, отпускает.
	commitChanges := func(extraKey int) {
		mu.Lock()
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		target := pendingText
		enqueueDiff(target, extraKey) // освобождает mu
	}

	textHint.onKey = func(keyName fyne.KeyName) {
		if keyName == fyne.KeyReturn || keyName == fyne.KeyTab {
			code := input.GetKeyCode(keyName)
			commitChanges(code)
			return
		}
		if keyName == fyne.KeyBackspace {
			mu.Lock()
			empty := prevText == ""
			mu.Unlock()
			if empty {
				netChan <- netTask{extraKey: 42}
			}
		}
	}

	textHint.OnChanged = func(newText string) {
		mu.Lock()
		if suppress {
			pendingText = newText
			prevText = newText
			mu.Unlock()
			return
		}

		isPrefix := strings.HasPrefix(newText, prevText)

		if isPrefix {
			// Быстрый путь: простой набор — шлём diff сразу без таймера.
			if timer != nil {
				timer.Stop()
				timer = nil
			}
			pendingText = newText
			enqueueDiff(newText, 0) // освобождает mu
			return
		}

		// Медленный путь: IME заменяет слово (автозамена, autocomplete).
		// Ждём стабилизации 20ms.
		pendingText = newText
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(20*time.Millisecond, func() {
			commitChanges(0)
		})
		mu.Unlock()
	}

	// Очистка буфера при переполнении (100+ рун).
	actualOnChanged := textHint.OnChanged
	textHint.OnChanged = func(s string) {
		actualOnChanged(s)
		if len([]rune(s)) > 100 {
			mu.Lock()
			suppress = true
			pendingText = ""
			prevText = ""
			mu.Unlock()
			fyne.Do(func() {
				textHint.SetText("")
				mu.Lock()
				suppress = false
				mu.Unlock()
			})
		}
	}

	textHint.SetPlaceHolder(i18n.Current.VirtualKeyboardClickToType)

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
		contentH := fPopupContent.MinSize().Height
		popup.ShowAtPosition(fyne.NewPos(pos.X, pos.Y-contentH))
		
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

	enterBtn := vk.createKey("Enter", 40, 0)

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

	keysWithEnterAndDpad := container.NewBorder(nil, nil, leftKeys, dpad, enterBtn)

	clearBtn := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		suppress = true
		textHint.SetText("")
		mu.Lock()
		pendingText = ""
		prevText = ""
		mu.Unlock()
		suppress = false
	})
	clearBtn.Importance = widget.MediumImportance
	inputRow := container.NewBorder(nil, nil, nil, clearBtn, textHint)
	main := container.NewVBox(keysWithEnterAndDpad, inputRow)

	background := canvas.NewRectangle(theme.BackgroundColor())
	background.FillColor = theme.BackgroundColor()

	vk.imeSpacer = &imeSpacerLayout{}
	vk.imeSpacerCont = container.New(vk.imeSpacer)

	textHint.onFocused = func() {
		vk.adjustForIME(true)
	}
	// Мы НЕ сбрасываем отступ в onUnfocused (adjustForIME(false)),
	// так как на Android системный навигационный бар все еще занимает место.
	// Мы доверяем событиям KeyboardBridge.onIMEHeightChanged, которые приходят
	// от Android при скрытии клавиатуры и содержат актуальную высоту (например, только NavBar).
	textHint.onUnfocused = func() {
	}

	paddedMain := view.NewInset(main, 4, 4, 4, 4)
	innerLayout := container.NewBorder(nil, vk.imeSpacerCont, nil, nil, paddedMain)
	return container.NewStack(background, innerLayout)
}

// FocusInput запрашивает фокус у строки ввода Android-клавиатуры
func (vk *VirtualKeyboard) FocusInput() {
	if vk.parentWindow == nil || vk.mobileInput == nil {
		return
	}
	vk.parentWindow.RequestFocus()
	vk.parentWindow.Canvas().Focus(vk.mobileInput)
}

// BlurInput снимает фокус со строки ввода
func (vk *VirtualKeyboard) BlurInput() {
	if vk.parentWindow == nil {
		return
	}
	vk.parentWindow.Canvas().Focus(nil)
}

// SetOnIMEChanged устанавливает callback
func (vk *VirtualKeyboard) SetOnIMEChanged(fn func(open bool)) {
	vk.onIMEChanged = fn
}

// setIMEOffset выставляет точный нижний отступ
func (vk *VirtualKeyboard) setIMEOffset(imeH float32) {
	if vk.imeSpacer == nil || vk.imeSpacerCont == nil {
		return
	}
	if imeH < 0 {
		imeH = 0
	}
	logrus.Infof("⌨️ [IME] setIMEOffset: %.0f Fyne-единиц", imeH)
	vk.imeSpacer.height = imeH
	vk.imeSpacerCont.Refresh()
	if vk.keyboard != nil {
		vk.keyboard.Refresh()
	}
	if vk.onIMEChanged != nil {
		vk.onIMEChanged(imeH > 0)
	}
}

// adjustForIME — запасной путь
func (vk *VirtualKeyboard) adjustForIME(open bool) {
	if open {
		if vk.imeSpacer != nil && vk.imeSpacer.height > 0 {
			return
		}
		// Устанавливаем минимальный начальный отступ, пока не пришло реальное значение от JNI
		vk.setIMEOffset(10)
	} else {
		vk.setIMEOffset(0)
	}
}

// ResetIMEState сбрасывает отступ IME
func (vk *VirtualKeyboard) ResetIMEState() {
	if vk.imeSpacer == nil || vk.imeSpacer.height == 0 {
		return
	}
	logrus.Info("⌨️ [IME] принудительный сброс отступа (canvas вырос — IME закрыта)")
	vk.setIMEOffset(0)
}
