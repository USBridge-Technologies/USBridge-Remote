package controller

import (
	"fmt"
	"strings"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"
)

// loadCurrentFlash loads the current backup flash drive
func (bw *BackupWidget) loadCurrentFlash() {
	if bw.isClosing.Load() {
		return
	}
	if !bw.loadingCurrentFlash.CompareAndSwap(false, true) {
		logrus.Debug("loadCurrentFlash already in flight, skipping overlapping refresh")
		return
	}
	go func() {
		defer bw.loadingCurrentFlash.Store(false)
		// Capture usbClient once — UpdateClient(nil) can race with this goroutine.
		client := bw.usbClient
		if bw.isClosing.Load() || client == nil {
			logrus.Debug("USB client not initialized or closing, skipping loading current flash drive")
			return
		}

		logrus.Debug("📱 Loading current backup flash drive...")

		localDrives, err := client.GetLocalDrives()
		if err != nil {
			logrus.Errorf(i18n.Current.ErrorLoadingLocalDevices, err)
			return
		}

		for _, drive := range localDrives.Drives {
			if drive.SourceType == "mtp" && drive.Name == "data" {
				bw.currentFlash = &drive
				logrus.Infof("✅ Found current backup flash drive: %s", drive.Name)
				break
			}
		}

		bw.currentFlashConnected = false
		deviceInfo, err := client.GetDeviceInfo()
		if err == nil {
			bw.agentOS = deviceInfo.AgentOS
			for _, device := range deviceInfo.Devices {
				if device.Status == "connected" &&
					device.Type == "mtp" &&
					strings.Contains(device.Name, "data") &&
					!strings.Contains(device.ProductName, "snapshot") {
					bw.currentFlashConnected = true
					logrus.Infof("✅ Backup flash drive connected: %s", device.Name)
					break
				}
			}
		}

		bw.loadISOSpace()
		bw.updateUIAsync(func() {
			bw.ui.SnapshotsList.Refresh()
		})
	}()
}

// loadISOSpace loads information about space on the SD card
func (bw *BackupWidget) loadISOSpace() {
	client := bw.usbClient
	if client == nil {
		return
	}
	spaceInfo, err := client.GetISOSpace()
	if err != nil {
		logrus.Debugf("SD card space information is unavailable: %v", err)
		bw.updateUIAsync(func() {
			bw.sdSpaceInfo = nil
			bw.updateSDStorageInfo()
		})
		return
	}
	bw.updateUIAsync(func() {
		bw.sdSpaceInfo = spaceInfo
		bw.updateSDStorageInfo()
	})
}

// updateSDStorageInfo updates the progress bar in main window via callback
func (bw *BackupWidget) updateSDStorageInfo() {
	if bw.sdSpaceInfo == nil || bw.sdSpaceInfo.TotalSpace <= 0 {
		if bw.onStorageInfoUpdate != nil {
			bw.onStorageInfoUpdate(0, 0, 0)
		}
		return
	}
	usedPct := bw.sdSpaceInfo.UsedPercent
	total := bw.sdSpaceInfo.TotalSpace
	available := bw.sdSpaceInfo.AvailableSpace
	if bw.onStorageInfoUpdate != nil {
		bw.onStorageInfoUpdate(usedPct/100, available, total)
	}
}

// SetOnStorageInfoUpdate sets the callback for updating progress bar in main window
func (bw *BackupWidget) SetOnStorageInfoUpdate(fn func(usedPct float64, available, total int64)) {
	bw.onStorageInfoUpdate = fn
}

// loadSnapshots loads the list of snapshots
func (bw *BackupWidget) loadSnapshots() {
	if bw.isClosing.Load() {
		return
	}
	if !bw.loadingSnapshots.CompareAndSwap(false, true) {
		logrus.Debug("loadSnapshots already in flight, skipping overlapping refresh")
		return
	}
	go func() {
		defer bw.loadingSnapshots.Store(false)
		client := bw.usbClient
		if bw.isClosing.Load() || client == nil {
			logrus.Debug("USB client not initialized or closing, skipping snapshots loading")
			bw.updateUIAsync(func() {
				if !bw.isClosing.Load() {
					bw.ui.StatusLabel.SetText(i18n.Current.WaitingConnection)
				}
			})
			return
		}

		bw.updateStatusAsync(i18n.Current.LoadingSnapshots)
		logrus.Debug("📦 Loading snapshot list...")

		snapshotsResp, err := client.GetSnapshots()
		if err != nil {
			if strings.Contains(err.Error(), "/mnt/sdcard/backup/") && strings.Contains(err.Error(), "no such file or directory") {
				logrus.Debugf("Snapshots directory is absent on device: %v", err)
			} else {
				logrus.Errorf("Error loading snapshots: %v", err)
			}
			bw.updateUIAsync(func() {
				bw.ui.StatusLabel.SetText(i18n.Current.ErrorLoadingSnapshots)
			})
			return
		}

		bw.snapshots = make([]*models.SnapshotInfo, len(snapshotsResp.Snapshots))
		for i := range snapshotsResp.Snapshots {
			bw.snapshots[i] = &snapshotsResp.Snapshots[i]
		}

		bw.updateUIAsync(func() {
			bw.ui.SnapshotsList.Refresh()
			bw.ui.StatusLabel.SetText(fmt.Sprintf(i18n.Current.LoadedSnapshots, len(bw.snapshots)))
		})

		logrus.Infof("✅ Loaded %d snapshots", len(bw.snapshots))
	}()
}

// startPeriodicRefresh starts periodic snapshot refresh
func (bw *BackupWidget) startPeriodicRefresh() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if bw.isClosing.Load() {
					return
				}
				bw.Refresh()
			}
		}
	}()
}
