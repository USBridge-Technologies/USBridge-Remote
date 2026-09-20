//go:build windows

package app

// No signals on Windows: the running GUI keeps its tray icon, which reopens
// the window, so a duplicate launch just exits.
func watchRaiseRequests()   {}
func raiseOtherGUI(pid int) {}
