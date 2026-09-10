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

	"github.com/sirupsen/logrus"
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
		if st, err := os.Stat(env); err == nil && !st.IsDir() {
			return env
		}
	}
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	// AppImage: os.Executable is inside the read-only squashfs mount; also
	// look next to the .AppImage file and under APPDIR/usr/bin.
	if appImage := os.Getenv("APPIMAGE"); appImage != "" {
		dirs = append(dirs, filepath.Dir(appImage))
	}
	if appDir := os.Getenv("APPDIR"); appDir != "" {
		dirs = append(dirs, filepath.Join(appDir, "usr", "bin"), appDir)
	}
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
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
	if exe != "" {
		devices, err := listViaBroker(exe)
		if err == nil {
			return devices, nil
		}
		// Fall through to platform enumeration when the closed helper
		// is present but --list fails (wrong arch, missing deps, …).
	}
	if runtime.GOOS == "linux" {
		return listSysfs()
	}
	if exe == "" {
		return nil, fmt.Errorf("usb-broker not staged")
	}
	return nil, fmt.Errorf("usb-broker --list failed")
}

func listViaBroker(exe string) ([]models.USBPassthroughDevice, error) {
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
		busID := inst
		// Windows SetupAPI instance ids are not USB/IP busids; hash them.
		// Linux (and our broker --list on Linux) already emits N-M busids.
		if runtime.GOOS == "windows" || strings.Contains(inst, "\\") || strings.Contains(strings.ToUpper(inst), "USB\\") {
			busID = StableUSBIPBusID(inst)
		}
		devices = append(devices, models.USBPassthroughDevice{
			BusID:         busID,
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

// Attach starts rust-shine --role client in the background and returns once
// the process has started. The broker stays up for the life of the session;
// StopAttach / StopSession kill it. (Blocking on cmd.Run kept the Devices
// UI locked in beginOperation for the entire passthrough lifetime.)
func Attach(opts AttachOptions) error {
	exe := ResolveBroker()
	if exe == "" {
		return fmt.Errorf("usbridge-usb-broker not staged next to the client (needed for AES attach to the Windows agent VHCI)")
	}
	if opts.ExportService == "" {
		opts.ExportService = "3240"
	}
	if opts.USBIPBusID == "" {
		opts.USBIPBusID = StableUSBIPBusID(opts.InstanceID)
	}
	// Drop any previous attach (tracked + orphans) before starting a new one.
	StopAttach()
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
	setAttachProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start usb-broker client: %w", err)
	}
	attachMu.Lock()
	attachCmd = cmd
	attachMu.Unlock()
	go func() {
		err := cmd.Wait()
		attachMu.Lock()
		if attachCmd == cmd {
			attachCmd = nil
		}
		attachMu.Unlock()
		if err != nil {
			logrus.Warnf("usbpass: broker client exited: %v", err)
		} else {
			logrus.Infof("usbpass: broker client exited")
		}
	}()
	logrus.Infof("usbpass: broker client started pid=%d agent=%s bus=%s usbip=%s",
		cmd.Process.Pid, opts.AgentAddr, opts.InstanceID, opts.USBIPBusID)
	return nil
}

// StopAttach kills a running rust client attach process (and orphans).
func StopAttach() {
	attachMu.Lock()
	cmd := attachCmd
	attachCmd = nil
	attachMu.Unlock()
	if cmd != nil && cmd.Process != nil {
		killAttachProcess(cmd.Process)
	}
	killOrphanClientBrokers()
}
