package streamhost

// NewDefault constructs the Backend the agent boots with. Always Sunshine,
// unconditionally — boot must never depend on a network round-trip
// succeeding, so RustShine is never the default even for an already-
// entitled supporter. A supporter's saved preference (config.Config's
// PreferredBackend) is applied afterwards, once, by App.applyPreferredBackend
// during startup, and from then on by App.SetStreamBackend whenever the
// user switches — both construct a rustshineBackend directly via
// NewRustshine when needed. This file used to be a //go:build !rustshine
// vs. rustshine compile-time seam (NewDefault picking one or the other);
// that's gone now that the entitlement check happens at runtime instead of
// at build time (see agent/internal/entitlement) — every build always
// contains both backends, and which one is *running* is just state.
func NewDefault(exeDir, stateDir, logPath string) Backend {
	return NewSunshine(exeDir, stateDir, logPath)
}
