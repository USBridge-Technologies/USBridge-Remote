//go:build darwin && !ios

package account

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func collectPlatformHardware() (cpu, gpu string, ramMB int) {
	cpu, _ = unix.Sysctl("machdep.cpu.brand_string")
	if cpu == "" {
		cpu, _ = unix.Sysctl("hw.model")
	}
	if mem, err := unix.SysctlUint64("hw.memsize"); err == nil {
		ramMB = int(mem / (1024 * 1024))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "system_profiler", "SPDisplaysDataType").Output(); err == nil {
		var gpus []string
		for _, line := range strings.Split(string(out), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Chipset Model:"); ok {
				gpus = append(gpus, strings.TrimSpace(rest))
			}
		}
		if len(gpus) > 2 {
			gpus = gpus[:2]
		}
		gpu = strings.Join(gpus, " + ")
	}
	return
}
