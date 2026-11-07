//go:build windows

package gui

import (
	"fmt"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	comdlg32                 = windows.NewLazySystemDLL("comdlg32.dll")
	procGetSaveFileNameW     = comdlg32.NewProc("GetSaveFileNameW")
	procCommDlgExtendedError = comdlg32.NewProc("CommDlgExtendedError")
	saveOle32                = windows.NewLazySystemDLL("ole32.dll")
	procSaveCoInitializeEx   = saveOle32.NewProc("CoInitializeEx")
	procSaveCoUninitialize   = saveOle32.NewProc("CoUninitialize")
)

// openFileNameW is OPENFILENAMEW; Go's field alignment matches the C
// layout on both 386 and amd64/arm64.
type openFileNameW struct {
	structSize    uint32
	owner         uintptr
	instance      uintptr
	filter        *uint16
	customFilter  *uint16
	maxCustFilter uint32
	filterIndex   uint32
	file          *uint16
	maxFile       uint32
	fileTitle     *uint16
	maxFileTitle  uint32
	initialDir    *uint16
	title         *uint16
	flags         uint32
	fileOffset    uint16
	fileExtension uint16
	defExt        *uint16
	custData      uintptr
	hook          uintptr
	templateName  *uint16
	reserved      uintptr
	reservedDword uint32
	flagsEx       uint32
}

const (
	ofnOverwritePrompt = 0x00000002
	ofnHideReadOnly    = 0x00000004
	ofnNoChangeDir     = 0x00000008
	ofnPathMustExist   = 0x00000800
	ofnExplorer        = 0x00080000
)

func nativeSaveAvailable() bool { return true }

// nativeSaveFile shows the Explorer save panel (comdlg32) owned by the
// client window, so it opens modal on top of it. "" with a nil error is a
// cancel.
func nativeSaveFile(parent uintptr, title, defaultName string, filter saveFilter) (string, error) {
	// The panel runs a COM apartment on the calling thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const coinitApartmentThreaded, coinitDisableOle1DDE = 0x2, 0x4
	if hr, _, _ := procSaveCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOle1DDE); int32(hr) >= 0 {
		defer procSaveCoUninitialize.Call()
	}

	// Double-NUL-terminated "name\0pattern\0" pairs.
	pattern := "*." + filter.Ext
	filterStr := fmt.Sprintf("%s (%s)\x00%s\x00All files (*.*)\x00*.*\x00\x00", filter.Name, pattern, pattern)
	// (UTF16FromString rejects the embedded NULs.)
	filterU16 := utf16.Encode([]rune(filterStr))
	titleU16, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return "", err
	}
	defExt, err := syscall.UTF16PtrFromString(filter.Ext)
	if err != nil {
		return "", err
	}
	nameU16, err := syscall.UTF16FromString(defaultName)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, 32768)
	copy(buf, nameU16)

	ofn := openFileNameW{
		owner:       parent,
		filter:      &filterU16[0],
		filterIndex: 1,
		file:        &buf[0],
		maxFile:     uint32(len(buf)),
		title:       titleU16,
		flags:       ofnOverwritePrompt | ofnHideReadOnly | ofnNoChangeDir | ofnPathMustExist | ofnExplorer,
		defExt:      defExt,
	}
	ofn.structSize = uint32(unsafe.Sizeof(ofn))

	ok, _, _ := procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ok == 0 {
		code, _, _ := procCommDlgExtendedError.Call()
		if code == 0 {
			return "", nil // cancelled
		}
		return "", fmt.Errorf("GetSaveFileNameW failed: 0x%x", code)
	}
	return syscall.UTF16ToString(buf), nil
}
