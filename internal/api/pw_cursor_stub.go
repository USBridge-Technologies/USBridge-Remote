//go:build !linux || !cgo

package api

import "fmt"

type pwCursorWatcher struct{}

func newPWCursorWatcher(_ uint32, _ int) (*pwCursorWatcher, error) {
	return nil, fmt.Errorf("pipewire cursor watcher is unavailable on this platform")
}

func (w *pwCursorWatcher) stop() {}

func (w *pwCursorWatcher) snapshot(_, _ int) *CursorState {
	return nil
}
