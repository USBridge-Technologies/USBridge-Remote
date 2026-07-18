//go:build darwin

package clipboard

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>

extern int clipboard_change_count(void);
extern char* clipboard_get_text(void);
extern int clipboard_set_text(const char* text);
extern unsigned char* clipboard_get_image_png(size_t* outLen);
extern int clipboard_set_image_png(const unsigned char* data, size_t len);
extern int clipboard_get_file_count(void);
extern char* clipboard_get_file_path(int index);
extern int clipboard_set_files(const char** paths, int count);
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"unsafe"
)

type darwinBackend struct{}

// NewBackend returns the native macOS clipboard backend. winHandle is unused
// here (NSPasteboard is a systemwide singleton, not tied to any window) — the
// parameter exists so callers can construct a Backend the same way on every
// platform; the mobile backend (client only) is the one that actually needs
// it, as a *fyne.Window.
func NewBackend(winHandle any) Backend { return &darwinBackend{} }

func (b *darwinBackend) ChangeStamp() (string, error) {
	return strconv.Itoa(int(C.clipboard_change_count())), nil
}

func (b *darwinBackend) Read() (Content, bool, error) {
	// Files/images first: copying an image from Finder also leaves a file
	// URL (and sometimes a TIFF) on the pasteboard, so checking richer
	// representations before falling back to plain text avoids treating a
	// copied image or file as if it were merely its filename.
	if n := int(C.clipboard_get_file_count()); n > 0 {
		files := make([]FileItem, 0, n)
		for i := 0; i < n; i++ {
			cPath := C.clipboard_get_file_path(C.int(i))
			if cPath == nil {
				continue
			}
			path := C.GoString(cPath)
			C.free(unsafe.Pointer(cPath))
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			files = append(files, FileItem{Name: filepath.Base(path), Data: data})
		}
		if len(files) > 0 {
			return Content{Kind: KindFile, Files: files}, true, nil
		}
	}

	var outLen C.size_t
	if cData := C.clipboard_get_image_png(&outLen); cData != nil {
		img := C.GoBytes(unsafe.Pointer(cData), C.int(outLen))
		C.free(unsafe.Pointer(cData))
		if len(img) > 0 {
			return Content{Kind: KindImage, Image: img}, true, nil
		}
	}

	if cText := C.clipboard_get_text(); cText != nil {
		text := C.GoString(cText)
		C.free(unsafe.Pointer(cText))
		if text != "" {
			return Content{Kind: KindText, Text: text}, true, nil
		}
	}

	return Content{}, false, nil
}

func (b *darwinBackend) Write(content Content) error {
	switch content.Kind {
	case KindText:
		cText := C.CString(content.Text)
		defer C.free(unsafe.Pointer(cText))
		if C.clipboard_set_text(cText) == 0 {
			return fmt.Errorf("clipboard: set text failed")
		}
		return nil
	case KindImage:
		if len(content.Image) == 0 {
			return fmt.Errorf("clipboard: empty image")
		}
		cData := C.CBytes(content.Image)
		defer C.free(cData)
		if C.clipboard_set_image_png((*C.uchar)(cData), C.size_t(len(content.Image))) == 0 {
			return fmt.Errorf("clipboard: set image failed")
		}
		return nil
	case KindFile:
		if len(content.Files) == 0 {
			return fmt.Errorf("clipboard: no files")
		}
		dir, err := os.MkdirTemp("", "usbridge-clip-*")
		if err != nil {
			return err
		}
		cPaths := make([]*C.char, len(content.Files))
		for i, f := range content.Files {
			path := filepath.Join(dir, sanitizeFileName(f.Name))
			if err := os.WriteFile(path, f.Data, 0o600); err != nil {
				return err
			}
			cPaths[i] = C.CString(path)
		}
		defer func() {
			for _, p := range cPaths {
				C.free(unsafe.Pointer(p))
			}
		}()
		if C.clipboard_set_files((**C.char)(unsafe.Pointer(&cPaths[0])), C.int(len(cPaths))) == 0 {
			return fmt.Errorf("clipboard: set files failed")
		}
		return nil
	default:
		return fmt.Errorf("clipboard: unsupported kind %q", content.Kind)
	}
}
