package displaypower

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Mode is a display mode: size and refresh rate.
type Mode struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	Hz     int `json:"hz"`
}

func (m Mode) String() string { return fmt.Sprintf("%dx%d@%d", m.Width, m.Height, m.Hz) }

// ParseMode parses "WIDTHxHEIGHT@HZ" (the agent's "virtual:" output spec).
func ParseMode(spec string) (Mode, error) {
	res, hz, ok := strings.Cut(strings.TrimSpace(spec), "@")
	w, h, ok2 := strings.Cut(res, "x")
	if !ok || !ok2 {
		return Mode{}, fmt.Errorf("bad display mode %q, want WIDTHxHEIGHT@HZ", spec)
	}
	var m Mode
	var err1, err2, err3 error
	m.Width, err1 = strconv.Atoi(strings.TrimSpace(w))
	m.Height, err2 = strconv.Atoi(strings.TrimSpace(h))
	m.Hz, err3 = strconv.Atoi(strings.TrimSpace(hz))
	if err := errors.Join(err1, err2, err3); err != nil || m.Width <= 0 || m.Height <= 0 || m.Hz <= 0 {
		return Mode{}, fmt.Errorf("bad display mode %q, want WIDTHxHEIGHT@HZ", spec)
	}
	return m, nil
}

// Placed is one active display's GDI name and desktop position.
type Placed struct {
	Name string `json:"name"`
	X    int32  `json:"x"`
	Y    int32  `json:"y"`
}

// VirtualState is what AttachVirtual did, for SetPrimary and RestoreVirtual to undo.
// It is JSON so it can be kept in a file between helper processes (RunHelper).
type VirtualState struct {
	// GDIName is the virtual monitor's `\\.\DISPLAYn` (it changes whenever the driver
	// restarts, so it is looked up again on every attach).
	GDIName string `json:"gdi_name"`
	// FriendlyName is its EDID name, e.g. "VDD by MTT": stable, so a stream host's
	// output_name can point at it.
	FriendlyName string `json:"friendly_name"`
	// DevicePath is its monitor device path (`\\?\DISPLAY#MTT1337#...`).
	DevicePath string `json:"device_path"`
	// Mode is the mode it got: the requested one, or the driver's closest.
	Mode Mode `json:"mode"`
	// Layout is the desktop from before the virtual monitor was made primary; nil when
	// it was not.
	Layout []Placed `json:"layout,omitempty"`
	// Topology is the active display config from before the other monitors were
	// switched off (raw paths and modes); "" when they were not.
	Topology string `json:"topology,omitempty"`
}

// HelperArg is the first command-line argument that makes the agent executable run
// RunHelper instead of the agent. The display calls need the interactive session (a
// Session 0 service gets ERROR_ACCESS_DENIED from the CCD API), so the service runs
// them in a short-lived copy of itself there, like it runs the stream host.
const HelperArg = "vdisplay-helper"

// HelperResultPrefix starts the one stdout line carrying RunHelper's result as JSON;
// everything else on the output is log.
const HelperResultPrefix = "VDISPLAY-RESULT "

// HelperResult is that line's JSON.
type HelperResult struct {
	State    *VirtualState `json:"state,omitempty"`
	Monitors []Monitor     `json:"monitors,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// LoadState reads a state file; nil when there is none.
func LoadState(path string) *VirtualState {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var st VirtualState
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	return &st
}

func saveState(path string, st *VirtualState) error {
	if st == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RunHelper runs one helper command (args after HelperArg) and prints its result line:
//
//	apply STATEFILE                       restore and detach (whatever STATEFILE recorded)
//	apply STATEFILE WxH@HZ PRIMARY EXCL   ...then attach in that mode (PRIMARY/EXCL: 0 or 1)
//	primary STATEFILE 0|1                 make the attached monitor primary, or undo that
//	others STATEFILE 0|1                  monitors around a virtual one: off (0), back on (1)
//	list -                                every monitor (List)
//	enable - MONITOR_ID 0|1               switch one monitor off or on (SetEnabled)
//	make-primary - MONITOR_ID             make one active monitor the primary display
//	vname -                               the virtual monitor's GDI/friendly name, attached or not
//	park -                                detach the virtual monitor a driver install attached
//	watch STATEFILE AGENT_PID SERVICE     restore STATEFILE once the agent died and stayed down
//
// The state file is rewritten after every change, so a crash in between never loses
// what there is to undo. Returns the process exit code.
func RunHelper(args []string) int {
	prepareHelperThread()
	res := runHelper(args)
	data, _ := json.Marshal(res)
	fmt.Println(HelperResultPrefix + string(data))
	if res.Error != "" {
		return 1
	}
	return 0
}

func runHelper(args []string) HelperResult {
	if len(args) < 2 {
		return HelperResult{Error: "usage: apply|primary STATEFILE ..."}
	}
	cmd, path := args[0], args[1]
	switch cmd {
	case "list":
		m, err := List()
		if err != nil {
			return HelperResult{Error: err.Error()}
		}
		return HelperResult{Monitors: m}
	case "watch":
		// The agent's stand-in when it dies mid-session: see WatchAgent.
		if len(args) < 4 {
			return HelperResult{Error: "usage: watch STATEFILE AGENT_PID SERVICE"}
		}
		pid, err := strconv.Atoi(args[2])
		if err != nil {
			return HelperResult{Error: err.Error()}
		}
		if err := WatchAgent(path, pid, args[3]); err != nil {
			return HelperResult{Error: err.Error()}
		}
		return HelperResult{}
	case "park":
		// After a driver install (the agent setup): see ParkVirtual.
		parked, err := ParkVirtual(20 * time.Second)
		if err != nil {
			return HelperResult{Error: err.Error()}
		}
		if parked {
			log.Printf("[vdisplay] the virtual monitor the driver install attached is detached")
		}
		return HelperResult{}
	case "vname":
		gdi, friendly, err := PlannedVirtual()
		if err != nil {
			return HelperResult{Error: err.Error()}
		}
		return HelperResult{State: &VirtualState{GDIName: gdi, FriendlyName: friendly}}
	case "make-primary":
		if len(args) < 3 {
			return HelperResult{Error: "usage: make-primary - MONITOR_ID"}
		}
		if err := MakePrimary(args[2]); err != nil {
			return HelperResult{Error: err.Error()}
		}
		return HelperResult{}
	case "enable":
		if len(args) < 4 {
			return HelperResult{Error: "usage: enable - MONITOR_ID 0|1"}
		}
		if err := SetEnabled(args[2], args[3] == "1"); err != nil {
			return HelperResult{Error: err.Error()}
		}
		return HelperResult{}
	}
	st := LoadState(path)
	switch cmd {
	case "apply":
		if err := RestoreVirtual(st); err != nil {
			return HelperResult{Error: "restore: " + err.Error()}
		}
		if err := saveState(path, nil); err != nil {
			return HelperResult{Error: err.Error()}
		}
		if len(args) < 5 {
			return HelperResult{}
		}
		m, err := ParseMode(args[2])
		if err != nil {
			return HelperResult{Error: err.Error()}
		}
		st, err := AttachVirtual(m, args[3] == "1", args[4] == "1")
		if st != nil {
			if serr := saveState(path, st); serr != nil && err == nil {
				err = serr
			}
		}
		if err != nil {
			return HelperResult{State: st, Error: err.Error()}
		}
		return HelperResult{State: st}
	case "others":
		// The other monitors around a virtual one the stream host attached itself:
		// "others STATEFILE 0" switches them off, "others STATEFILE 1" back on.
		if len(args) < 3 {
			return HelperResult{Error: "usage: others STATEFILE 0|1"}
		}
		if args[2] == "1" {
			err := OthersOn(st)
			if serr := saveState(path, nil); serr != nil && err == nil {
				err = serr
			}
			if err != nil {
				return HelperResult{Error: err.Error()}
			}
			return HelperResult{}
		}
		st, err := OthersOff(st)
		if st != nil && st.Topology != "" {
			if serr := saveState(path, st); serr != nil && err == nil {
				err = serr
			}
		}
		if err != nil {
			return HelperResult{State: st, Error: err.Error()}
		}
		return HelperResult{State: st}
	case "primary":
		if st == nil {
			return HelperResult{}
		}
		if len(args) < 3 {
			return HelperResult{Error: "usage: primary STATEFILE 0|1"}
		}
		err := st.SetPrimary(args[2] == "1")
		if serr := saveState(path, st); serr != nil && err == nil {
			err = serr
		}
		if err != nil {
			return HelperResult{State: st, Error: err.Error()}
		}
		return HelperResult{State: st}
	}
	return HelperResult{Error: "unknown command " + cmd}
}

// ParseHelperOutput finds the result line in a helper's output.
func ParseHelperOutput(out []byte) (HelperResult, error) {
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if rest, ok := strings.CutPrefix(line, HelperResultPrefix); ok {
			var res HelperResult
			if err := json.Unmarshal([]byte(rest), &res); err != nil {
				return HelperResult{}, err
			}
			return res, nil
		}
	}
	return HelperResult{}, fmt.Errorf("no result from the display helper; its output: %s", strings.TrimSpace(string(out)))
}
