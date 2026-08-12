//go:build android || ios || (js && wasm)

// This file's special-keys panel layout (Esc/Enter/arrows and friends,
// not a full QWERTY) is also what the web client wants -- see
// video_widget_web.go (client/internal/gui/controller) for why plain
// desktop's ShowInSeparateWindow()-based full keyboard was wrong for a
// browser tab in the first place. RegisterAsIMETarget (called from
// textHint.onFocused below) has no wasm implementation of its own here --
// see keyboard_ime_web.go for why a no-op is correct for this platform
// (there's no native OS IME to register with), and why FocusInput/
// BlurInput's plain Canvas.Focus()/Focus(nil) calls already trigger the
// browser's real on-screen keyboard on their own, with no wasm-specific
// wiring needed.
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
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// activeIMEKeyboardMu protects activeIMEKeyboardTarget from races.
var (
	activeIMEKeyboardMu     sync.RWMutex
	activeIMEKeyboardTarget *VirtualKeyboard
)

// RegisterAsIMETarget registers in keyboard_ime_android.go

// UnregisterAsIMETarget clears this VK from the active IME target if it is currently registered.
// Call on fullscreen exit to prevent stale references receiving IME height events.
func (vk *VirtualKeyboard) UnregisterAsIMETarget() {
	activeIMEKeyboardMu.Lock()
	if activeIMEKeyboardTarget == vk {
		activeIMEKeyboardTarget = nil
	}
	activeIMEKeyboardMu.Unlock()
}

func activeIMEKeyboard() *VirtualKeyboard {
	activeIMEKeyboardMu.RLock()
	defer activeIMEKeyboardMu.RUnlock()
	return activeIMEKeyboardTarget
}

// backspaceEntry is an input field for Android/iOS that allows catching system keys
type backspaceEntry struct {
	widget.Entry
	onKey       func(fyne.KeyName)
	onFocused   func() // called when the field gains focus (IME will open)
	onUnfocused func() // called when the field loses focus (IME will close)
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

// imeSpacerLayout is a layout with dynamic height for IME padding
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

// createKeyboardLayout creates keyboard layout for mobile devices
func (vk *VirtualKeyboard) createKeyboardLayout() *fyne.Container {
	textHint := &backspaceEntry{}
	textHint.Password = false
	textHint.ExtendBaseWidget(textHint)
	vk.mobileInput = textHint

	// All state under one mutex - OnChanged is called from different goroutines on Android.
	var (
		mu          sync.Mutex
		prevText    string
		pendingText string
		suppress    bool
		timer       *time.Timer
	)

	// Background worker: all network calls here, UI thread is never blocked.
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

	// enqueueDiff calculates the diff from prevText to target and puts a task into netChan.
	// Call strictly under mu; it releases mu itself before sending to the channel.
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

	// commitChanges flushes the buffer: acquires mu, calculates diff, releases.
	commitChanges := func(extraKey int) {
		mu.Lock()
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		target := pendingText
		enqueueDiff(target, extraKey) // releases mu
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
			// Fast path: simple typing - send diff immediately without timer.
			if timer != nil {
				timer.Stop()
				timer = nil
			}
			pendingText = newText
			enqueueDiff(newText, 0) // releases mu
			return
		}

		// Slow path: IME replaces a word (autocorrect, autocomplete).
		// Waiting 20ms for stabilization.
		pendingText = newText
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(20*time.Millisecond, func() {
			commitChanges(0)
		})
		mu.Unlock()
	}

	// Buffer cleanup on overflow (100+ runes).
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

	// keysSwitch swaps between the normal key panel and the F-key panel.
	var showNormal, showFKeys func()
	keysSwitch := container.NewStack()

	// F-key panel: two fixed rows of 7 cell-widths each -- F1-F7 on row
	// one, F8-F12 + a double-width Back on row two (5 + 2 = 7, same total
	// width as row one, so the two rows line up like the top of a Tetris
	// board rather than Back trailing off at some arbitrary width). Plain
	// GridWithColumns can't do this on its own (every column in one grid
	// is forced equal width, so there's no way to make Back span two
	// columns' worth of width) -- each key is instead individually wrapped
	// in its own container.NewGridWrap(size, ...), which reports that
	// exact fixed size as its MinSize, and the row is an HBox of those
	// (HBox packs children at their own MinSize instead of stretching them
	// to fill the row, unlike GridWithColumns -- see this func's earlier
	// history for why that stretch was the original overflow bug). Wider
	// than createKey's desktop F-key convention (35x30) on purpose -- at
	// that size 7 cells left a visible empty gap on the right of the row
	// (confirmed live); this fills the same available width the row
	// already had to itself instead of leaving it unused.
	fKeySize := fyne.NewSize(44, 34)
	backSize := fyne.NewSize(fKeySize.Width*2, fKeySize.Height)
	newFKey := func(label string, code int) *fyne.Container {
		btn := widget.NewButton(label, func() {
			if vk.onKeyPress != nil {
				vk.onKeyPress(code, 0)
			}
		})
		return container.NewGridWrap(fKeySize, btn)
	}
	row1 := container.NewHBox(
		newFKey("F1", 58), newFKey("F2", 59), newFKey("F3", 60), newFKey("F4", 61),
		newFKey("F5", 62), newFKey("F6", 63), newFKey("F7", 64),
	)
	backBtn := widget.NewButton("Back", func() { showNormal() })
	row2 := container.NewHBox(
		newFKey("F8", 65), newFKey("F9", 66), newFKey("F10", 67), newFKey("F11", 68), newFKey("F12", 69),
		container.NewGridWrap(backSize, backBtn),
	)
	fPanel := container.NewThemeOverride(
		container.NewVBox(row1, row2),
		design.NewBrandTheme(),
	)

	fBtn := widget.NewButton("Fx", func() { showFKeys() })

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
		r := canvas.NewRectangle(design.ColorGray950)
		r.Resize(fyne.NewSize(dpadSize, dpadSize))
		return r
	}
	upBtn := vk.createIconKey(theme.MoveUpIcon(), 82, 0)
	upBtn.Resize(fyne.NewSize(dpadSize, dpadSize))
	leftBtn := vk.createIconKey(theme.NavigateBackIcon(), 80, 0)
	leftBtn.Resize(fyne.NewSize(dpadSize, dpadSize))
	downBtn := vk.createIconKey(theme.MoveDownIcon(), 81, 0)
	downBtn.Resize(fyne.NewSize(dpadSize, dpadSize))
	rightBtn := vk.createIconKey(theme.NavigateNextIcon(), 79, 0)
	rightBtn.Resize(fyne.NewSize(dpadSize, dpadSize))
	dpad := container.NewGridWithColumns(3,
		ph(), upBtn, ph(),
		leftBtn, downBtn, rightBtn,
	)

	normalPanel := container.NewBorder(nil, nil, leftKeys, dpad, enterBtn)
	keysSwitch.Objects = []fyne.CanvasObject{normalPanel}

	showNormal = func() {
		keysSwitch.Objects = []fyne.CanvasObject{normalPanel}
		keysSwitch.Refresh()
	}
	showFKeys = func() {
		keysSwitch.Objects = []fyne.CanvasObject{fPanel}
		keysSwitch.Refresh()
	}

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

	pasteBtn := widget.NewButtonWithIcon("", theme.MediaReplayIcon(), func() {
		runes := []rune(textHint.Text)
		if len(runes) == 0 {
			return
		}
		netChan <- netTask{runes: runes}
	})
	pasteBtn.Importance = widget.MediumImportance

	inputRow := container.NewBorder(nil, nil, nil, container.NewHBox(pasteBtn, clearBtn), textHint)
	main := container.NewVBox(keysSwitch, inputRow)

	background := canvas.NewRectangle(design.ColorGray950)
	background.FillColor = design.ColorGray950

	vk.imeSpacer = &imeSpacerLayout{height: 0} // real value set by deliverIMEHeightFromJNI
	vk.imeSpacerCont = container.New(vk.imeSpacer)

	textHint.onFocused = func() {
		// Re-register so IME height events reach this VK even after a fullscreen session.
		vk.RegisterAsIMETarget()
		vk.adjustForIME(true)
	}
	// We do NOT reset the padding in onUnfocused (adjustForIME(false)),
	// because on Android the system navigation bar still takes up space.
	// We rely on KeyboardBridge.onIMEHeightChanged events that come
	// from Android when hiding the keyboard and contain the actual height (e.g. just NavBar).
	textHint.onUnfocused = func() {
	}

	paddedMain := view.NewInset(main, 4, 4, 4, 4)
	innerLayout := container.NewBorder(nil, vk.imeSpacerCont, nil, nil, paddedMain)
	return container.NewMax(container.NewThemeOverride(
		container.NewStack(background, innerLayout),
		design.NewBrandTheme(),
	))
}

// FocusInput requests focus on the Android keyboard input field
func (vk *VirtualKeyboard) FocusInput() {
	if vk.parentWindow == nil || vk.mobileInput == nil {
		return
	}
	vk.parentWindow.RequestFocus()
	vk.parentWindow.Canvas().Focus(vk.mobileInput)
}

// BlurInput removes focus from the input field
func (vk *VirtualKeyboard) BlurInput() {
	if vk.parentWindow == nil {
		return
	}
	vk.parentWindow.Canvas().Focus(nil)
}

// SetOnIMEChanged sets the callback
func (vk *VirtualKeyboard) SetOnIMEChanged(fn func(imeHeightDp float32)) {
	vk.onIMEChanged = fn
}

// setIMEOffset sets the exact bottom padding
func (vk *VirtualKeyboard) setIMEOffset(imeH float32) {
	if vk.imeSpacer == nil || vk.imeSpacerCont == nil {
		return
	}
	if imeH < 0 {
		imeH = 0
	}
	logrus.Infof("⌨️ [IME] setIMEOffset: %.0f Fyne units", imeH)
	vk.imeSpacer.height = imeH
	vk.imeSpacerCont.Refresh()
	if vk.keyboard != nil {
		vk.keyboard.Refresh()
	}
	if vk.onIMEChanged != nil {
		vk.onIMEChanged(imeH)
	}
}

// adjustForIME - fallback path
func (vk *VirtualKeyboard) adjustForIME(open bool) {
	if open {
		if vk.imeSpacer != nil && vk.imeSpacer.height > 0 {
			return
		}
		// Set minimal initial padding until the real value comes from JNI
		vk.setIMEOffset(10)
	} else {
		vk.setIMEOffset(0)
	}
}

// ResetIMEState resets the IME padding
func (vk *VirtualKeyboard) ResetIMEState() {
	if vk.imeSpacer == nil || vk.imeSpacer.height == 0 {
		return
	}
	logrus.Info("⌨️ [IME] forced padding reset (canvas grew - IME is closed)")
	vk.setIMEOffset(0)
}
