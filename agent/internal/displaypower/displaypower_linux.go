//go:build linux

package displaypower

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"usbridge_agent/internal/vdisplay"
)

// On Linux the virtual monitor is vkms's connector (vdisplay): the agent forces
// it on for a session and off after, like the MttVDD monitor on Windows. The
// desktop side -- which monitor is primary, which are switched off -- goes
// through the compositor: KWin's kscreen-doctor (KDE, Wayland and X11). On
// another desktop List and the monitor switches report ErrUnsupported, while
// the virtual monitor itself still comes and goes with sessions.

// AgentVirtual: the agent brings the virtual monitor up and down for sessions
// itself (Windows, Linux); elsewhere the stream host does.
const AgentVirtual = true

type kscreenOutput struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	Priority  int    `json:"priority"`
	Size      struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"size"`
	Modes []struct {
		ID          string  `json:"id"`
		RefreshRate float64 `json:"refreshRate"`
		Size        struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"size"`
	} `json:"modes"`
}

func (o kscreenOutput) virtual() bool { return strings.HasPrefix(o.Name, "Virtual-") }

// kscreenOutputs lists the connected outputs as KWin sees them.
func kscreenOutputs() ([]kscreenOutput, error) {
	if _, err := exec.LookPath("kscreen-doctor"); err != nil {
		return nil, ErrUnsupported
	}
	out, err := exec.Command("kscreen-doctor", "-j").Output()
	if err != nil {
		return nil, fmt.Errorf("kscreen-doctor -j: %v", err)
	}
	var d struct {
		Outputs []kscreenOutput `json:"outputs"`
	}
	if err := json.Unmarshal(out, &d); err != nil {
		return nil, fmt.Errorf("kscreen-doctor -j: %v", err)
	}
	var connected []kscreenOutput
	for _, o := range d.Outputs {
		if o.Connected {
			connected = append(connected, o)
		}
	}
	return connected, nil
}

// kscreenSet applies settings, e.g. "output.HDMI-A-1.priority.1".
func kscreenSet(settings ...string) error {
	if len(settings) == 0 {
		return nil
	}
	out, err := exec.Command("kscreen-doctor", settings...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("kscreen-doctor %s: %v (%s)", strings.Join(settings, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// List reports every connected monitor. ID is the connector name ("HDMI-A-1").
func List() ([]Monitor, error) {
	outs, err := kscreenOutputs()
	if err != nil {
		return nil, err
	}
	monitors := make([]Monitor, 0, len(outs))
	for _, o := range outs {
		m := Monitor{ID: o.Name, Name: o.Name, Active: o.Enabled, Virtual: o.virtual()}
		if o.Enabled {
			m.GDIName = o.Name
			m.Primary = o.Priority == 1
			m.Width, m.Height = o.Size.Width, o.Size.Height
		}
		monitors = append(monitors, m)
	}
	return monitors, nil
}

func findOutput(outs []kscreenOutput, id string) (kscreenOutput, bool) {
	for _, o := range outs {
		if o.Name == id {
			return o, true
		}
	}
	return kscreenOutput{}, false
}

// SetEnabled switches one monitor on or off. Switching off the only active one
// is refused.
func SetEnabled(id string, on bool) error {
	outs, err := kscreenOutputs()
	if err != nil {
		return err
	}
	o, ok := findOutput(outs, id)
	if !ok {
		return ErrNotFound
	}
	if o.Enabled == on {
		return nil
	}
	if !on {
		active := 0
		for _, x := range outs {
			if x.Enabled {
				active++
			}
		}
		if active <= 1 {
			return ErrLastDisplay
		}
		return kscreenSet("output." + id + ".disable")
	}
	return kscreenSet("output." + id + ".enable")
}

// MakePrimary makes a monitor the primary display, switching it on first when
// it is off.
func MakePrimary(id string) error {
	outs, err := kscreenOutputs()
	if err != nil {
		return err
	}
	o, ok := findOutput(outs, id)
	if !ok {
		return ErrNotFound
	}
	if o.Enabled && o.Priority == 1 {
		return nil
	}
	settings := []string{}
	if !o.Enabled {
		settings = append(settings, "output."+id+".enable")
	}
	return kscreenSet(append(settings, "output."+id+".priority.1")...)
}

// waitVirtualOutput waits for KWin to list the vkms connector, enabled.
func waitVirtualOutput(connector string, timeout time.Duration) (kscreenOutput, error) {
	deadline := time.Now().Add(timeout)
	for {
		outs, err := kscreenOutputs()
		if err != nil {
			return kscreenOutput{}, err
		}
		if o, ok := findOutput(outs, connector); ok && o.Enabled {
			return o, nil
		}
		if time.Now().After(deadline) {
			return kscreenOutput{}, fmt.Errorf("the virtual monitor %s did not show up on the desktop", connector)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// closestModeID picks the output's mode nearest the wanted one: size first,
// refresh rate as the tie-breaker (vkms offers standard modes only).
func closestModeID(o kscreenOutput, m Mode) (string, Mode) {
	best, bestMode := "", Mode{}
	bestSize, bestHz := math.MaxFloat64, math.MaxFloat64
	for _, x := range o.Modes {
		dw, dh := float64(x.Size.Width-m.Width), float64(x.Size.Height-m.Height)
		size, hz := dw*dw+dh*dh, math.Abs(x.RefreshRate-float64(m.Hz))
		if size < bestSize || (size == bestSize && hz < bestHz) {
			best, bestSize, bestHz = x.ID, size, hz
			bestMode = Mode{Width: x.Size.Width, Height: x.Size.Height, Hz: int(math.Round(x.RefreshRate))}
		}
	}
	return best, bestMode
}

// AttachVirtual brings the vkms monitor onto the desktop in the mode closest to
// m, makes it primary (remembering the primary before) and with exclusive
// switches the others off.
func AttachVirtual(m Mode, primary, exclusive bool) (*VirtualState, error) {
	card, connector, err := vdisplay.Prepare()
	if err != nil {
		return nil, err
	}
	if err := vdisplay.Up(); err != nil {
		return nil, err
	}
	st := &VirtualState{GDIName: connector, FriendlyName: filepath.Base(card) + "-" + connector, DevicePath: card, Mode: m}
	o, err := waitVirtualOutput(connector, 5*time.Second)
	if errors.Is(err, ErrUnsupported) {
		return st, nil // not KDE: the monitor is up, the desktop is left as it arranges it
	}
	if err != nil {
		return st, err
	}
	if id, got := closestModeID(o, m); id != "" {
		st.Mode = got
		if err := kscreenSet("output." + connector + ".mode." + id); err != nil {
			log.Printf("[vdisplay] virtual monitor mode %s: %v", got, err)
		}
	}
	if primary {
		if err := st.SetPrimary(true); err != nil {
			return st, err
		}
	}
	if exclusive {
		return OthersOff(st)
	}
	return st, nil
}

// SetPrimary makes the attached virtual monitor the primary display (the one
// before is kept in Layout), or gives the role back.
func (st *VirtualState) SetPrimary(on bool) error {
	outs, err := kscreenOutputs()
	if err != nil {
		return err
	}
	if on {
		if len(st.Layout) == 0 {
			for _, o := range outs {
				if o.Enabled && o.Priority == 1 && !o.virtual() {
					st.Layout = []Placed{{Name: o.Name}}
				}
			}
		}
		return kscreenSet("output." + st.GDIName + ".priority.1")
	}
	err = restorePrimary(outs, st.Layout)
	st.Layout = nil
	return err
}

// restorePrimary makes the monitor recorded in layout the primary again, or,
// when it is gone, the first real active monitor -- never the virtual one.
func restorePrimary(outs []kscreenOutput, layout []Placed) error {
	if len(layout) > 0 {
		if o, ok := findOutput(outs, layout[0].Name); ok {
			return MakePrimary(o.Name)
		}
	}
	for _, o := range outs {
		if o.Enabled && o.Priority == 1 && !o.virtual() {
			return nil
		}
	}
	for _, o := range outs {
		if !o.virtual() && o.Enabled {
			return MakePrimary(o.Name)
		}
	}
	return nil
}

// OthersOff switches every monitor but the virtual one off; their names are
// kept in Topology. A no-op until the virtual monitor is active.
func OthersOff(st *VirtualState) (*VirtualState, error) {
	outs, err := kscreenOutputs()
	if err != nil {
		return st, err
	}
	virtualOn := false
	for _, o := range outs {
		if o.virtual() && o.Enabled {
			virtualOn = true
		}
	}
	if !virtualOn {
		return st, nil
	}
	if st == nil {
		st = &VirtualState{}
	}
	off := splitNames(st.Topology)
	var settings []string
	for _, o := range outs {
		if o.virtual() || !o.Enabled {
			continue
		}
		settings = append(settings, "output."+o.Name+".disable")
		if !contains(off, o.Name) {
			off = append(off, o.Name)
		}
	}
	st.Topology = strings.Join(off, ",")
	return st, kscreenSet(settings...)
}

// OthersOn switches the monitors OthersOff switched off back on.
func OthersOn(st *VirtualState) error {
	if st == nil || st.Topology == "" {
		return nil
	}
	outs, err := kscreenOutputs()
	if err != nil {
		return err
	}
	var settings []string
	for _, name := range splitNames(st.Topology) {
		if o, ok := findOutput(outs, name); ok && !o.Enabled {
			settings = append(settings, "output."+name+".enable")
		}
	}
	return kscreenSet(settings...)
}

// RestoreVirtual undoes AttachVirtual: the others back on, the primary role
// back on a real monitor, the virtual monitor off the desktop. With no state
// (a monitor left up with nothing recorded) it only takes the monitor off.
func RestoreVirtual(st *VirtualState) error {
	var errs []error
	if st != nil {
		if err := OthersOn(st); err != nil && !errors.Is(err, ErrUnsupported) {
			errs = append(errs, err)
		}
	}
	if outs, err := kscreenOutputs(); err == nil {
		var layout []Placed
		if st != nil {
			layout = st.Layout
		}
		if err := restorePrimary(outs, layout); err != nil {
			errs = append(errs, err)
		}
	}
	if err := vdisplay.Down(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// ParkVirtual: no driver install attaches a monitor on Linux.
func ParkVirtual(time.Duration) (bool, error) { return false, nil }

// PlannedVirtual names the virtual monitor before it is up: its connector
// ("Virtual-1") and card ("/dev/dri/card0").
func PlannedVirtual() (gdi, friendly string, err error) {
	card, connector, err := vdisplay.Prepare()
	return connector, card, err
}

func prepareHelperThread() {}

// watchGrace is how long the agent may be gone before the watcher puts the
// displays back: a restart has it running again well within.
const watchGrace = 20 * time.Second

// WatchAgent waits for the agent (pid) to exit; when no agent is running again
// within watchGrace, it puts the displays back as the state file records -- the
// agent died mid-session and nothing else would.
func WatchAgent(state string, pid int, _ string) error {
	for processAlive(pid) {
		time.Sleep(time.Second)
	}
	deadline := time.Now().Add(watchGrace)
	for time.Now().Before(deadline) {
		if otherAgentRunning() {
			return nil // the new agent takes over the state file
		}
		time.Sleep(time.Second)
	}
	st := LoadState(state)
	if st == nil {
		if monitors, err := List(); err == nil && !StrayVirtual(monitors) {
			return nil
		}
	}
	log.Printf("[vdisplay] the agent is gone -- putting the displays back")
	err := RestoreVirtual(st)
	if serr := saveState(state, nil); serr != nil && err == nil {
		err = serr
	}
	return err
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// otherAgentRunning reports whether an agent (this executable, not a helper)
// other than this process runs.
func otherAgentRunning() bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	self, _ = filepath.EvalSymlinks(self)
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil || strings.TrimSuffix(exe, " (deleted)") != self {
			continue
		}
		cmdline, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if !strings.Contains(string(cmdline), HelperArg) {
			return true
		}
	}
	return false
}

func splitNames(s string) []string {
	var out []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
