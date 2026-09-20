//go:build linux

package account

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func collectPlatformHardware() (cpu, gpu string, ramMB int) {
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(k) {
			case "model name", "Model", "Hardware":
				if cpu == "" {
					cpu = strings.TrimSpace(v)
				}
			}
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if rest, ok := strings.CutPrefix(line, "MemTotal:"); ok {
				fields := strings.Fields(rest)
				if len(fields) > 0 {
					if kb, err := strconv.Atoi(fields[0]); err == nil {
						ramMB = kb / 1024
					}
				}
				break
			}
		}
	}
	gpu = linuxGPU()
	return
}

// linuxGPU asks lspci (absent on Android and minimal images -- then the GPU
// is just left empty) for display-class devices.
func linuxGPU() string {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lspci", "-mm").Output()
	if err != nil {
		return ""
	}
	var gpus []string
	for _, line := range strings.Split(string(out), "\n") {
		f := splitQuoted(line)
		if len(f) < 4 {
			continue
		}
		class := strings.ToLower(f[1])
		if strings.Contains(class, "vga") || strings.Contains(class, "3d controller") || strings.Contains(class, "display controller") {
			gpus = append(gpus, f[2]+" "+f[3])
		}
	}
	if len(gpus) > 2 {
		gpus = gpus[:2]
	}
	return strings.Join(gpus, " + ")
}

// splitQuoted splits an `lspci -mm` line: `slot "class" "vendor" "device" ...`.
func splitQuoted(line string) []string {
	var out []string
	slot, rest, ok := strings.Cut(line, " ")
	if !ok {
		return nil
	}
	out = append(out, slot)
	for {
		i := strings.IndexByte(rest, '"')
		if i < 0 {
			break
		}
		j := strings.IndexByte(rest[i+1:], '"')
		if j < 0 {
			break
		}
		out = append(out, rest[i+1:i+1+j])
		rest = rest[i+1+j+1:]
	}
	return out
}
