package controller

import (
	"net/url"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

func (cm *ConnectionManager) createInterface() {
	cm.ui = view.NewConnectionManagerUI(cm.showLanguageMenu, cm.openQuickStartDocs, cm.handleQRScan, cm.showAddDialog)
	cm.refreshConnectionsList()
}

func (cm *ConnectionManager) showLanguageMenu(anchor fyne.CanvasObject) {
	currentLanguage := cm.app.Preferences().StringWithFallback("language", "en")
	view.ShowStyledMenu(anchor, []view.StyledMenuItem{
		{
			Label:    i18n.Current.LanguageEnglish,
			Selected: currentLanguage == "en",
			OnTap: func() {
				cm.setLanguage("en")
			},
		},
		{
			Label:    i18n.Current.LanguageRussian,
			Selected: currentLanguage == "ru",
			OnTap: func() {
				cm.setLanguage("ru")
			},
		},
	})
}

func (cm *ConnectionManager) openQuickStartDocs() {
	const docsURL = "https://www.usbridge.io/docs/getting-started/quick-start-guide/"

	uri, err := url.Parse(docsURL)
	if err != nil {
		logrus.Errorf("failed to parse docs URL %q: %v", docsURL, err)
		return
	}

	app := cm.app
	if app == nil {
		app = fyne.CurrentApp()
	}
	if app == nil {
		logrus.Error("failed to open docs URL: fyne app is nil")
		return
	}

	if err := app.OpenURL(uri); err != nil {
		logrus.Errorf("failed to open docs URL %q: %v", docsURL, err)
	}
}

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

func (cm *ConnectionManager) GetContainer() *fyne.Container {
	return cm.ui.Container
}

func (cm *ConnectionManager) SetLanguageChangeCallback(callback func()) {
	cm.onLanguageChange = callback
}
