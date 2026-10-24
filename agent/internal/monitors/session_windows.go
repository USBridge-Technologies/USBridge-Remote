//go:build windows

package monitors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"usbridge_agent/internal/sessionlaunch"
)

// HelperArg is the first command-line argument that makes the agent executable
// run RunHelper instead of the agent. A service lives in Session 0, where
// EnumDisplayMonitors reports one made-up 1024x768 "WinDisc" monitor instead of
// the real desktop, so it asks a short-lived copy of itself in the interactive
// session (like displaypower's vdisplay-helper).
const HelperArg = "monitors-helper"

// helperResultPrefix starts the one stdout line carrying the snapshot as JSON.
const helperResultPrefix = "MONITORS-RESULT "

// snapshotTTL: List sits behind status polls, and starting a helper costs a
// process launch; a monitor change shows up within this long.
const snapshotTTL = 2 * time.Second

// snapshot is what the helper sees of the desktop: List, GDINames and
// LogicalOrigin all answered at once.
type snapshot struct {
	Monitors []Monitor         `json:"monitors"`
	GDI      []string          `json:"gdi"`
	Logical  map[string][2]int `json:"logical"`
}

func localSnapshot() (snapshot, error) {
	mons, err := listLocal()
	if err != nil {
		return snapshot{}, err
	}
	snap := snapshot{Monitors: mons, GDI: gdiNamesLocal(), Logical: map[string][2]int{}}
	for _, m := range mons {
		if x, y, ok := logicalOriginLocal(m.ID); ok {
			snap.Logical[m.ID] = [2]int{x, y}
		}
	}
	return snap, nil
}

// RunHelper prints the local desktop's snapshot as one result line and returns
// the process exit code.
func RunHelper() int {
	snap, err := localSnapshot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "monitors helper:", err)
		return 1
	}
	data, _ := json.Marshal(snap)
	fmt.Println(helperResultPrefix + string(data))
	return 0
}

func inSession0() bool {
	var sid uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sid); err != nil {
		return false
	}
	return sid == 0
}

var (
	snapMu   sync.Mutex
	snapAt   time.Time
	snapLast snapshot
)

// sessionSnapshot is the desktop as the interactive session sees it; ok is
// false when this process can see the desktop itself (not in Session 0) or the
// helper couldn't run (nobody logged on, helper failed) -- callers then use
// their own enumeration.
func sessionSnapshot() (snapshot, bool) {
	if !inSession0() {
		return snapshot{}, false
	}
	snapMu.Lock()
	defer snapMu.Unlock()
	if !snapAt.IsZero() && time.Since(snapAt) < snapshotTTL {
		return snapLast, true
	}
	exe, err := os.Executable()
	if err != nil {
		return snapshot{}, false
	}
	out, err := sessionlaunch.RunAndCaptureOutput(exe, []string{HelperArg}, "", nil, 15*time.Second)
	if err != nil {
		log.Printf("[monitors] session helper: %v", err)
		return snapshot{}, false
	}
	var snap snapshot
	for _, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if rest, ok := bytes.CutPrefix(line, []byte(helperResultPrefix)); ok {
			if err := json.Unmarshal(rest, &snap); err != nil {
				log.Printf("[monitors] session helper output: %v", err)
				return snapshot{}, false
			}
			snapLast, snapAt = snap, time.Now()
			return snap, true
		}
	}
	log.Printf("[monitors] session helper printed no result: %q", out)
	return snapshot{}, false
}
