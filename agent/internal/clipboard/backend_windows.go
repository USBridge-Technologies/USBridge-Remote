//go:build windows

package clipboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalSize   = kernel32.NewProc("GlobalSize")

	procDragQueryFileW = shell32.NewProc("DragQueryFileW")
)

const (
	cfUnicodeText = 13 // CF_UNICODETEXT
	cfHDROP       = 15 // CF_HDROP
	gmemMoveable  = 0x0002
)

var (
	pngFormatOnce sync.Once
	pngFormatID   uintptr
)

// pngFormat lazily registers the "PNG" clipboard format used by modern
// Windows apps (Chrome, Photoshop, GIMP, Slack, current mspaint) for lossless
// image copy/paste. RegisterClipboardFormatW returns the same value for a
// given name for the lifetime of the session, so registering once is enough.
func pngFormat() uintptr {
	pngFormatOnce.Do(func() {
		name, _ := windows.UTF16PtrFromString("PNG")
		ret, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
		pngFormatID = ret
	})
	return pngFormatID
}

// openClipboard retries briefly: OpenClipboard fails transiently if another
// process (e.g. a clipboard manager) has it open at the same instant.
func openClipboard() bool {
	for i := 0; i < 10; i++ {
		if ret, _, _ := procOpenClipboard.Call(0); ret != 0 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func closeClipboard() {
	procCloseClipboard.Call()
}

func isFormatAvailable(format uintptr) bool {
	ret, _, _ := procIsClipboardFormatAvailable.Call(format)
	return ret != 0
}

type windowsBackend struct{}

// NewBackend returns the native Windows clipboard backend. winHandle is
// unused (the Win32 clipboard is a systemwide resource opened with a NULL
// window handle) — see the darwin backend for why the parameter exists.
func NewBackend(winHandle any) Backend { return &windowsBackend{} }

func (b *windowsBackend) ChangeStamp() (string, error) {
	seq, _, _ := procGetClipboardSequenceNumber.Call()
	return strconv.FormatUint(uint64(seq), 10), nil
}

func (b *windowsBackend) Read() (Content, bool, error) {
	if files, ok := readFiles(); ok {
		return Content{Kind: KindFile, Files: files}, true, nil
	}
	if img, ok := readImagePNG(); ok {
		return Content{Kind: KindImage, Image: img}, true, nil
	}
	if text, ok := readText(); ok {
		return Content{Kind: KindText, Text: text}, true, nil
	}
	return Content{}, false, nil
}

func (b *windowsBackend) Write(content Content) error {
	switch content.Kind {
	case KindText:
		return writeText(content.Text)
	case KindImage:
		return writeImagePNG(content.Image)
	case KindFile:
		return writeFiles(content.Files)
	default:
		return fmt.Errorf("clipboard: unsupported kind %q", content.Kind)
	}
}

func readText() (string, bool) {
	if !isFormatAvailable(cfUnicodeText) {
		return "", false
	}
	if !openClipboard() {
		return "", false
	}
	defer closeClipboard()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}
	lp, _, _ := procGlobalLock.Call(h)
	if lp == 0 {
		return "", false
	}
	defer procGlobalUnlock.Call(h)
	text := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(lp)))
	return text, true
}

func writeText(text string) error {
	if !openClipboard() {
		return fmt.Errorf("clipboard: OpenClipboard failed")
	}
	defer closeClipboard()
	procEmptyClipboard.Call()

	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := len(utf16) * 2
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(size))
	if h == 0 {
		return fmt.Errorf("clipboard: GlobalAlloc failed")
	}
	lp, _, _ := procGlobalLock.Call(h)
	if lp == 0 {
		return fmt.Errorf("clipboard: GlobalLock failed")
	}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(lp)), len(utf16))
	copy(dst, utf16)
	procGlobalUnlock.Call(h)

	if ret, _, _ := procSetClipboardData.Call(cfUnicodeText, h); ret == 0 {
		return fmt.Errorf("clipboard: SetClipboardData failed")
	}
	return nil
}

func readImagePNG() ([]byte, bool) {
	fmtID := pngFormat()
	if fmtID == 0 || !isFormatAvailable(fmtID) {
		return nil, false
	}
	if !openClipboard() {
		return nil, false
	}
	defer closeClipboard()
	h, _, _ := procGetClipboardData.Call(fmtID)
	if h == 0 {
		return nil, false
	}
	lp, _, _ := procGlobalLock.Call(h)
	if lp == 0 {
		return nil, false
	}
	defer procGlobalUnlock.Call(h)
	size, _, _ := procGlobalSize.Call(h)
	if size == 0 {
		return nil, false
	}
	buf := make([]byte, size)
	copy(buf, unsafe.Slice((*byte)(unsafe.Pointer(lp)), size))
	return buf, true
}

func writeImagePNG(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("clipboard: empty image")
	}
	fmtID := pngFormat()
	if fmtID == 0 {
		return fmt.Errorf("clipboard: failed to register PNG format")
	}
	if !openClipboard() {
		return fmt.Errorf("clipboard: OpenClipboard failed")
	}
	defer closeClipboard()
	procEmptyClipboard.Call()

	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return fmt.Errorf("clipboard: GlobalAlloc failed")
	}
	lp, _, _ := procGlobalLock.Call(h)
	if lp == 0 {
		return fmt.Errorf("clipboard: GlobalLock failed")
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(lp)), len(data))
	copy(dst, data)
	procGlobalUnlock.Call(h)

	if ret, _, _ := procSetClipboardData.Call(fmtID, h); ret == 0 {
		return fmt.Errorf("clipboard: SetClipboardData failed")
	}
	return nil
}

func readFiles() ([]FileItem, bool) {
	if !isFormatAvailable(cfHDROP) {
		return nil, false
	}
	if !openClipboard() {
		return nil, false
	}
	defer closeClipboard()
	h, _, _ := procGetClipboardData.Call(cfHDROP)
	if h == 0 {
		return nil, false
	}
	count, _, _ := procDragQueryFileW.Call(h, 0xFFFFFFFF, 0, 0)
	if count == 0 {
		return nil, false
	}
	var files []FileItem
	for i := uintptr(0); i < count; i++ {
		n, _, _ := procDragQueryFileW.Call(h, i, 0, 0)
		if n == 0 {
			continue
		}
		buf := make([]uint16, n+1)
		procDragQueryFileW.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		path := windows.UTF16ToString(buf)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		files = append(files, FileItem{Name: filepath.Base(path), Data: data})
	}
	if len(files) == 0 {
		return nil, false
	}
	return files, true
}

// dropFilesHeader mirrors the Win32 DROPFILES struct: 20 bytes on both amd64
// and arm64 Windows (LLP64 — LONG/BOOL are always 4 bytes), followed in
// memory by a double-NUL-terminated list of wide-char file paths.
type dropFilesHeader struct {
	pFiles uint32
	ptX    int32
	ptY    int32
	fNC    int32
	fWide  int32
}

func writeFiles(items []FileItem) error {
	if len(items) == 0 {
		return fmt.Errorf("clipboard: no files")
	}
	dir, err := os.MkdirTemp("", "usbridge-clip-*")
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(items))
	for _, f := range items {
		path := filepath.Join(dir, sanitizeFileName(f.Name))
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			return err
		}
		paths = append(paths, path)
	}

	const headerSize = 20 // sizeof(DROPFILES)
	var wideList []uint16
	for _, p := range paths {
		u16, err := windows.UTF16FromString(p)
		if err != nil {
			return err
		}
		wideList = append(wideList, u16...) // UTF16FromString includes the trailing NUL
	}
	wideList = append(wideList, 0) // extra NUL terminates the whole list

	totalSize := headerSize + len(wideList)*2
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(totalSize))
	if h == 0 {
		return fmt.Errorf("clipboard: GlobalAlloc failed")
	}
	lp, _, _ := procGlobalLock.Call(h)
	if lp == 0 {
		return fmt.Errorf("clipboard: GlobalLock failed")
	}
	hdr := (*dropFilesHeader)(unsafe.Pointer(lp))
	hdr.pFiles = headerSize
	hdr.ptX = 0
	hdr.ptY = 0
	hdr.fNC = 0
	hdr.fWide = 1
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(lp+uintptr(headerSize))), len(wideList))
	copy(dst, wideList)
	procGlobalUnlock.Call(h)

	if !openClipboard() {
		return fmt.Errorf("clipboard: OpenClipboard failed")
	}
	defer closeClipboard()
	procEmptyClipboard.Call()

	if ret, _, _ := procSetClipboardData.Call(cfHDROP, h); ret == 0 {
		return fmt.Errorf("clipboard: SetClipboardData failed")
	}
	return nil
}
