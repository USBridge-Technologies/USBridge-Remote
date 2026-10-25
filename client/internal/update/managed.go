package update

import "context"

// packageManagedApply, when set by a platform file, updates an install that
// a system package manager owns (the Linux .deb or Arch package, see
// apply_linux.go) instead of downloading and swapping the binary.
// handled=false means this install isn't package-managed and the normal
// download path runs.
var packageManagedApply func(ctx context.Context, version string) (handled bool, err error)
