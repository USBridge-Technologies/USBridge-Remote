package update

import "context"

// packageManagedApply, when set by a platform file, updates an install that
// a system package manager owns (the Linux .deb or Arch package, see
// apply_linux.go) instead of downloading and swapping the binary.
// handled=false means this install isn't package-managed and the normal
// download path runs.
var packageManagedApply func(ctx context.Context, version, tag string) (handled bool, err error)

// installIsPackageManaged reports whether a system package manager owns this
// install (nil off Linux). Such an install can't be re-flashed with the same
// version, so the test channel doesn't offer a same-version rebuild for it.
var installIsPackageManaged func() bool
