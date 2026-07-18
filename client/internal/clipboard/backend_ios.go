//go:build ios

package clipboard

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework UIKit
#include <stdlib.h>

extern int clipboard_change_count(void);
extern char* clipboard_get_text(void);
extern int clipboard_set_text(const char* text);
extern unsigned char* clipboard_get_image_png(size_t* outLen);
extern int clipboard_set_image_png(const unsigned char* data, size_t len);
extern int clipboard_get_file_count(void);
extern char* clipboard_get_file_name(void);
extern unsigned char* clipboard_get_file_data(size_t* outLen);
extern int clipboard_set_file(const char* name, const unsigned char* data, size_t len);
*/
import "C"

import (
	"fmt"
	"strconv"
	"unsafe"
)

type iosBackend struct{}

// NewBackend returns the native iOS clipboard backend. winHandle is unused —
// UIPasteboard is a systemwide singleton, not tied to any window (same
// reasoning as the darwin desktop backend).
func NewBackend(winHandle any) Backend { return &iosBackend{} }

func (b *iosBackend) ChangeStamp() (string, error) {
	return strconv.Itoa(int(C.clipboard_change_count())), nil
}

func (b *iosBackend) Read() (Content, bool, error) {
	// Files/images first, mirroring the darwin desktop backend: a richer
	// representation should win over incidentally-also-present text.
	if C.clipboard_get_file_count() != 0 {
		name := "file"
		if cName := C.clipboard_get_file_name(); cName != nil {
			name = C.GoString(cName)
			C.free(unsafe.Pointer(cName))
		}
		var outLen C.size_t
		if cData := C.clipboard_get_file_data(&outLen); cData != nil {
			data := C.GoBytes(unsafe.Pointer(cData), C.int(outLen))
			C.free(unsafe.Pointer(cData))
			if len(data) > 0 {
				return Content{Kind: KindFile, Files: []FileItem{{Name: name, Data: data}}}, true, nil
			}
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

func (b *iosBackend) Write(content Content) error {
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
		// UIPasteboard has no generalized multi-file slot the way desktop
		// clipboards do (CF_HDROP on Windows, file URLs on mac/Linux) — only
		// the first file round-trips, carried via the custom UTIs defined in
		// backend_ios.m (reliable between USBridge instances; a foreign iOS
		// app's own "copy file" action uses its own document UTI and won't
		// be recognized here).
		f := content.Files[0]
		name := sanitizeFileName(f.Name)
		cName := C.CString(name)
		defer C.free(unsafe.Pointer(cName))
		cData := C.CBytes(f.Data)
		defer C.free(cData)
		if C.clipboard_set_file(cName, (*C.uchar)(cData), C.size_t(len(f.Data))) == 0 {
			return fmt.Errorf("clipboard: set file failed")
		}
		return nil

	default:
		return fmt.Errorf("clipboard: unsupported kind %q", content.Kind)
	}
}
