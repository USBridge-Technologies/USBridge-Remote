package gui

import (
	"bytes"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
)

// benchTestUI serializes the benchmark dialog's async UI work (benchUIDo)
// with the tests' own checks. Fyne's test driver runs fyne.Do inline on the
// calling goroutine, so without this a test walking the overlays raced the
// dialog being built on the benchmark goroutine. Reentrant: UI code that
// calls back into benchUIDo on the same goroutine runs inline.
var benchTestUI reentrantUILock

func init() {
	benchUIDo = func(f func()) { benchTestUI.Do(func() { fyne.Do(f) }) }
}

type reentrantUILock struct {
	mu    sync.Mutex
	owner atomic.Int64
}

func (l *reentrantUILock) Do(f func()) {
	id := goroutineID()
	if l.owner.Load() == id {
		f()
		return
	}
	l.mu.Lock()
	l.owner.Store(id)
	defer func() {
		l.owner.Store(0)
		l.mu.Unlock()
	}()
	f()
}

func goroutineID() int64 {
	buf := make([]byte, 64)
	buf = buf[:runtime.Stack(buf, false)]
	buf = bytes.TrimPrefix(buf, []byte("goroutine "))
	if i := bytes.IndexByte(buf, ' '); i > 0 {
		buf = buf[:i]
	}
	id, _ := strconv.ParseInt(string(buf), 10, 64)
	return id
}
