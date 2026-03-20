package controller

import (
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// createInterface создает интерфейс менеджера
func (cm *ConnectionManager) createInterface() {
	cm.ui = view.NewConnectionManagerUI(cm.showLanguageMenu, cm.handleQRScan, cm.showAddDialog)
	cm.refreshConnectionsList()
}

// showLanguageMenu показывает меню выбора языка
func (cm *ConnectionManager) showLanguageMenu(btn *widget.Button) {
	menu := fyne.NewMenu("",
		fyne.NewMenuItem(i18n.Current.LanguageEnglish, func() { cm.setLanguage("en") }),
		fyne.NewMenuItem(i18n.Current.LanguageRussian, func() { cm.setLanguage("ru") }),
	)
	popup := widget.NewPopUpMenu(menu, cm.window.Canvas())
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(btn)
	popup.ShowAtPosition(pos.Add(fyne.NewPos(0, btn.Size().Height)))
}

// refreshConnectionsList перерисовывает список подключений
func (cm *ConnectionManager) refreshConnectionsList() {
	if len(cm.connections) == 0 {
		cm.ui.SetEmptyState()
		return
	}

	rows := make([]*fyne.Container, 0, len(cm.connections))
	for i, conn := range cm.connections {
		rows = append(rows, cm.createConnectionRow(conn, i))
	}
	cm.ui.SetRows(rows)
}

// createConnectionRow создает карточку для подключения
func (cm *ConnectionManager) createConnectionRow(conn SavedConnection, idx int) *fyne.Container {
	conn.Protocol = normalizeConnectionProtocol(conn.Protocol)

	fillForm := func() {
		fyne.Do(func() {
			cm.hostEntry.SetText(conn.Host)
			cm.tokenEntry.SetText(conn.Token)
			cm.selectedIndex = idx
		})
	}

	return view.NewConnectionRow(
		view.ConnectionRowData{
			Name:          conn.Name,
			Host:          conn.Host,
			ProtocolBadge: connectionProtocolBadge(conn.Protocol),
			EditLabel:     i18n.Current.EditButton,
		},
		view.ConnectionRowActions{
			OnSelect: fillForm,
			OnUse: func() {
				fyne.Do(func() {
					cm.hostEntry.SetText(conn.Host)
					cm.tokenEntry.SetText(conn.Token)
					cm.selectedIndex = idx
					if cm.onConnect != nil {
						protocol := normalizeConnectionProtocol(cm.connections[idx].Protocol)
						cm.onConnect(conn.Host, conn.Token, protocol, conn.WireGuardInvite)
					}
				})
			},
			OnEdit: func() {
				cm.showEditDialog(idx)
			},
			OnDelete: func() {
				cm.handleDeleteConnection(idx)
			},
			OnProtocolMenu: func(btn *widget.Button) {
				cm.showProtocolMenu(btn, idx)
			},
		},
	)
}

func (cm *ConnectionManager) showProtocolMenu(protocolBtn *widget.Button, idx int) {
	menu := fyne.NewMenu("",
		fyne.NewMenuItem(connectionProtocolBadge(models.ConnectionProtocolAuto), func() {
			cm.updateConnectionProtocol(idx, models.ConnectionProtocolAuto)
		}),
		fyne.NewMenuItem(connectionProtocolBadge(models.ConnectionProtocolQUIC), func() {
			cm.updateConnectionProtocol(idx, models.ConnectionProtocolQUIC)
		}),
		fyne.NewMenuItem(connectionProtocolBadge(models.ConnectionProtocolWireGuard), func() {
			cm.updateConnectionProtocol(idx, models.ConnectionProtocolWireGuard)
		}),
	)
	popup := widget.NewPopUpMenu(menu, cm.window.Canvas())
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(protocolBtn)
	popup.ShowAtPosition(pos.Add(fyne.NewPos(0, protocolBtn.Size().Height)))
}

func (cm *ConnectionManager) updateConnectionProtocol(idx int, protocol string) {
	cm.connections[idx].Protocol = protocol
	cm.saveConnections()
	cm.refreshConnectionsList()
}

// GetContainer возвращает контейнер
func (cm *ConnectionManager) GetContainer() *fyne.Container {
	return cm.ui.Container
}

// SetLanguageChangeCallback устанавливает callback для изменения языка
func (cm *ConnectionManager) SetLanguageChangeCallback(callback func()) {
	cm.onLanguageChange = callback
}
