//go:build linux

package app

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/display"
)

var drmCardRe = regexp.MustCompile(`^card\d+$`)

// hostGPUsFallback lists the host's GPUs from sysfs for when the streamer's
// monitor enumeration reports no adapters (the Linux streamer doesn't, so
// GPUs() came back empty). The UI treats an empty list as "unknown" and
// shows the NVIDIA settings under an "NVIDIA" heading -- on an Intel-only
// laptop that read as if the agent thought it had an NVIDIA card.
func hostGPUsFallback() []config.GPUInfo {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return nil
	}
	monitors := map[string][]string{}
	for _, c := range display.Connectors() {
		if m := drmConnectorRe.FindStringSubmatch(c.Name); m != nil && !strings.HasPrefix(m[2], "Virtual-") {
			monitors[m[1]] = append(monitors[m[1]], m[2])
		}
	}
	var gpus []config.GPUInfo
	for _, e := range entries {
		card := e.Name()
		if !drmCardRe.MatchString(card) {
			continue
		}
		dev := "/sys/class/drm/" + card + "/device/"
		// Platform devices (vkms, simpledrm) have no PCI vendor: not a GPU
		// the encoder settings could apply to.
		b, err := os.ReadFile(dev + "vendor")
		if err != nil {
			continue
		}
		id, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(string(b)), "0x"), 16, 32)
		if err != nil {
			continue
		}
		vendor := gpuVendorName(uint32(id))
		gpus = append(gpus, config.GPUInfo{
			Name:     pciGPUName(dev, vendor, card),
			Vendor:   vendor,
			Monitors: monitors[card],
		})
	}
	return gpus
}

// pciGPUName is the card's marketing name from lspci ("UHD Graphics" out of
// "Alder Lake-P GT1 [UHD Graphics]"), or "<vendor> (cardN)" without lspci.
func pciGPUName(dev, vendor, card string) string {
	fallback := card
	if vendor != "" {
		fallback = strings.ToUpper(vendor[:1]) + vendor[1:] + " (" + card + ")"
	}
	b, err := os.ReadFile(dev + "uevent")
	if err != nil {
		return fallback
	}
	slot := ""
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "PCI_SLOT_NAME="); ok {
			slot = strings.TrimSpace(v)
		}
	}
	if slot == "" {
		return fallback
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lspci", "-mm", "-s", slot).Output()
	if err != nil {
		return fallback
	}
	// `slot "class" "vendor" "device" ...`: quoted fields at odd indices.
	f := strings.Split(string(out), `"`)
	if len(f) < 8 {
		return fallback
	}
	vendorName, device := f[3], f[5]
	if i := strings.IndexByte(device, '['); i >= 0 {
		if j := strings.LastIndexByte(device, ']'); j > i {
			if name := strings.TrimSpace(device[i+1 : j]); name != "" {
				device = name
			}
		}
	}
	if v := strings.Fields(vendorName); len(v) > 0 {
		return v[0] + " " + device
	}
	return device
}
