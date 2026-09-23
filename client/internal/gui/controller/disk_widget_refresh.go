package controller

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"

	"usbridge-client/internal/gui/view"
)

// scheduleCombine debounces concurrent loader completions into a single
// combineDrives + refresh cycle. Multiple calls within the 80 ms window
// collapse into one. Must be called on the Fyne event-loop thread.
//
// Deferred entirely while the video overlay is the visible nav destination
// (Control tab, actively streaming): combineDrives' list rebuild +
// refreshDashboard() cascade measured 150-220ms of main-thread time on real
// hardware -- even with zero drives, since the cost is in the Devices
// dashboard's widget tree (footers/buttons/list) invalidating, not the
// drive count -- and every one of those ms blocks AppKit's run loop, which
// stalls the Metal CADisplayLink tied to it (confirmed live: this ticket's
// benchmark caught a stall logged by [Metal]'s own "AppKit/DisplayLink
// stalled" profiler at the exact same timestamp as this widget's periodic
// 10s refresh, repeating every 10s for the whole run). The Devices tab
// isn't even visible then, so skipping the rebuild is free: pendingCombine
// still clears so the next scheduleCombine call (from the next periodic
// loader tick, at most 10s later) tries again, and it actually runs the
// moment the user leaves Control.
func (dw *DiskWidget) scheduleCombine() {
	if dw.pendingCombine.Swap(true) {
		return // already scheduled
	}
	time.AfterFunc(80*time.Millisecond, func() {
		if !view.NavVideoHidden() {
			dw.pendingCombine.Store(false)
			return
		}
		fyne.Do(func() {
			// pendingCombine only clears once combineDrives itself has
			// finished, not merely once this closure started -- clearing it
			// first (as this used to) let a second scheduleCombine call
			// land while combineDrives (150-220ms on real hardware per
			// this func's own doc comment) was still running, queuing a
			// second, overlapping combineDrives whose steps interleaved
			// with the first's. Confirmed live: two locally-driven
			// dw.allDrives writes with no agent round-trip to
			// self-correct them (a pen tablet toggle's IsMounted, see
			// disk_widget_pen.go) raced exactly this way -- one
			// combineDrives' fresh rebuild was immediately clobbered by
			// the other's still-in-flight one using pre-click data,
			// undoing the toggle within about a second, every time.
			dw.combineDrives()
			dw.pendingCombine.Store(false)
		})
	})
}

func (dw *DiskWidget) requestDevicesRefresh() {
	if dw == nil || dw.devicesList == nil {
		return
	}

	signature := dw.computeDrivesSignature()
	if signature == dw.lastDrivesTraceSig {
		return
	}

	if !dw.devicesRefreshPending.CompareAndSwap(false, true) {
		return
	}

	delay := dw.nextDevicesRefreshDelay()
	runRefresh := func() {
		fyne.Do(func() {
			defer dw.devicesRefreshPending.Store(false)
			if dw.devicesList != nil {
				currentSig := dw.computeDrivesSignature()
				if currentSig == dw.lastDrivesTraceSig && !dw.lastDevicesRefresh.IsZero() {
					return
				}
				dw.lastDrivesTraceSig = currentSig
				dw.markDevicesRefresh()
				dw.devicesList.Refresh()
				dw.refreshDashboard()
			}
		})
	}
	if delay <= 0 {
		runRefresh()
		return
	}
	time.AfterFunc(delay, runRefresh)
}

func (dw *DiskWidget) computeDrivesSignature() string {
	if dw == nil {
		return ""
	}
	drives := dw.allDrives
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("total=%d|api=%d|local=%d|user=%d|video=%d|mounted=%d|os=%s|op=%v|mnt=%v",
		len(drives), len(dw.localDrives), len(dw.localFiles), len(dw.userImages), len(dw.videoDevices),
		len(dw.mountedDevices), dw.agentOS, dw.userOperationInFlight.Load(), dw.apiMountInProgress.Load()))

	for i := range drives {
		drive := drives[i]
		builder.WriteString(fmt.Sprintf("|%d:%s:%t:%t:%t:%t:%s:%s:%s",
			i, drive.Source, drive.IsMounted, drive.IsMounting, drive.IsUploading, drive.ReadOnly,
			drive.Name, drive.RNDISMode, drive.MouseType))
		if drive.IsUploading {
			builder.WriteString(fmt.Sprintf(":up%.0f", drive.UploadProgress/2.0))
		}
		if drive.IsVideo && drive.VideoDevice != nil {
			builder.WriteString(fmt.Sprintf(":vc%t", drive.VideoDevice.Connected))
		}
	}
	return builder.String()
}

func (dw *DiskWidget) nextDevicesRefreshDelay() time.Duration {
	const mobileRefreshInterval = 120 * time.Millisecond

	if !fyne.CurrentDevice().IsMobile() {
		return 0
	}

	dw.refreshMu.Lock()
	defer dw.refreshMu.Unlock()

	if dw.lastDevicesRefresh.IsZero() {
		return 0
	}
	elapsed := time.Since(dw.lastDevicesRefresh)
	if elapsed >= mobileRefreshInterval {
		return 0
	}
	return mobileRefreshInterval - elapsed
}

func (dw *DiskWidget) markDevicesRefresh() {
	dw.refreshMu.Lock()
	dw.lastDevicesRefresh = time.Now()
	dw.refreshMu.Unlock()
}

// startPeriodicRefresh polls only the API sources (mounted devices + local drives).
// Gamepad, video, and local file scanning happen on explicit Refresh() calls only.
func (dw *DiskWidget) startPeriodicRefresh() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-dw.refreshStop:
				return
			case <-ticker.C:
				if dw.isClosing.Load() {
					continue
				}
				dw.loadLocalDrives()
				dw.loadMountedDevices()
				dw.loadUSBPassthroughDevices()
			}
		}
	}()
}

func (dw *DiskWidget) Shutdown() {
	dw.isClosing.Store(true)
	dw.stopRefreshOnce.Do(func() {
		if dw.refreshStop != nil {
			close(dw.refreshStop)
		}
	})
}
