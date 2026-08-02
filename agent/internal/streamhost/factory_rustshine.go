//go:build rustshine

package streamhost

// NewDefault constructs this build's Backend. This file (built only with
// -tags rustshine) swaps the sibling !rustshine factory_default.go's
// NewSunshine for NewRustshine — app.go and everything else calling
// NewDefault needs no changes either way.
func NewDefault(exeDir, stateDir, logPath string) Backend {
	return NewRustshine(exeDir, stateDir, logPath)
}
