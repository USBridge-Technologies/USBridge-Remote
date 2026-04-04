package controller

import (
	"fmt"
	"strings"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"
)

// loadCurrentFlash загружает актуальную бэкап флешку
func (bw *BackupWidget) loadCurrentFlash() {
	if !bw.loadingCurrentFlash.CompareAndSwap(false, true) {
		logrus.Debug("loadCurrentFlash already in flight, skipping overlapping refresh")
		return
	}
	go func() {
		defer bw.loadingCurrentFlash.Store(false)
		if bw.usbClient == nil {
			logrus.Debug("USB клиент не инициализирован, пропускаем загрузку актуальной флешки")
			return
		}

		logrus.Debug("📱 Загрузка актуальной бэкап флешки...")

		localDrives, err := bw.usbClient.GetLocalDrives()
		if err != nil {
			logrus.Errorf(i18n.Current.ErrorLoadingLocalDevices, err)
			return
		}

		for _, drive := range localDrives.Drives {
			if drive.SourceType == "mtp" && drive.Name == "data" {
				bw.currentFlash = &drive
				logrus.Infof("✅ Найдена актуальная бэкап флешка: %s", drive.Name)
				break
			}
		}

		bw.currentFlashConnected = false
		deviceInfo, err := bw.usbClient.GetDeviceInfo()
		if err == nil {
			for _, device := range deviceInfo.Devices {
				if device.Status == "connected" &&
					device.Type == "mtp" &&
					strings.Contains(device.Name, "data") &&
					!strings.Contains(device.ProductName, "snapshot") {
					bw.currentFlashConnected = true
					logrus.Infof("✅ Бэкап-флешка подключена: %s", device.Name)
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

// loadISOSpace загружает информацию о месте на SD-карте
func (bw *BackupWidget) loadISOSpace() {
	if bw.usbClient == nil {
		return
	}
	spaceInfo, err := bw.usbClient.GetISOSpace()
	if err != nil {
		logrus.Debugf("Информация о месте на SD-карте недоступна: %v", err)
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

// updateSDStorageInfo обновляет прогрессбар в main window через callback
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

// SetOnStorageInfoUpdate устанавливает callback для обновления прогрессбара в main window
func (bw *BackupWidget) SetOnStorageInfoUpdate(fn func(usedPct float64, available, total int64)) {
	bw.onStorageInfoUpdate = fn
}

// loadSnapshots загружает список снапшотов
func (bw *BackupWidget) loadSnapshots() {
	if !bw.loadingSnapshots.CompareAndSwap(false, true) {
		logrus.Debug("loadSnapshots already in flight, skipping overlapping refresh")
		return
	}
	go func() {
		defer bw.loadingSnapshots.Store(false)
		if bw.usbClient == nil {
			logrus.Debug("USB client not initialized, skipping snapshots loading")
			bw.updateUIAsync(func() {
				bw.ui.StatusLabel.SetText(i18n.Current.WaitingConnection)
			})
			return
		}

		bw.updateStatusAsync(i18n.Current.LoadingSnapshots)
		logrus.Debug("📦 Загрузка списка снапшотов...")

		snapshotsResp, err := bw.usbClient.GetSnapshots()
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

		logrus.Infof("✅ Загружено %d снапшотов", len(bw.snapshots))
	}()
}

// startPeriodicRefresh запускает периодическое обновление снапшотов
func (bw *BackupWidget) startPeriodicRefresh() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			bw.Refresh()
		}
	}()
}
