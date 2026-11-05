//go:build !linux || android

package netutil

// Snaps exist only on Linux desktops (see snap_plugs_linux.go).

func InSnap() bool { return false }

func SnapMissingPlugs() []string { return nil }

func SnapConnectCommand(plugs []string) string { return "" }
