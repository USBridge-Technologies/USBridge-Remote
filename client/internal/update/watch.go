package update

import (
	"context"
	"time"
)

// recheckInterval is how often a long-running process looks for a new
// release again; without it an update is only noticed at launch, and a
// machine that is never restarted stays on an old version indefinitely.
const recheckInterval = 24 * time.Hour

// Watch calls Check (right away when checkNow is set, then every 24h) and
// hands each newer manifest to onNewer, once per version: a release the
// user already saw and declined is not offered again until the next launch.
// onNewer runs on the watcher goroutine, so it must not block for long.
// Returns when ctx is cancelled; run it in its own goroutine.
func Watch(ctx context.Context, currentVersion string, checkNow bool, onNewer func(*Manifest)) {
	offered := ""
	check := func() {
		m := Check(ctx, currentVersion)
		if m == nil || m.Version == offered {
			return
		}
		offered = m.Version
		onNewer(m)
	}
	if checkNow {
		check()
	}
	t := time.NewTicker(recheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check()
		}
	}
}
