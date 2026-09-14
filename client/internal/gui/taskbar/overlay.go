package taskbar

import "fyne.io/fyne/v2"

// ScriptState is the taskbar overlay that mirrors the script footer chip.
type ScriptState int

const (
	ScriptIdle ScriptState = iota
	ScriptRunning
	ScriptDone
	ScriptError
)

// SetScriptState paints a status dot on the Windows taskbar button (blue /
// green / red) and clears it when idle. Other OSes are a no-op: macOS Dock
// badges need AppKit, Linux taskbars have no common overlay API.
func SetScriptState(win fyne.Window, state ScriptState) {
	setScriptState(win, state)
}

// ProbeCOM creates ITaskbarList3 once the HWND exists so startup logs
// show whether the overlay API is available (not only when a script runs).
func ProbeCOM(win fyne.Window) {
	setScriptState(win, ScriptIdle)
}
