//go:build android || ios

package clipboard

import "fyne.io/fyne/v2"

// mobileBackend provides text-only clipboard sync on Android/iOS via Fyne's
// cross-platform Clipboard API. Image/file clipboard on mobile needs its own
// native bridge per OS (JNI ClipboardManager on Android, UIPasteboard via an
// Objective-C bridge on iOS) and is deliberately out of scope for this pass —
// see the clipboard sync plan for why desktop got full support first.
type mobileBackend struct {
	win fyne.Window
}

// NewBackend returns the mobile clipboard backend. winHandle must be a
// fyne.Window (or nil, in which case the backend is a no-op) — unlike the
// desktop backends, Fyne's Clipboard API is scoped to a window.
func NewBackend(winHandle any) Backend {
	win, _ := winHandle.(fyne.Window)
	return &mobileBackend{win: win}
}

func (b *mobileBackend) clipboard() fyne.Clipboard {
	if b.win == nil {
		return nil
	}
	return b.win.Clipboard()
}

func (b *mobileBackend) ChangeStamp() (string, error) {
	cb := b.clipboard()
	if cb == nil {
		return "", nil
	}
	// Mobile clipboard content is text-only and typically small, so using
	// the content itself as the "cheap" change stamp is fine — Fyne exposes
	// no cross-platform sequence-number API to poll more cheaply than this.
	return cb.Content(), nil
}

func (b *mobileBackend) Read() (Content, bool, error) {
	cb := b.clipboard()
	if cb == nil {
		return Content{}, false, nil
	}
	text := cb.Content()
	if text == "" {
		return Content{}, false, nil
	}
	return Content{Kind: KindText, Text: text}, true, nil
}

func (b *mobileBackend) Write(content Content) error {
	cb := b.clipboard()
	if cb == nil || content.Kind != KindText {
		return nil
	}
	cb.SetContent(content.Text)
	return nil
}
