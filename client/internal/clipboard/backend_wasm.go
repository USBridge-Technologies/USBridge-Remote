//go:build js && wasm

package clipboard

// wasmBackend is a no-op clipboard backend for browser builds. Real
// browser clipboard access (the async Clipboard API, which needs JS interop
// and user-gesture/permission handling) is a separate follow-up piece, not
// implemented here — clipboard sync simply reports "nothing on the
// clipboard" and silently drops writes.
type wasmBackend struct{}

func NewBackend(winHandle any) Backend { return &wasmBackend{} }

func (b *wasmBackend) ChangeStamp() (string, error) { return "", nil }

func (b *wasmBackend) Read() (Content, bool, error) { return Content{}, false, nil }

func (b *wasmBackend) Write(content Content) error { return nil }
