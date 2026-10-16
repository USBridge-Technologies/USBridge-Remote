//go:build !darwin

package monitors

// ActiveDisplayIDs is macOS-only.
func ActiveDisplayIDs() []uint32 { return nil }
