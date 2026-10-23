package input

import "sync"

// A hardware KVM is a plain USB keyboard to the target: it can only press
// keys, and the target's own keyboard layout decides what each key types.
// Text-mode typing (and paste) therefore has to know that layout to find the
// key a character sits on -- US positions type "ö" for "0" on a Hungarian
// target. The tables come from Windows' own layouts, see
// tools/gen_keyboard_layouts.ps1.

// RemoteLayoutAuto picks the layout this computer is typing with, assuming
// the target uses the same one (the usual case: your keyboard, your
// machines). Falls back to US where the client layout isn't known.
const RemoteLayoutAuto = "auto"

// RemoteLayoutUS is the layout every key press means without a table.
const RemoteLayoutUS = "00000409"

type remoteLayoutTable struct {
	ID, Name string
	// rune<<16 | flags<<8 | PS/2 Set-1 scan code; flags see remoteKey*.
	entries []uint32
}

const (
	remoteKeyShift = 1
	remoteKeyAltGr = 2
	remoteKeyDead  = 4
)

// RemoteKey is the physical key and modifiers that type one character.
type RemoteKey struct {
	ScanCode int // PS/2 Set-1, as GetVKCodeFromScanCode takes after normalizing
	Shift    bool
	AltGr    bool
	// Dead: the key only arms an accent; Space after it types the character.
	Dead bool
}

// RemoteLayoutInfo names one selectable layout.
type RemoteLayoutInfo struct {
	ID, Name string
}

// RemoteLayouts lists the layouts with a table, in menu order.
func RemoteLayouts() []RemoteLayoutInfo {
	out := make([]RemoteLayoutInfo, len(remoteLayoutTables))
	for i, t := range remoteLayoutTables {
		out[i] = RemoteLayoutInfo{ID: t.ID, Name: t.Name}
	}
	return out
}

// RemoteLayoutName returns the display name of a layout ID, "" if unknown.
func RemoteLayoutName(id string) string {
	if i := remoteLayoutIndex(id); i >= 0 {
		return remoteLayoutTables[i].Name
	}
	return ""
}

// ResolveRemoteLayout turns a setting (an ID or RemoteLayoutAuto) into the
// ID of a table: auto follows the client's active layout when it is known
// and tabled, US otherwise.
func ResolveRemoteLayout(setting string) string {
	if setting == "" || setting == RemoteLayoutAuto {
		setting = ClientKeyboardLayoutID()
	}
	if remoteLayoutIndex(setting) >= 0 {
		return setting
	}
	return RemoteLayoutUS
}

// RemoteLayoutKey returns the key that types r on layout id.
func RemoteLayoutKey(id string, r rune) (RemoteKey, bool) {
	i := remoteLayoutIndex(id)
	if i < 0 {
		return RemoteKey{}, false
	}
	v, ok := remoteLayoutMaps[i]()[r]
	if !ok {
		return RemoteKey{}, false
	}
	flags := v >> 8
	return RemoteKey{
		ScanCode: int(v & 0xFF),
		Shift:    flags&remoteKeyShift != 0,
		AltGr:    flags&remoteKeyAltGr != 0,
		Dead:     flags&remoteKeyDead != 0,
	}, true
}

func remoteLayoutIndex(id string) int {
	for i, t := range remoteLayoutTables {
		if t.ID == id {
			return i
		}
	}
	return -1
}

var remoteLayoutMaps = func() []func() map[rune]uint16 {
	out := make([]func() map[rune]uint16, len(remoteLayoutTables))
	for i := range remoteLayoutTables {
		t := &remoteLayoutTables[i]
		out[i] = sync.OnceValue(func() map[rune]uint16 {
			m := make(map[rune]uint16, len(t.entries))
			for _, e := range t.entries {
				m[rune(e>>16)] = uint16(e)
			}
			return m
		})
	}
	return out
}()
