//go:build !windows && !linux && !darwin && !android

package update

import "context"

// apply is the fallback for any GOOS this package doesn't have a real
// updater for (currently: iOS, which CheckAndApply already short-circuits
// before ever reaching here — this only exists so the package still
// compiles for whatever other GOOS/GOARCH combination Go supports).
func apply(ctx context.Context, artifactPath, version string) error {
	return unsupportedPlatformApply(ctx, artifactPath, version)
}
