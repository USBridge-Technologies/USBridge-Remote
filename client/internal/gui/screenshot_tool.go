package gui

// screenshot_tool.go -- the Control footer's Screenshot / Copy Text from
// Screen menu (mw.screenshotToolIcon). Both actions grab a frame from
// api.RequestLiveFrame, the same already-decoded-video shortcut local
// ui.parse uses (see internal/api/live_frame.go) -- no extra device round
// trip, just whatever the operator is already looking at.

import (
	"fmt"
	"os"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// screenshotToolFrameTimeout mirrors local_ui_intercept.go's own
// liveFrameWaitTimeout -- long enough to cover a real frame's PNG encode
// (measured live: 110-120ms+ at 3840x2160 for even a highly compressible
// frame, see that file's doc comment), short enough that tapping the icon
// with no video session open fails fast instead of hanging the menu.
const screenshotToolFrameTimeout = 2 * time.Second

// showScreenshotToolMenu opens the two-item menu: Screenshot (always) and
// Copy Text from Screen (macOS only for now -- see
// screenshot_tool_ocr_darwin.go/screenshot_tool_ocr_other.go; hidden
// entirely elsewhere rather than shown disabled, since there's no near-term
// plan for a Windows/Linux OCR backend to explain the greyed-out state to).
func (mw *MainWindow) showScreenshotToolMenu() {
	if mw.screenshotToolIcon == nil {
		return
	}
	items := []view.StyledMenuItem{
		{
			Label: "Screenshot",
			OnTap: func() { mw.captureScreenshotToFile() },
		},
	}
	if ocrHelperAvailable() {
		items = append(items, view.StyledMenuItem{
			Label: "Copy Text from Screen",
			OnTap: func() { mw.copyScreenTextToClipboard() },
		})
	}
	mw.presentControlStyledMenu(mw.screenshotToolIcon, items)
}

// captureScreenshotToFile grabs the current frame and saves it wherever the
// operator picks, via the OS's own native save panel (nativeSaveFile) when
// available -- Fyne's own dialog.NewFileSave only when it isn't (see that
// function's callers elsewhere in this package for the same fallback
// pattern).
func (mw *MainWindow) captureScreenshotToFile() {
	parent := nativeParentWindow(mw.window)
	go func() {
		png, ok := api.RequestLiveFrame(screenshotToolFrameTimeout)
		if !ok {
			fyne.Do(func() {
				view.ShowErrorDialog(fmt.Errorf("no live video frame available -- open Control and make sure the stream is playing"), mw.window)
			})
			return
		}
		name := "usbridge-screenshot-" + time.Now().Format("20060102-150405") + ".png"
		if nativeSaveAvailable() {
			path, err := nativeSaveFile(parent, "Save Screenshot", name, pngSaveFilter)
			if err != nil {
				fyne.Do(func() { mw.fyneSaveScreenshotPNG(png, name) })
				return
			}
			if path == "" {
				return // cancelled
			}
			if err := writeScreenshotPNG(path, png); err != nil {
				fyne.Do(func() { view.ShowErrorDialog(err, mw.window) })
			}
			return
		}
		fyne.Do(func() { mw.fyneSaveScreenshotPNG(png, name) })
	}()
}

// copyScreenTextToClipboard grabs the current frame, runs it through the
// bundled Vision OCR helper (macOS only -- see screenshot_tool_ocr_darwin.go),
// and puts the recognized text on the clipboard, matching Preview/Photos'
// own Live Text "copy text from image" behavior.
func (mw *MainWindow) copyScreenTextToClipboard() {
	go func() {
		png, ok := api.RequestLiveFrame(screenshotToolFrameTimeout)
		if !ok {
			fyne.Do(func() {
				view.ShowErrorDialog(fmt.Errorf("no live video frame available -- open Control and make sure the stream is playing"), mw.window)
			})
			return
		}
		text, err := recognizeTextInPNG(png)
		if err != nil {
			fyne.Do(func() { view.ShowErrorDialog(err, mw.window) })
			return
		}
		fyne.Do(func() {
			if text == "" {
				view.ShowInfoDialog("Copy Text from Screen", "No text was recognized on screen.", mw.window)
				return
			}
			if mw.window != nil && mw.window.Clipboard() != nil {
				mw.window.Clipboard().SetContent(text)
			}
			view.ShowInfoDialog("Copy Text from Screen", "Text copied to clipboard.", mw.window)
		})
	}()
}

// writeScreenshotPNG is the native-save-panel path's plain os.Create write
// (path already chosen by the operator) -- mirrors saveBenchmarkZipFile's
// own os.Create+write pattern elsewhere in this package.
func writeScreenshotPNG(path string, png []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, werr := f.Write(png)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// fyneSaveScreenshotPNG is the fallback save path on platforms/builds
// without a native save panel (native_filesave_default.go) -- mirrors
// fyneSaveBenchmarkZip's own dialog.NewFileSave pattern.
func (mw *MainWindow) fyneSaveScreenshotPNG(png []byte, name string) {
	fd := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil || wc == nil {
			return
		}
		_, werr := wc.Write(png)
		if cerr := wc.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			view.ShowErrorDialog(werr, mw.window)
			return
		}
		view.ShowInfoDialog("Screenshot", fmt.Sprintf("Saved to %s", wc.URI().Name()), mw.window)
	}, mw.window)
	fd.SetFileName(name)
	view.ShowFileDialog(fd, mw.window)
}
