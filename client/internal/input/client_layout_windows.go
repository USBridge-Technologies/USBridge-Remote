package input

import (
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetKeyboardLayout        = user32.NewProc("GetKeyboardLayout")
)

// ClientKeyboardLayoutID returns the KLID ("0000040E") of the layout the
// foreground window types with -- the client's own while it has focus, and
// the layout follows the user switching it (Alt+Shift) mid-session.
func ClientKeyboardLayoutID() string {
	hwnd, _, _ := procGetForegroundWindow.Call()
	var tid uintptr
	if hwnd != 0 {
		tid, _, _ = procGetWindowThreadProcessId.Call(hwnd, 0)
	}
	hkl, _, _ := procGetKeyboardLayout.Call(tid)
	klidCacheMu.Lock()
	defer klidCacheMu.Unlock()
	klid, ok := klidCache[uint32(hkl)]
	if !ok {
		klid = klidFromHKL(uint32(hkl), layoutIDToKLID)
		klidCache[uint32(hkl)] = klid
	}
	return klid
}

// klidCache: variants cost a registry scan, and this runs per typed key.
var (
	klidCacheMu sync.Mutex
	klidCache   = map[uint32]string{}
)

func layoutIDToKLID(layoutID uint16) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Keyboard Layouts`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return ""
	}
	defer k.Close()
	names, _ := k.ReadSubKeyNames(-1)
	want := fmt.Sprintf("%04x", layoutID)
	for _, n := range names {
		sk, err := registry.OpenKey(k, n, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		id, _, err := sk.GetStringValue("Layout Id")
		sk.Close()
		if err == nil && strings.EqualFold(id, want) {
			return strings.ToUpper(n)
		}
	}
	return ""
}
