package api

import (
	"bytes"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sirupsen/logrus"
)

// The UI goroutine's id (Fyne's main loop), set by MarkUIGoroutine. A
// request made on it freezes the whole window until the reply -- in the
// browser there is no other thread to keep painting -- so each one is
// reported once per call site with its stack. Always on, not a build flag:
// it costs a runtime.Stack only for requests, and only finds bugs.
var (
	uiGoroutineID atomic.Uint64
	uiBlockSeen   sync.Map
)

// MarkUIGoroutine records the calling goroutine as the UI one; call it from
// inside fyne.Do.
func MarkUIGoroutine() { uiGoroutineID.Store(currentGoroutineID()) }

func currentGoroutineID() uint64 {
	var buf [64]byte
	b := buf[:runtime.Stack(buf[:], false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	if i := bytes.IndexByte(b, ' '); i > 0 {
		id, _ := strconv.ParseUint(string(b[:i]), 10, 64)
		return id
	}
	return 0
}

// reportUIBlockingRequest logs method/endpoint with its stack when called on
// the UI goroutine.
func reportUIBlockingRequest(method, endpoint string) {
	id := uiGoroutineID.Load()
	if id == 0 || currentGoroutineID() != id {
		return
	}
	buf := make([]byte, 16<<10)
	stack := string(buf[:runtime.Stack(buf, false)])
	// Key on the frames above the api package: one report per caller.
	key := method + " " + endpoint
	for _, line := range strings.Split(stack, "\n") {
		if strings.HasPrefix(line, "usbridge-client/internal/") && !strings.HasPrefix(line, "usbridge-client/internal/api.") {
			key += " " + line
			break
		}
	}
	if _, dup := uiBlockSeen.LoadOrStore(key, struct{}{}); dup {
		return
	}
	logrus.Warnf("🐢 [UI-BLOCK] %s %s runs on the UI goroutine (the window freezes until it answers):\n%s", method, endpoint, stack)
}
