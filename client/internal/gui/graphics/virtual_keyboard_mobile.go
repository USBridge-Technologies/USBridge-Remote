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
	border      *canvas.Rectangle
	content     fyne.CanvasObject
}

func (e *backspaceEntry) CreateRenderer() fyne.WidgetRenderer {
	r := e.Entry.CreateRenderer()
	if e.content != nil {
		return &backspaceEntryRenderer{renderer: r, entry: e}
	}
	return r
}

type backspaceEntryRenderer struct {
	renderer fyne.WidgetRenderer
	entry    *backspaceEntry
}

func (r *backspaceEntryRenderer) Destroy() {
	r.renderer.Destroy()
}

func (r *backspaceEntryRenderer) Layout(size fyne.Size) {
	r.renderer.Layout(size)
	if r.entry.content != nil {
		r.entry.content.Resize(size)
		r.entry.content.Move(fyne.NewPos(0, 0))
	}
}

func (r *backspaceEntryRenderer) MinSize() fyne.Size {
	if r.entry.content != nil {
		return r.entry.content.MinSize()
	}
	return r.renderer.MinSize()
}

func (r *backspaceEntryRenderer) Objects() []fyne.CanvasObject {
	objs := r.renderer.Objects()
	if r.entry.content != nil {
		for _, o := range objs {
			o.Hide()
		}
		return append(objs, r.entry.content)
	}
	return objs
}

func (r *backspaceEntryRenderer) Refresh() {
	r.renderer.Refresh()
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
	if e.border != nil {
		e.border.StrokeColor = design.ColorConnectionBadgeText
		e.border.Refresh()
	}
	if e.onFocused != nil {
		e.onFocused()
	}
}

func (e *backspaceEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.border != nil {
		e.border.StrokeColor = design.ColorStatusBarBorder
		e.border.Refresh()
	}
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

	// Special-keys only in the header. Soft IME typing is RustDesk-style via
	// the native EditText (sticky) → keyboardTyped → touchpad UTF-8 — no
	// visible buffer the user has to type into and clear.
	keys := vk.createCompactKeysChrome()
	textHint.content = view.NewInsetExact(keys, 4, 4, 2, 1)

	vk.imeSpacer = &imeSpacerLayout{height: 0}
	vk.imeSpacerCont = container.New(vk.imeSpacer)
	vk.imeSpacerCont.Hide()

	textHint.onFocused = func() {
		vk.RegisterAsIMETarget()
	}
	textHint.onUnfocused = func() {
		// iOS/wasm: video touches steal Fyne focus and would collapse the
		// system IME. Re-assert Entry focus while sticky until the special-
		// keys dismiss button clears keepIMEFocus.
		if !vk.keepIMEFocus.Load() {
			return
		}
		time.AfterFunc(16*time.Millisecond, func() {
			fyne.Do(func() {
				if vk.keepIMEFocus.Load() {
					vk.FocusInput()
				}
			})
		})
	}

	background := canvas.NewRectangle(design.ColorGray900)
	return container.NewMax(container.NewThemeOverride(
		container.NewStack(background, textHint),
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

// SetKeepIMEFocus toggles re-focus-on-blur for the soft IME entry (iOS/wasm
// sticky keyboard). Must be cleared before BlurInput when dismissing.
func (vk *VirtualKeyboard) SetKeepIMEFocus(on bool) {
	if vk == nil {
		return
	}
	vk.keepIMEFocus.Store(on)
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
