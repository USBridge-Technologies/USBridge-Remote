package controller

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
)

// scheduleCombine debounces concurrent loader completions into a single
// combineDrives + refresh cycle. Multiple calls within the 80 ms window
// collapse into one. Must be called on the Fyne event-loop thread.
func (dw *DiskWidget) scheduleCombine() {
	if dw.pendingCombine.Swap(true) {
		return // already scheduled
	}
	time.AfterFunc(80*time.Millisecond, func() {
		fyne.Do(func() {
			dw.pendingCombine.Store(false)
			dw.combineDrives()
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
		for range ticker.C {
			dw.loadLocalDrives()
			dw.loadMountedDevices()
		}
	}()
}
