package account

import (
	"runtime"
	"strings"
	"sync"
)

// DeviceInfo is the hardware summary sent once, in the body of the
// device-code login start request (POST /v1/account/login/start), so the
// account dashboard can show what kind of machine a login came from. It is
// deliberately coarse -- model names and total RAM only: no serial numbers,
// MAC addresses, disk ids or installed software. Disclosed in the privacy
// policy. Any field the platform can't determine is simply left empty; a
// failure to collect never blocks the login.
type DeviceInfo struct {
	OS    string `json:"os"`
	Arch  string `json:"arch"`
	CPU   string `json:"cpu,omitempty"`
	Cores int    `json:"cores"`
	GPU   string `json:"gpu,omitempty"`
	RAMMB int    `json:"ram_mb,omitempty"`
}

var (
	deviceInfoOnce sync.Once
	deviceInfo     DeviceInfo
)

// CollectDeviceInfo gathers (once per process, then cached) the hardware
// summary. Platform-specific pieces live in hwinfo_<os>.go.
func CollectDeviceInfo() DeviceInfo {
	deviceInfoOnce.Do(func() {
		deviceInfo = DeviceInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, Cores: runtime.NumCPU()}
		deviceInfo.CPU, deviceInfo.GPU, deviceInfo.RAMMB = collectPlatformHardware()
		deviceInfo.CPU = cleanField(deviceInfo.CPU)
		deviceInfo.GPU = cleanField(deviceInfo.GPU)
	})
	return deviceInfo
}

func cleanField(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
