package app

import (
	"fmt"
	"log"
	"runtime"
	"slices"
	"strings"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/streamhost"
)

// nvencPrefKeys are the NVIDIA encoder preferences as a streamer of kind
// reads them: Sunshine's own keys and values, which RustShine takes too.
// Sunshine's nvenc_latency_over_power only knows max performance on or
// off; RustShine also gets the full power mode (nvidia_power_mode).
func (a *App) nvencPrefKeys(kind string) map[string]string {
	twoPass, maxPerf := "disabled", "disabled"
	if a.cfg.NvencTwoPassOK() {
		twoPass = "quarter_res"
	}
	if a.cfg.NvidiaMaxPerformanceOK() {
		maxPerf = "enabled"
	}
	keys := map[string]string{
		"nvenc_twopass":            twoPass,
		"nvenc_latency_over_power": maxPerf,
	}
	if kind == "rustshine" {
		keys["nvidia_power_mode"] = a.cfg.NvidiaPowerModeValue()
	}
	return keys
}

// applyNvencPrefs writes the NVIDIA encoder preferences into b's config,
// reporting whether anything changed (a running streamer then needs a
// restart to pick it up).
func (a *App) applyNvencPrefs(b streamhost.Backend, kind string) (changed bool) {
	// These are Sunshine's keys. Punktfunk's settings store rejects them
	// ("unknown setting"), which cost two failed punktfunk-host runs and
	// four log lines on every watchdog tick.
	if b == nil || kind == "punktfunk" {
		return false
	}
	for key, value := range a.nvencPrefKeys(kind) {
		if b.ConfigKey(key) == value {
			continue
		}
		if err := b.SetConfigKey(key, value); err != nil {
			log.Printf("[app] writing %s = %s: %v", key, value, err)
			continue
		}
		changed = true
	}
	return changed
}

// applySunshineEncoder writes sunshineEncoderPin into Sunshine's config,
// reporting whether it changed. Only a value this sets is ever removed, so
// a hand-picked encoder on a host without the pin stays.
func (a *App) applySunshineEncoder(b streamhost.Backend, kind string) (changed bool) {
	if b == nil || kind != "sunshine" || runtime.GOOS != "linux" {
		return false
	}
	want, have := sunshineEncoderPin(), b.ConfigKey("encoder")
	if want == have || (want == "" && have != "nvenc") {
		return false
	}
	if err := b.SetConfigKey("encoder", want); err != nil {
		log.Printf("[app] writing encoder = %s: %v", want, err)
		return false
	}
	log.Printf("[app] sunshine: encoder %q -> %q", have, want)
	return true
}

func (a *App) NvencTwoPassEnabled() bool { return a.cfg.NvencTwoPassOK() }

func (a *App) NvidiaPowerMode() string { return a.cfg.NvidiaPowerModeValue() }

func (a *App) SetNvencTwoPass(enabled bool) error {
	next := a.cfg
	next.NvencTwoPass = &enabled
	return a.saveNvencPrefs(next)
}

func (a *App) SetNvidiaPowerMode(mode string) error {
	if !slices.Contains(config.NvidiaPowerModes, mode) {
		return fmt.Errorf("unknown NVIDIA power mode %q", mode)
	}
	next := a.cfg
	next.NvidiaPowerMode = mode
	return a.saveNvencPrefs(next)
}

// saveNvencPrefs persists the preferences and restarts the running
// streamer when its config changed; the other streamer gets them on its
// next start (startSunshineNow).
func (a *App) saveNvencPrefs(next config.Config) error {
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.applyNvencPrefs(a.stream, a.streamKind) && a.stream.Running() {
		log.Printf("[app] NVIDIA encoder preferences changed, restarting %s", a.streamKind)
		return a.RestartSunshine()
	}
	return nil
}

// GPUs lists the host's GPUs with their monitors, from the streamer's own
// monitor enumeration (usbridge-streamer --list-capture-devices, the one
// that reports each monitor's adapter), so the encoder settings can say
// which card they apply to. When that binary isn't staged or reports no
// adapters (the Linux streamer), falls back to hostGPUsFallback.
func (a *App) GPUs() []config.GPUInfo {
	if !a.rustshineStaged() {
		return hostGPUsFallback()
	}
	devices := streamhost.NewRustshine(a.exeDir, a.cfg.StateDir, a.logPath).ListCaptureDevices()

	// The monitor the running streamer captures, by GDI name.
	a.streamMu.Lock()
	stream := a.stream
	a.streamMu.Unlock()
	streaming := ""
	if stream != nil && stream.Running() {
		current := stream.OutputName()
		for _, d := range stream.ListCaptureDevices() {
			if d.OutputName == current {
				streaming = d.GDIName
			}
		}
	}

	var gpus []config.GPUInfo
	index := map[string]int{}
	for _, d := range devices {
		if d.Adapter == "" {
			continue
		}
		i, ok := index[d.Adapter]
		if !ok {
			i = len(gpus)
			index[d.Adapter] = i
			gpus = append(gpus, config.GPUInfo{Name: d.Adapter, Vendor: gpuVendorName(d.VendorID)})
		}
		monitor := strings.TrimPrefix(d.GDIName, `\\.\`)
		gpus[i].Monitors = append(gpus[i].Monitors, monitor)
		if d.GDIName != "" && d.GDIName == streaming {
			gpus[i].Streaming = true
		}
	}
	if len(gpus) == 0 {
		return hostGPUsFallback()
	}
	return gpus
}

func gpuVendorName(id uint32) string {
	switch id {
	case 0x10DE:
		return "nvidia"
	case 0x1002, 0x1022:
		return "amd"
	case 0x8086:
		return "intel"
	}
	return ""
}
