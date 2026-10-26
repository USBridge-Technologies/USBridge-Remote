//go:build !linux || android

package kwinplanes

// KWin's single-plane tune is Linux desktop only (see kwinplanes_linux.go).

func Status() (applicable, done bool) { return false, false }

func Active() bool { return false }

func Apply() error { return nil }
