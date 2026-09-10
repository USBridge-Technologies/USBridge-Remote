// Package usbpass: thin GPLv3 shell around closed rust-shine usb-broker
// plus an in-process USB/IP v1.1.1 export server (no external usbipd-win).
package usbpass

import (
	"bufio"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"usbridge-client/internal/models"
)

func brokerName() string {
	if runtime.GOOS == "windows" {
		return "usbridge-usb-broker.exe"
	}
	return "usbridge-usb-broker"
}

func ResolveBroker() string {
	if env := os.Getenv("USBRIDGE_USB_BROKER"); env != "" {
		return env
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, p := range []string{
			filepath.Join(dir, "usb-broker", brokerName()),
			filepath.Join(dir, brokerName()),
		} {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

func ListLocal() ([]models.USBPassthroughDevice, error) {
	exe := ResolveBroker()
	if exe == "" {
		return nil, fmt.Errorf("usb-broker not staged")
	}
	cmd := exec.Command(exe, "--list")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var devices []models.USBPassthroughDevice
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	header := true
	for sc.Scan() {
		line := sc.Text()
		if header {
			header = false
			continue
		}
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 5 {
			continue
		}
		vidpid := parts[1]
		vid, pid := vidpid, ""
		if i := strings.IndexByte(vidpid, ':'); i >= 0 {
			vid, pid = vidpid[:i], vidpid[i+1:]
		}
		inst := parts[0]
		devices = append(devices, models.USBPassthroughDevice{
			BusID:         StableUSBIPBusID(inst),
			InstanceID:    inst,
			VID:           vid,
			PID:           pid,
			Protected:     parts[2] == "true",
			PreferredTest: parts[3] == "true",
			Description:   parts[4],
		})
	}
	return devices, nil
}

// StableUSBIPBusID maps a Windows instance id to a Linux-style busid (N-M)
// that usbip-win2 VHCI expects in PLUGIN_HARDWARE.
func StableUSBIPBusID(instanceID string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(instanceID))
	v := h.Sum32()
	bus := (v % 9) + 1
	port := (v / 9) % 200 + 1
	return strconv.FormatUint(uint64(bus), 10) + "-" + strconv.FormatUint(uint64(port), 10)
}

// AttachOptions configures the closed rust-shine AES client (USB/IP client
// lives on the agent; this process only tells it where our Go export listens).
type AttachOptions struct {
	AgentAddr     string
	Secret        string
	InstanceID    string // Windows SetupAPI id for --bus-id
	USBIPBusID    string // Linux-style id advertised by our export server
	ExportService string // default 3240
	AllowUnlicensed bool
}

var (
	attachMu   sync.Mutex
	attachCmd  *exec.Cmd
)

// Attach starts rust-shine --role client and blocks until it exits.
// The Go USB/IP export server must already be listening.
func Attach(opts AttachOptions) error {
	exe := ResolveBroker()
	if exe == "" {
		return fmt.Errorf("usb-broker not staged")
	}
	if opts.ExportService == "" {
		opts.ExportService = "3240"
	}
	if opts.USBIPBusID == "" {
		opts.USBIPBusID = StableUSBIPBusID(opts.InstanceID)
	}
	args := []string{
		"--role", "client",
		"--agent-addr", opts.AgentAddr,
		"--secret", opts.Secret,
		"--bus-id", opts.InstanceID,
		"--usbip-bus-id", opts.USBIPBusID,
		"--export-service", opts.ExportService,
		// export_host empty → agent uses AES peer IP (Direct/Tailscale).
	}
	if opts.AllowUnlicensed {
		args = append(args, "--allow-unlicensed")
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	attachMu.Lock()
	attachCmd = cmd
	attachMu.Unlock()
	err := cmd.Run()
	attachMu.Lock()
	attachCmd = nil
	attachMu.Unlock()
	return err
}

// StopAttach kills a running rust client attach process, if any.
func StopAttach() {
	attachMu.Lock()
	defer attachMu.Unlock()
	if attachCmd != nil && attachCmd.Process != nil {
		_ = attachCmd.Process.Kill()
	}
	attachCmd = nil
}
