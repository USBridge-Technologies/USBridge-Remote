//go:build !windows

package usbpass

import "os/exec"

func hideBrokerWindow(cmd *exec.Cmd) {}
