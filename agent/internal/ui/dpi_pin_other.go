//go:build !linux || android || wayland

package ui

func (w *Window) pinLinuxDPIScale() {}
