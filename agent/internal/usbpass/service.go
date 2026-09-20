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
	"runtime"
	"sync"
	"time"

	"usbridge_agent/internal/hwid"
)

const (
	DefaultURBPort     = 8090
	DefaultControlAddr = "127.0.0.1:18090"
)

type Status struct {
	Available   bool     `json:"available"`
	Platform    string   `json:"platform"`
	BrokerAlive bool     `json:"broker_alive"`
	StubDriver  bool     `json:"stub_driver"`
	VhciDriver  bool     `json:"vhci_driver"`
	ListenPort  int      `json:"listen_port"`
	Sessions    []string `json:"sessions"`
	BrokerError string   `json:"broker_error,omitempty"`
	DriverHint  string   `json:"driver_hint,omitempty"`
	// AttachGranted is false on Linux until the one-time polkit grant (see
	// access_linux.go) is in place; without it every attach/detach prompts.
	AttachGranted bool `json:"attach_granted"`
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
	urbPort     int
	controlAddr string
	tsnetBridge string
}

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
		urbPort:     urbPort,
		controlAddr: DefaultControlAddr,
		tsnetBridge: tsnetBridge,
	}
}

func (s *Service) ListenPort() int { return s.urbPort }

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
	if env := os.Getenv("USBRIDGE_USB_BROKER"); env != "" {
		candidates = append([]string{env}, candidates...)
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
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
	args := []string{
		"--role", "agent",
		"--listen", fmt.Sprintf("0.0.0.0:%d", s.urbPort),
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
		}
	}()
	return nil
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	s.cmd = nil
}

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
		ListenPort: s.urbPort,
	}
	st.AttachGranted = AttachAccessGranted()
	if !st.Available {
		st.DriverHint = "USB passthrough v1 is Windows/Linux only"
		return st
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
		return st
	}
	st.BrokerAlive = true
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
