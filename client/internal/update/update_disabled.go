//go:build noselfupdate

// Self-update, fully compiled out. This is the counterpart to update.go
// (see its doc comment) for builds that must never fetch or install
// executable code on their own — currently the Android "market" flavor
// (Play Store, F-Droid, ...). Check always reports "nothing to do" and
// DownloadAndApply/CheckAndApply are no-ops, so every call site (desktop
// cmd/main.go, cmd/android/main.go) keeps compiling unchanged regardless of
// this tag; they just never see an update.
package update

import (
	"context"
	"fmt"
)

// Check always reports no update is available — see the package doc
// comment above for why.
func Check(ctx context.Context, currentVersion string) *Manifest {
	return nil
}

// DownloadAndApply is never reachable in practice (Check never returns a
// non-nil Manifest for a caller to pass in here), but is kept with the same
// signature as the enabled build so call sites don't need build tags of
// their own.
func DownloadAndApply(ctx context.Context, manifest *Manifest, onProgress ProgressFunc) error {
	return fmt.Errorf("self-update disabled in this build")
}

// CheckAndApply mirrors the enabled build's "check, then apply, swallowing
// every failure" contract — here that's simply a no-op.
func CheckAndApply(ctx context.Context, currentVersion string) {}
