package controller

import (
	"net/url"
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
}

// NewBackupWidget creates a new backup widget
func NewBackupWidget(usbClient *api.USBClient, hostEntry *widget.Entry, updateStatus func()) *BackupWidget {
	bw := &BackupWidget{
		usbClient:    usbClient,
		hostEntry:    hostEntry,
		snapshots:    make([]*models.SnapshotInfo, 0),
		currentFlash: nil,
		updateStatus: updateStatus,
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
	// In Fyne we use fyne.Do to update UI from goroutines
	fyne.Do(updateFunc)
}

// updateStatusAsync safely updates status from a goroutine
func (bw *BackupWidget) updateStatusAsync(status string) {
	bw.updateUIAsync(func() {
		bw.ui.StatusLabel.SetText(status)
	})
}

// openHardwarePromo opens the USBridge KVM hardware page, used by the
// Snapshots empty-state placeholder shown for non-USBridge agents.
func (bw *BackupWidget) openHardwarePromo() {
	const promoURL = "https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0"

	uri, err := url.Parse(promoURL)
	if err != nil {
		logrus.Errorf("failed to parse hardware promo URL %q: %v", promoURL, err)
		return
	}

	fyneApp := fyne.CurrentApp()
	if fyneApp == nil {
		logrus.Errorf("failed to open hardware promo URL: fyne app is nil")
		return
	}

	go func() {
		if err := fyneApp.OpenURL(uri); err != nil {
			logrus.Errorf("failed to open hardware promo URL %q: %v", promoURL, err)
		}
	}()
}
