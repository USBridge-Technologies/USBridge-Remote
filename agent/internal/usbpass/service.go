// Package usbpass is a thin GPLv3 shell around the closed rust-shine
// usbridge-usb-broker binary. No URB codec, crypto, or license logic lives
// here — those stay in rust-shine.
package usbpass

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"usbridge_agent/internal/hwid"
)

const (
	DefaultURBPort     = 8090
	DefaultControlAddr = "127.0.0.1:18090"
	// urbPortFallbacks is how many ports above the configured one Start()
	// tries when the configured port is already taken by an unrelated
	// process (see pickURBPort).
	urbPortFallbacks = 20
)

type Status struct {
	Available   bool     `json:"available"`
	Platform    string   `json:"platform"`
	BrokerAlive bool     `json:"broker_alive"`
	StubDriver  bool     `json:"stub_driver"`
	VhciDriver  bool     `json:"vhci_driver"`
	ListenPort  int      `json:"listen_port"`
	// ConfiguredPort is usb_passthrough_port; differs from ListenPort when
	// pickURBPort had to fall back because something else held it.
	ConfiguredPort int `json:"configured_port,omitempty"`
	Sessions    []string `json:"sessions"`
	BrokerError string   `json:"broker_error,omitempty"`
	// BrokerLastExit is the broker's own last error line (from broker.log)
	// when it has crashed -- BrokerError alone is just "control dial
	// refused", which says nothing about why.
	BrokerLastExit string `json:"broker_last_exit,omitempty"`
	DriverHint     string `json:"driver_hint,omitempty"`
	// AttachGranted is false on Linux until the one-time polkit grant (see
	// access_linux.go) is in place; without it every attach/detach prompts.
	AttachGranted bool `json:"attach_granted"`
	// ConsentGiven is the user's one-time, explicit consent to run this
	// closed binary at all (see config.Config.USBBrokerConsentGiven,
	// App.EnableUSBBroker) -- set by App.USBPassthroughStatus, not by this
	// package itself (Service has no access to Config). false means the
	// binary must never be staged/started, regardless of license tier; the
	// UI shows the consent button instead of the usual status row while
	// this is false.
	ConsentGiven bool `json:"consent_given"`
	// Dongle is the USBridge USB/IP dongle (hardware importer, see
	// rust-shine's usb_passthrough::dongle) as the broker sees it; nil when
	// none is plugged in. It stands in for the OS VHCI driver, and is the
	// only importer there is on macOS.
	Dongle *DongleStatus `json:"dongle,omitempty"`
}

type DongleStatus struct {
	Firmware      string `json:"fw,omitempty"`
	Serial        string `json:"serial,omitempty"`
	Attached      bool   `json:"attached"`
	USBConfigured bool   `json:"usb_configured"`
	BusID         string `json:"bus_id,omitempty"`
	VID           string `json:"vid,omitempty"`
	PID           string `json:"pid,omitempty"`
	// Error is set when something answers on the dongle's address but
	// cannot be used (e.g. firmware speaking another protocol version).
	Error string `json:"error,omitempty"`
}

func parseDongleStatus(v any) *DongleStatus {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	str := func(k string) string { s, _ := m[k].(string); return s }
	flag := func(k string) bool { b, _ := m[k].(bool); return b }
	return &DongleStatus{
		Firmware:      str("fw"),
		Serial:        str("serial"),
		Attached:      flag("attached"),
		USBConfigured: flag("usb_configured"),
		BusID:         str("bus_id"),
		VID:           str("vid"),
		PID:           str("pid"),
		Error:         str("error"),
	}
}

type Device struct {
	BusID         string `json:"bus_id"`
	VID           string `json:"vid"`
	PID           string `json:"pid"`
	Description   string `json:"description"`
	Protected     bool   `json:"protected"`
	PreferredTest bool   `json:"preferred_test"`
}

type Service struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	exe         string
	stateDir    string
	exeDir      string
	secret      string
	basePort    int // configured URB port (usb_passthrough_port)
	urbPort     int // port the broker is actually told to listen on
	controlAddr string
	tsnetBridge string
	// lastExit is the broker's own reason for its last crash (last
	// broker.log line), shown in the agent UI; cleared once it answers.
	lastExit string
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// tsnetBridge is the Go agent's UsbTunnelBridge loopback address (see
// agent/internal/tailscale/usb_bridge.go) — empty disables it, which just
// means USB passthrough over Tailscale won't work (Direct/LAN attaches
// never need it, so they're unaffected either way).
func New(exeDir, stateDir, secret string, urbPort int, tsnetBridge string) *Service {
	if urbPort <= 0 {
		urbPort = DefaultURBPort
	}
	return &Service{
		exeDir:      exeDir,
		stateDir:    stateDir,
		secret:      secret,
		basePort:    urbPort,
		urbPort:     urbPort,
		controlAddr: DefaultControlAddr,
		tsnetBridge: tsnetBridge,
	}
}

// ListenPort is the port the broker is actually listening on -- normally the
// configured one, but a fallback when that was taken (see pickURBPort).
// Clients must use this (it's in Status and the session reply), not their
// own copy of the configured port.
func (s *Service) ListenPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.urbPort
}

// killOrphanBrokers kills brokers this Service holds no handle to -- left by
// an agent that was force-killed (Windows doesn't take children down with
// the parent). Only called from Start() when s.cmd is nil. Confirmed live:
// each agent restart leaked one broker; the orphan kept 127.0.0.1:18090 and
// the URB port, so the next broker moved to the next URB port, failed its
// control bind, and Status() was really talking to the orphan -- four
// brokers on 8091..8094 after three restarts. Mirrors streamhost's
// killOrphanStreamerProcesses. Waits briefly for the control port to be
// released so pickURBPort doesn't see the orphan's ports as still taken.
func (s *Service) killOrphanBrokers() {
	if !killBrokersByName(brokerName()) {
		return
	}
	log.Printf("[usbpass] killed orphaned %s before starting fresh", brokerName())
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		ln, err := net.Listen("tcp4", s.controlAddr)
		if err == nil {
			ln.Close()
			return
		}
	}
}

// pickURBPort returns the configured URB port if it can be bound right now,
// otherwise the first free one of the next urbPortFallbacks ports. Confirmed
// live: Wondershare NativePush (WsToastNotification.exe, installed with
// Filmora) listens on 0.0.0.0:8090 from login onward, so the broker died on
// bind every watchdog tick, forever. Falls back to the configured port if
// nothing in range is free, so the broker's own bind error still surfaces.
func (s *Service) pickURBPort() int {
	for p := s.basePort; p <= s.basePort+urbPortFallbacks; p++ {
		// "tcp4", not "tcp": for a wildcard address Go opens a dual-stack
		// [::] socket, which Windows happily binds next to someone else's
		// IPv4 0.0.0.0:p -- the probe then says "free" while the broker's
		// own IPv4 bind still fails (confirmed live against Wondershare).
		ln, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", p))
		if err != nil {
			continue
		}
		ln.Close()
		if p != s.basePort {
			who := whatHoldsPort(s.basePort)
			if who == "" {
				who = "another process"
			}
			log.Printf("[usbpass] URB port %d is held by %s -- broker will listen on %d instead", s.basePort, who, p)
		}
		return p
	}
	return s.basePort
}

func brokerName() string {
	if runtime.GOOS == "windows" {
		return "usbridge-usb-broker.exe"
	}
	return "usbridge-usb-broker"
}

func (s *Service) resolveBroker() string {
	candidates := []string{
		filepath.Join(s.stateDir, "usb-broker", brokerName()),
		filepath.Join(s.exeDir, "usb-broker", brokerName()),
		filepath.Join(s.exeDir, brokerName()),
	}
	// Never one named by an environment variable: the broker runs as SYSTEM
	// on Windows, so only the signed build the agent staged or shipped runs.
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// promoteBrokerSidecar puts an update staged beside the broker in its place:
// on Windows an update that lands while the broker runs is staged as
// exe+".new" (entitlement's extract), and nothing else ever swapped it in --
// the broker kept running the old build indefinitely. Called with no broker
// running (killOrphanBrokers first). The image lock of one just killed can
// linger, so the rename is retried; past that, the old file is moved aside
// (Windows lets a locked .exe be renamed, not replaced) and removed on a
// later start. The broker always runs under its own name, so the next
// update's kill by name finds it.
func promoteBrokerSidecar(exe string) string {
	old := exe + ".old"
	_ = os.Remove(old) // left by an earlier swap, unlocked by now
	sidecar := exe + ".new"
	if _, err := os.Stat(sidecar); err != nil {
		return exe
	}
	var err error
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(250 * time.Millisecond) {
		if err = os.Rename(sidecar, exe); err == nil {
			log.Printf("[usbpass] updated %s from its staged update", filepath.Base(exe))
			return exe
		}
		if time.Now().After(deadline) {
			break
		}
	}
	if err2 := os.Rename(exe, old); err2 == nil {
		if err = os.Rename(sidecar, exe); err == nil {
			log.Printf("[usbpass] updated %s from its staged update (the old one was still locked; moved aside)", filepath.Base(exe))
			return exe
		}
		_ = os.Rename(old, exe)
	}
	log.Printf("[usbpass] could not swap in the staged broker update (%v) -- keeping the current one until the next start", err)
	return exe
}

// Restart stops the broker and starts it again (picking up a staged update).
func (s *Service) Restart() error {
	s.Stop()
	return s.Start()
}

// Staged reports whether the usb-broker binary is present on disk.
func (s *Service) Staged() bool { return s.resolveBroker() != "" }

func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		return nil
	}
	exe := s.resolveBroker()
	if exe == "" {
		return fmt.Errorf("usbridge-usb-broker not staged (closed rust-shine binary)")
	}
	s.killOrphanBrokers()
	exe = promoteBrokerSidecar(exe)
	port := s.pickURBPort()
	s.urbPort = port
	args := []string{
		"--role", "agent",
		"--listen", fmt.Sprintf("0.0.0.0:%d", port),
		"--control", s.controlAddr,
		"--secret", s.secret,
	}
	if s.tsnetBridge != "" {
		args = append(args, "--tsnet-bridge", s.tsnetBridge)
	}
	if os.Getenv("USBRIDGE_USB_ALLOW_UNLICENSED") == "1" {
		args = append(args, "--allow-unlicensed")
	} else {
		// Forward the same token file RustShine already uses. The broker
		// (rust-shine) is what checks pro/enterprise — Go never inspects the token.
		token := filepath.Join(s.stateDir, "rustshine", "entitlement.token")
		if st, err := os.Stat(token); err == nil && !st.IsDir() {
			args = append(args, "--entitlement-file", token)
			if hw, err := hwid.Get(); err == nil && hw != "" {
				args = append(args, "--hardware-id", hw)
			}
		}
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	// usbridge-usb-broker.exe is a console-subsystem binary; launched from
	// this (GUI-subsystem) agent process without this, Windows allocates it
	// a brand new, visible console window that just sits there for the
	// broker's whole lifetime -- confirmed live. hideBrokerWindow is a
	// no-op on non-Windows (see exec_others.go).
	hideBrokerWindow(cmd)
	// cmd.Stdout/Stderr were left nil, which per os/exec's own doc comment
	// means Go connects them to the null device -- so the broker's own
	// error!() logging (rust-shine's main.rs logs the entitlement-check
	// failure reason via tracing before exiting 1) was being thrown away
	// unconditionally, regardless of what it actually printed. Confirmed
	// live: the agent never had a chance to see it. A dedicated file next
	// to the broker binary captures it from here on.
	var logFile *os.File
	if f, err := os.OpenFile(filepath.Join(cmd.Dir, "broker.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		logFile = f
		cmd.Stdout = f
		cmd.Stderr = f
	} else {
		log.Printf("[usbpass] warning: could not open broker.log: %v", err)
	}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return err
	}
	s.cmd = cmd
	s.exe = exe
	startedAt := time.Now()
	logPath := filepath.Join(cmd.Dir, "broker.log")
	// cmd.Wait()'s result used to be discarded outright, which meant a
	// broker that exits immediately after a successful fork (e.g. the
	// entitlement/enterprise gate inside rust-shine rejecting) was
	// completely invisible here: cmd.Start() had already returned nil, so
	// Start() reported success, and the Process!=nil guard above then
	// permanently refused to ever launch it again for the rest of this
	// agent's lifetime, even though the OS process was long dead. Logging
	// the outcome and clearing s.cmd lets both the log and a later Start()
	// call (e.g. a user retry) see reality.
	go func() {
		err := cmd.Wait()
		if logFile != nil {
			logFile.Close()
		}
		s.mu.Lock()
		if s.cmd == cmd {
			s.cmd = nil
		}
		s.mu.Unlock()
		if err != nil {
			log.Printf("[usbpass] broker exited: %v", err)
			reason := err.Error()
			if tail := tailFile(logPath, 1); len(tail) == 1 {
				reason = tail[0]
			}
			s.mu.Lock()
			s.lastExit = reason
			s.mu.Unlock()
			// A crash within a couple seconds of launch is the signature of
			// a startup-time failure (most commonly: another process already
			// holds the URB port -- confirmed live on a Windows test machine
			// where WsToastNotification.exe raced the broker for port 8090
			// on every boot, silently, for over an hour, with nothing but
			// this one now-orphaned "exit status 1" anywhere in the agent's
			// own log to go on). broker.log has the real reason (rust-shine
			// logs its own bind error via tracing before exiting), but
			// nobody reads a separate per-subprocess log file proactively --
			// surface its last lines here, in the log actually being
			// watched, and identify whatever's squatting the port while
			// we're at it so the fix doesn't need manual netstat/tasklist
			// archaeology next time.
			if time.Since(startedAt) < 5*time.Second {
				for _, line := range tailFile(logPath, 6) {
					log.Printf("[usbpass] broker.log: %s", line)
				}
				if who := whatHoldsPort(port); who != "" {
					log.Printf("[usbpass] port %d is held by %s -- that's almost certainly why the broker failed to bind it", port, who)
				}
			}
		}
	}()
	return nil
}

// tailFile returns the last n non-empty lines of path, oldest first, or nil
// if it can't be read -- best-effort diagnostic, never fatal.
func tailFile(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// rust-shine's tracing output is ANSI-colored even into a file.
	text := ansiEscape.ReplaceAllString(string(data), "")
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(text, "\r", ""), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func (s *Service) Stop() {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	// A hard Kill() (SIGKILL on Unix, TerminateProcess on Windows) gives the
	// broker no chance to detach a hardware dongle it has a device cloned
	// onto. Confirmed live on macOS: leaving the clone enumerated like that
	// can wedge the Mac's own USB HID stack badly enough that even the
	// built-in trackpad and keyboard stop responding, until something
	// reopens the dongle's port and detaches it. Ask nicely first --
	// rust-shine's main.rs handles SIGTERM by detaching before it exits --
	// and only fall back to an unconditional kill if that doesn't work
	// (Windows has no equivalent of SIGTERM Process.Signal can deliver, so
	// this falls through to Kill() immediately there).
	if err := cmd.Process.Signal(syscall.SIGTERM); err == nil {
		exited := make(chan struct{})
		go func() {
			for {
				s.mu.Lock()
				stillOurs := s.cmd == cmd
				s.mu.Unlock()
				if !stillOurs {
					close(exited)
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
		}()
		select {
		case <-exited:
			return
		case <-time.After(3 * time.Second):
			log.Printf("[usbpass] broker did not exit within 3s of SIGTERM, killing it")
		}
	}
	_ = cmd.Process.Kill()
	s.mu.Lock()
	if s.cmd == cmd {
		s.cmd = nil
	}
	s.mu.Unlock()
}

// controlTimeout bounds one control round trip. The slowest honest answer
// is a status that probes a plugged-in dongle (a couple of seconds).
var controlTimeout = 5 * time.Second

func (s *Service) control(cmd string, extra map[string]any) (map[string]any, error) {
	payload := map[string]any{"cmd": cmd}
	for k, v := range extra {
		payload[k] = v
	}
	raw, _ := json.Marshal(payload)
	conn, err := net.DialTimeout("tcp", s.controlAddr, 800*time.Millisecond)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	// Without a deadline a broker that accepts the connection and then
	// never answers held this call forever -- and with it the GUI's whole
	// refresh (ui.Window.performRefresh waits on Status), so every
	// permission showed as missing and its Grant button seemed dead.
	// Confirmed live: the broker's status handler starved on a lock its
	// own dongle reader held across a sleep.
	_ = conn.SetDeadline(time.Now().Add(controlTimeout))
	_, _ = conn.Write(append(raw, '\n'))
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(line, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) Status() Status {
	st := Status{
		Available:  runtime.GOOS == "windows" || runtime.GOOS == "linux",
		Platform:   runtime.GOOS,
		ListenPort:     s.ListenPort(),
		ConfiguredPort: s.basePort,
	}
	st.AttachGranted = AttachAccessGranted()
	// Without an OS VHCI (macOS) the only importer is the hardware dongle,
	// which the broker finds -- so keep going and ask it.
	nativeVHCI := st.Available
	if !nativeVHCI {
		st.DriverHint = "plug in the USBridge USB/IP dongle (this OS has no USB/IP driver)"
	}
	if runtime.GOOS == "linux" {
		// Go-side check rather than the broker's own "status" reply --
		// vhci_driver in that reply is populated by the Windows build's
		// pnputil probe (driver_windows.go); the Linux broker never
		// bothered echoing it back since Go can check /sys directly.
		if vhci, hint := s.linuxDriverStatus(); vhci {
			st.VhciDriver = true
		} else {
			st.DriverHint = hint
		}
	}
	if s.resolveBroker() == "" {
		st.BrokerError = "closed usb-broker binary not staged"
		return st
	}
	resp, err := s.control("status", nil)
	if err != nil {
		st.BrokerError = err.Error()
		s.mu.Lock()
		st.BrokerLastExit = s.lastExit
		s.mu.Unlock()
		return st
	}
	st.BrokerAlive = true
	s.mu.Lock()
	s.lastExit = ""
	s.mu.Unlock()
	if v, ok := resp["stub_driver"].(bool); ok {
		st.StubDriver = v
	}
	if runtime.GOOS == "windows" {
		if v, ok := resp["vhci_driver"].(bool); ok {
			st.VhciDriver = v
		}
		if !st.VhciDriver {
			st.DriverHint = "install attested usbip-win VHCI via pnputil"
		}
	}
	if st.Dongle = parseDongleStatus(resp["dongle"]); st.Dongle != nil && st.Dongle.Error == "" {
		st.Available = true
		// The dongle is the importer, so no driver is missing; the broker
		// uses it whenever the OS VHCI is absent.
		if !st.VhciDriver {
			st.VhciDriver = true
			st.DriverHint = ""
		}
	}
	if arr, ok := resp["sessions"].([]any); ok {
		for _, x := range arr {
			if s, ok := x.(string); ok {
				st.Sessions = append(st.Sessions, s)
			}
		}
	}
	return st
}

func (s *Service) BrokerDir() string {
	if p := s.resolveBroker(); p != "" {
		return filepath.Dir(p)
	}
	return filepath.Join(s.stateDir, "usb-broker")
}
