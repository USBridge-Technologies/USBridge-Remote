package api

import (
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
)

type cursorTracker struct {
	mu             sync.RWMutex
	enabled        bool
	watcher        *pwCursorWatcher
	fallbackWidth  int
	fallbackHeight int
}

func (t *cursorTracker) configure(show bool, nodeID uint32, fd int, fallbackWidth, fallbackHeight int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clearLocked()

	wayland := isWaylandDisplay()
	log.Printf("[cursor] configure: show=%v wayland=%v node=%d fd=%d size=%dx%d",
		show, wayland, nodeID, fd, fallbackWidth, fallbackHeight)

	if !show || !wayland {
		log.Printf("[cursor] tracker disabled: show=%v wayland=%v", show, wayland)
		return
	}

	if nodeID == 0 || fd <= 0 {
		log.Printf("[cursor] ❌ wayland metadata unavailable: node=%d fd=%d — cursor overlay will not work", nodeID, fd)
		return
	}

	t.fallbackWidth = fallbackWidth
	t.fallbackHeight = fallbackHeight

	log.Printf("[cursor] starting PipeWire cursor watcher: node=%d fd=%d", nodeID, fd)
	watcher, err := newPWCursorWatcher(uint32(nodeID), fd)
	if err != nil {
		log.Printf("[cursor] ❌ failed to start wayland cursor watcher: %v", err)
		return
	}

	t.enabled = true
	t.watcher = watcher
	log.Printf("[cursor] ✅ wayland metadata watcher active: node=%d size=%dx%d", nodeID, fallbackWidth, fallbackHeight)
}

func (t *cursorTracker) clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clearLocked()
}

func (t *cursorTracker) clearLocked() {
	if t.watcher != nil {
		t.watcher.stop()
		t.watcher = nil
	}
	t.enabled = false
}

func (t *cursorTracker) apply(_ MouseRequest) *CursorState {
	return t.snapshot()
}

func (t *cursorTracker) snapshot() *CursorState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.enabled || t.watcher == nil {
		return nil
	}
	return t.watcher.snapshot(t.fallbackWidth, t.fallbackHeight)
}

func isWaylandDisplay() bool {
	osInfo := strings.ToLower(strings.TrimSpace(runtime.GOOS))
	display := strings.ToLower(strings.TrimSpace(os.Getenv("XDG_SESSION_TYPE")))
	if display == "" && os.Getenv("WAYLAND_DISPLAY") != "" {
		display = "wayland"
	}
	return strings.Contains(osInfo, "linux") && strings.Contains(display, "wayland")
}
