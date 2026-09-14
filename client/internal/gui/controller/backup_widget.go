package controller

import (
	"net/url"
	"sync"
	"sync/atomic"
	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// BackupWidget is a widget for displaying the snapshot list
type BackupWidget struct {
	onStorageInfoUpdate func(usedPct float64, available, total int64) // Callback for main window
	onSnapshotsLoaded   func(count int, snapshotMounted bool)
	window              fyne.Window
	ui                  *view.BackupWidgetUI

	// Data
	snapshots             []*models.SnapshotInfo
	sdSpaceInfo           *models.ISOSpaceInfo // SD card space information (iso/data/backup)
	currentFlash          *models.LocalDrive   // Current backup flash drive
	currentFlashConnected bool                 // Is backup flash connected (mtp:data)
	agentOS               string               // OS reported by the connected agent (empty/"usbridge" = real hardware)
	loadingCurrentFlash   atomic.Bool
	loadingSnapshots      atomic.Bool
	isMounting            atomic.Bool
	usbClient             *api.USBClient
	hostEntry             *widget.Entry
	updateStatus          func() // Callback for status update
	isClosing             atomic.Bool
	refreshStop           chan struct{}
	stopRefreshOnce       sync.Once

	firmwareBanner         *view.FirmwarePromoBanner
	firmwareChip           *view.FooterPromoChip
	firmwarePromoDismissed bool
}

// NewBackupWidget creates a new backup widget
func NewBackupWidget(usbClient *api.USBClient, hostEntry *widget.Entry, updateStatus func()) *BackupWidget {
	bw := &BackupWidget{
		usbClient:    usbClient,
		hostEntry:    hostEntry,
		snapshots:    make([]*models.SnapshotInfo, 0),
		currentFlash: nil,
		updateStatus: updateStatus,
		refreshStop:  make(chan struct{}),
	}

	bw.createInterface()
	bw.loadCurrentFlash()
	bw.loadSnapshots()
	bw.startPeriodicRefresh()

	return bw
}

func (bw *BackupWidget) Close() {
	bw.isClosing.Store(true)
}

// Shutdown stops the snapshot poller for real app exit. Close() only pauses
// it across a disconnect/reconnect cycle (see startPeriodicRefresh).
func (bw *BackupWidget) Shutdown() {
	bw.isClosing.Store(true)
	bw.stopRefreshOnce.Do(func() {
		if bw.refreshStop != nil {
			close(bw.refreshStop)
		}
	})
}

// SetWindow sets the window for dialogs
func (bw *BackupWidget) SetWindow(window fyne.Window) {
	bw.window = window
}

// UpdateClient updates the USB client
func (bw *BackupWidget) UpdateClient(usbClient *api.USBClient) {
	bw.usbClient = usbClient
	if usbClient != nil {
		bw.isClosing.Store(false)
	}
	if usbClient == nil {
		bw.sdSpaceInfo = nil
		bw.updateSDStorageInfo()
	}
	// Update data when client changes
	bw.loadCurrentFlash()
	bw.loadSnapshots()
}

// UpdateHostEntry updates the reference to the host entry field
func (bw *BackupWidget) UpdateHostEntry(hostEntry *widget.Entry) {
	bw.hostEntry = hostEntry
}

// GetContainer returns the widget container
func (bw *BackupWidget) GetContainer() *fyne.Container {
	return bw.ui.Container
}

// SetScriptFooter injects the shared script-run chip into the Snapshots
// footer so script status is visible while looking at snapshots.
func (bw *BackupWidget) SetScriptFooter(chip *view.ScriptFooterStatus) {
	if bw == nil || bw.ui == nil {
		return
	}
	bw.ui.SetScriptFooter(chip)
}

// SetConnectingHint injects the shared gadget-connect spinner so Devices'
// mount/unmount is visible from Snapshots too.
func (bw *BackupWidget) SetConnectingHint(hint *view.DeviceDashboardBusySpinner) {
	if bw == nil || bw.ui == nil {
		return
	}
	bw.ui.SetConnectingHint(hint)
}

// Refresh updates the widget
func (bw *BackupWidget) Refresh() {
	bw.loadCurrentFlash()
	bw.loadSnapshots()
}

func (bw *BackupWidget) GetISODirectory() string {
	if bw.sdSpaceInfo == nil {
		return ""
	}
	return bw.sdSpaceInfo.ISODirectory
}

// updateUIAsync safely updates UI from a goroutine
func (bw *BackupWidget) updateUIAsync(updateFunc func()) {
	if bw.isClosing.Load() {
		return
	}
	fyne.Do(updateFunc)
}

// updateStatusAsync safely updates status from a goroutine
func (bw *BackupWidget) updateStatusAsync(status string) {
	bw.updateUIAsync(func() {
		bw.ui.StatusLabel.SetText(status)
	})
}

const snapshotsFirmwarePromoDismissedPrefKey = "snapshots.firmware_promo.dismissed"

func (bw *BackupWidget) openFirmwarePromo() {
	uri, err := url.Parse(view.FirmwarePromoURL)
	if err != nil {
		logrus.Errorf("failed to parse firmware promo URL %q: %v", view.FirmwarePromoURL, err)
		return
	}
	fyneApp := fyne.CurrentApp()
	if fyneApp == nil {
		logrus.Errorf("failed to open firmware promo URL: fyne app is nil")
		return
	}
	go func() {
		if err := fyneApp.OpenURL(uri); err != nil {
			logrus.Errorf("failed to open firmware promo URL %q: %v", view.FirmwarePromoURL, err)
		}
	}()
}

func (bw *BackupWidget) firmwarePromoDismissedPref() bool {
	app := fyne.CurrentApp()
	if app == nil {
		return false
	}
	return app.Preferences().BoolWithFallback(snapshotsFirmwarePromoDismissedPrefKey, false)
}

func (bw *BackupWidget) setFirmwarePromoDismissed(on bool) {
	bw.firmwarePromoDismissed = on
	if app := fyne.CurrentApp(); app != nil {
		app.Preferences().SetBool(snapshotsFirmwarePromoDismissedPrefKey, on)
	}
}

func (bw *BackupWidget) dismissFirmwarePromo() {
	bw.setFirmwarePromoDismissed(true)
	if bw.ui != nil {
		bw.ui.Refresh()
	}
}

func (bw *BackupWidget) restoreFirmwarePromo() {
	bw.setFirmwarePromoDismissed(false)
	if bw.ui != nil {
		bw.ui.Refresh()
	}
}

func (bw *BackupWidget) syncFirmwareChip() {
	softwareAgent := bw.usbClient != nil && !isUSBridgeAgentOS(bw.agentOS)
	if bw.firmwareChip != nil {
		bw.firmwareChip.SetActive(softwareAgent && bw.firmwarePromoDismissed)
	}
}
