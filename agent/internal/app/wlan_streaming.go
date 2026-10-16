package app

import (
	"context"
	"log"
	"runtime"
	"time"

	"usbridge_agent/internal/netutil"
)

// wlanStreamingPollInterval is how often wlanStreamingWatchdog checks the
// session state; it also picks up a Wi-Fi interface that (re)connected
// mid-session.
const wlanStreamingPollInterval = 2 * time.Second

// wlanStreamingWatchdog keeps the host's Wi-Fi in media streaming mode with
// background scans off while a session is active and gives it back to
// Windows once the session ends -- see netutil/wlan_windows.go for the
// stutters this removes. Windows only; elsewhere it returns at once.
func (a *App) wlanStreamingWatchdog(ctx context.Context) {
	if runtime.GOOS != "windows" {
		return
	}
	ticker := time.NewTicker(wlanStreamingPollInterval)
	defer ticker.Stop()

	on := false
	lastErr := ""
	set := func(next bool) {
		if err := netutil.SetWLANStreamingMode(next); err != nil {
			if msg := err.Error(); msg != lastErr {
				log.Printf("[app] Wi-Fi streaming mode (on=%v): %v", next, err)
				lastErr = msg
			}
		} else {
			lastErr = ""
		}
		if next != on {
			log.Printf("[app] Wi-Fi streaming mode (media streaming, no background scans): %v", next)
			on = next
		}
	}
	for {
		select {
		case <-ctx.Done():
			if on {
				set(false)
			}
			return
		case <-ticker.C:
			a.streamMu.Lock()
			stream := a.stream
			a.streamMu.Unlock()
			active := stream != nil && stream.SessionActive()
			if active || on {
				set(active)
			}
		}
	}
}
