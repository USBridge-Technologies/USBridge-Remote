package controller

import (
	"sync/atomic"
	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// BackupWidget виджет для отображения списка снапшотов
type BackupWidget struct {
	onStorageInfoUpdate func(usedPct float64, available, total int64) // Callback для main window
	window              fyne.Window
	ui                  *view.BackupWidgetUI

	// Данные
	snapshots             []*models.SnapshotInfo
	sdSpaceInfo           *models.ISOSpaceInfo // Информация о месте на SD-карте (iso/data/backup)
	currentFlash          *models.LocalDrive   // Актуальная бэкап флешка
	currentFlashConnected bool                 // Подключена ли бэкап-флешка (mtp:data)
	loadingCurrentFlash   atomic.Bool
	loadingSnapshots      atomic.Bool
	isMounting            atomic.Bool
	usbClient             *api.USBClient
	hostEntry             *widget.Entry
	updateStatus          func() // Callback для обновления статуса
	isClosing             atomic.Bool
}

// NewBackupWidget создает новый виджет backup
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

// SetWindow устанавливает окно для диалогов
func (bw *BackupWidget) SetWindow(window fyne.Window) {
	bw.window = window
}

// UpdateClient обновляет USB клиент
func (bw *BackupWidget) UpdateClient(usbClient *api.USBClient) {
	bw.usbClient = usbClient
	if usbClient != nil {
		bw.isClosing.Store(false)
	}
	if usbClient == nil {
		bw.sdSpaceInfo = nil
		bw.updateSDStorageInfo()
	}
	// Обновляем данные при смене клиента
	bw.loadCurrentFlash()
	bw.loadSnapshots()
}

// UpdateHostEntry обновляет ссылку на поле ввода хоста
func (bw *BackupWidget) UpdateHostEntry(hostEntry *widget.Entry) {
	bw.hostEntry = hostEntry
}

// GetContainer возвращает контейнер виджета
func (bw *BackupWidget) GetContainer() *fyne.Container {
	return bw.ui.Container
}

// Refresh обновляет виджет
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

// updateUIAsync безопасно обновляет UI из горутины
func (bw *BackupWidget) updateUIAsync(updateFunc func()) {
	// В Fyne используем fyne.Do для обновления UI из горутин
	fyne.Do(updateFunc)
}

// updateStatusAsync безопасно обновляет статус из горутины
func (bw *BackupWidget) updateStatusAsync(status string) {
	bw.updateUIAsync(func() {
		bw.ui.StatusLabel.SetText(status)
	})
}
