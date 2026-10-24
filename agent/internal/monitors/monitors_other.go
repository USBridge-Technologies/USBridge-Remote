//go:build !windows

package monitors

// List is only implemented on Windows; elsewhere the benchmark keeps each
// backend's own monitor and the player's default placement.
func List() ([]Monitor, error) { return nil, ErrUnsupported }

// LogicalOrigin is only implemented on Windows.
func LogicalOrigin(id string) (x, y int, ok bool) { return 0, 0, false }

// ProcessMonitor is only implemented on Windows.
func ProcessMonitor(pid int) (string, bool) { return "", false }

// MoveProcessWindows is only implemented on Windows.
func MoveProcessWindows(pid int, m Monitor) error { return ErrUnsupported }

// GDINames is Windows-only.
func GDINames() []string { return nil }

// WithPhysicalDPI is Windows-only; elsewhere it just runs f.
func WithPhysicalDPI(f func()) { f() }

// HelperArg and RunHelper exist for the Windows Session 0 workaround only.
const HelperArg = "monitors-helper"

func RunHelper() int { return 1 }
