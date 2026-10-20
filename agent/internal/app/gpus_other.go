//go:build !linux

package app

import "usbridge_agent/internal/config"

// hostGPUsFallback is Linux-only: see gpus_linux.go.
func hostGPUsFallback() []config.GPUInfo { return nil }
