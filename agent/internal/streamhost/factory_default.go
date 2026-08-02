//go:build !rustshine

package streamhost

// NewDefault constructs this build's Backend. Plain builds (no build tags)
// always get Sunshine. A sibling build with a "rustshine" build tag and its
// own factory_rustshine.go (providing a NewDefault under a complementary
// //go:build rustshine tag) can swap this out for a different backend
// without touching app.go or anything else that calls NewDefault — that's
// the entire seam a second backend needs.
func NewDefault(exeDir, stateDir, logPath string) Backend {
	return NewSunshine(exeDir, stateDir, logPath)
}
