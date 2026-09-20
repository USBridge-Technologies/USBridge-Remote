//go:build !linux && !windows && !(darwin && !ios)

package account

// iOS and any other platform: no unprivileged way to read CPU/GPU/RAM
// details here, so only the OS/arch/core-count fields CollectDeviceInfo
// fills in itself are reported.
func collectPlatformHardware() (cpu, gpu string, ramMB int) { return "", "", 0 }
