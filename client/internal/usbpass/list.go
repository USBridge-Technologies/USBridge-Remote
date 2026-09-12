// Package usbpass: in-process USB/IP v1.1.1 export server (no external
// usbipd-win) plus a pure-Go AES-GCM client for the closed rust-shine
// agent's control plane (see usbaes_attach.go). The only remaining use of
// the closed usbridge-usb-broker binary is as an optional --list fallback
// on platforms without native enumeration (see listViaBroker below);
// Attach/StopAttach no longer spawn or depend on it at all.
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
	if runtime.GOOS == "darwin" {
		return listHIDDarwin()
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

// AttachOptions configures the AES attach to the agent's control plane.
// Attach/StopAttach live in usbaes_attach.go (Go, linux/windows) or
// usbaes_attach_stub.go (everywhere else) — see those files.
type AttachOptions struct {
	AgentAddr       string
	Secret          string
	InstanceID      string // Windows SetupAPI id (used to derive USBIPBusID if unset)
	USBIPBusID      string // Linux-style id advertised by our export server
	VID             string // hex, e.g. "0781" — from models.USBPassthroughDevice
	PID             string // hex, e.g. "55A9"
	ExportService   string // default 3240
	AllowUnlicensed bool   // unused client-side: the entitlement gate is on the agent
}
