//go:build !linux

package app

// fyneAppID is the agent's Fyne app ID (see appid_linux.go for Linux).
func fyneAppID() string { return "io.usbridge.agent" }
