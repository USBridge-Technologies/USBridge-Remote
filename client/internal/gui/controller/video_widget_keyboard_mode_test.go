package controller

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
)

// recordingInput is a connected video client that records every keyboard
// event the widget sends to the host. The embedded VideoClient is nil: only
// the methods the keyboard path calls are implemented.
type recordingInput struct {
	service.VideoClient

	mu   sync.Mutex
	sent []string
}

func (r *recordingInput) IsConnected() bool   { return true }
func (r *recordingInput) IsInputActive() bool { return true }

func (r *recordingInput) SendMoonlightKey(vk int16, action int8, mods int8) {
	dir := "down"
	if action == service.LiKeyActionUp {
		dir = "up"
	}
	r.record(fmt.Sprintf("key %s 0x%02X mods=0x%02X", dir, uint16(vk), uint8(mods)))
}

func (r *recordingInput) SendMoonlightUtf8Text(text string) { r.record("text " + text) }

func (r *recordingInput) SendMoonlightMouseMove(dx, dy int16)               {}
func (r *recordingInput) SendMoonlightMousePosition(x, y, refW, refH int16) {}
func (r *recordingInput) SendMoonlightMouseButton(action int8, button int)  {}
func (r *recordingInput) SendMoonlightScroll(clicks int8)                   {}
func (r *recordingInput) SendMoonlightControllerEvent(uint16, uint16, uint16, uint8, uint8, int16, int16, int16, int16) {
}
func (r *recordingInput) SendMoonlightPenEvent(uint8, uint8, uint8, float32, float32, float32, uint16, uint8) {
}

func (r *recordingInput) record(s string) {
	r.mu.Lock()
	r.sent = append(r.sent, s)
	r.mu.Unlock()
}

// waitSent waits for the async send queue to deliver want events, then
// checks nothing else trails behind them.
func (r *recordingInput) waitSent(t *testing.T, want ...string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		n := len(r.sent)
		r.mu.Unlock()
		if n >= len(want) || time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	r.mu.Lock()
	got := append([]string(nil), r.sent...)
	r.sent = nil
	r.mu.Unlock()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sent to host:\n got  %q\n want %q", got, want)
	}
}

func newKeyboardTestWidget(t *testing.T, mode, agentOS string) (*VideoWidget, *recordingInput) {
	t.Helper()
	if runtime.GOOS != "windows" {
		// Scan codes below are Windows PS/2 codes; other client OSes are
		// covered by input.TestNormalizeScanCodeFor.
		t.Skip("uses Windows scan codes")
	}
	rec := &recordingInput{}
	vw := &VideoWidget{videoClient: rec}
	vw.SetAgentEnvironment(agentOS, "")
	vw.SetKeyboardInputMode(mode)
	return vw, rec
}

// press drives one keystroke through the same three Fyne callbacks a real
// physical key produces: KeyDown, TypedRune (0 = none), KeyUp.
func press(vw *VideoWidget, name fyne.KeyName, scan int, r rune) {
	ev := &fyne.KeyEvent{Name: name, Physical: fyne.HardwareKey{ScanCode: scan}}
	vw.handlePhysicalKeyDown(ev)
	if r != 0 {
		vw.handlePhysicalRunePress(r)
	}
	vw.handlePhysicalKeyUp(ev)
}

func TestKeyboardInputModeDefaultsToText(t *testing.T) {
	vw := &VideoWidget{}
	if got := vw.GetKeyboardInputMode(); got != KeyboardInputModeText {
		t.Fatalf("default mode = %q, want %q", got, KeyboardInputModeText)
	}
	vw.SetKeyboardInputMode(KeyboardInputModeKeys)
	if got := vw.GetKeyboardInputMode(); got != KeyboardInputModeKeys {
		t.Fatalf("mode = %q, want %q", got, KeyboardInputModeKeys)
	}
	vw.SetKeyboardInputMode("bogus")
	if got := vw.GetKeyboardInputMode(); got != KeyboardInputModeText {
		t.Fatalf("unknown mode = %q, want text", got)
	}
}

// Characters mode: a printable key reaches the host exactly once, as the
// character the client layout produced (here Cyrillic, host layout ignored).
func TestTextModeSendsEachCharacterOnce(t *testing.T) {
	vw, rec := newKeyboardTestWidget(t, KeyboardInputModeText, "windows")

	press(vw, fyne.Key0, 0x0B, '0')
	press(vw, fyne.KeyName(""), 0x10, 'й') // Q position on a Russian layout
	rec.waitSent(t, "text 0", "text й")
}

// Characters mode still sends non-character keys as keys.
func TestTextModeSendsControlKeysAsKeys(t *testing.T) {
	vw, rec := newKeyboardTestWidget(t, KeyboardInputModeText, "windows")

	press(vw, fyne.KeyReturn, 0x1C, 0)
	press(vw, fyne.KeyBackspace, 0x0E, 0)
	rec.waitSent(t,
		"key down 0x0D mods=0x00", "key up 0x0D mods=0x00",
		"key down 0x08 mods=0x00", "key up 0x08 mods=0x00",
	)
}

// Left Shift used to fall inside the "character key" scan-code range and was
// never sent, so Shift+click and Shift+arrows did nothing with it.
func TestTextModeSendsLeftShift(t *testing.T) {
	vw, rec := newKeyboardTestWidget(t, KeyboardInputModeText, "windows")

	press(vw, fyne.KeyName("LeftShift"), 0x2A, 0)
	rec.waitSent(t, "key down 0xA0 mods=0x01", "key up 0xA0 mods=0x00")
}

// Keys mode: every key goes as a raw key by physical position, TypedRune is
// ignored, so a Russian client layout still presses Q on the host.
func TestKeysModeSendsPhysicalKeysOnly(t *testing.T) {
	vw, rec := newKeyboardTestWidget(t, KeyboardInputModeKeys, "windows")

	press(vw, fyne.KeyName(""), 0x10, 'й')
	press(vw, fyne.Key0, 0x0B, '0')
	rec.waitSent(t,
		"key down 0x51 mods=0x00", "key up 0x51 mods=0x00",
		"key down 0x30 mods=0x00", "key up 0x30 mods=0x00",
	)
}

// Keys mode goes by position, not by the client layout's key name: on AZERTY
// the key Fyne names "A" sits at the QWERTY Q position.
func TestKeysModeUsesPositionNotLayoutName(t *testing.T) {
	vw, rec := newKeyboardTestWidget(t, KeyboardInputModeKeys, "linux")

	press(vw, fyne.KeyA, 0x10, 'a')
	rec.waitSent(t, "key down 0x51 mods=0x00", "key up 0x51 mods=0x00")
}

// Keys mode with Shift: Shift is held on the host and the digit goes as a
// key, so the host layout decides between "!" and "1".
func TestKeysModeShiftedKey(t *testing.T) {
	vw, rec := newKeyboardTestWidget(t, KeyboardInputModeKeys, "windows")

	shift := &fyne.KeyEvent{Name: fyne.KeyName("LeftShift"), Physical: fyne.HardwareKey{ScanCode: 0x2A}}
	vw.handlePhysicalKeyDown(shift)
	press(vw, fyne.Key1, 0x02, '!')
	vw.handlePhysicalKeyUp(shift)
	rec.waitSent(t,
		"key down 0xA0 mods=0x01",
		"key down 0x31 mods=0x01", "key up 0x31 mods=0x01",
		"key up 0xA0 mods=0x00",
	)
}

// Switching modes with a key held releases it on the host instead of
// leaving it stuck.
func TestSwitchingModeReleasesHeldKeys(t *testing.T) {
	vw, rec := newKeyboardTestWidget(t, KeyboardInputModeKeys, "windows")

	vw.handlePhysicalKeyDown(&fyne.KeyEvent{Name: fyne.KeyW, Physical: fyne.HardwareKey{ScanCode: 0x11}})
	vw.SetKeyboardInputMode(KeyboardInputModeText)
	rec.waitSent(t, "key down 0x57 mods=0x00", "key up 0x57 mods=0x00")
}
