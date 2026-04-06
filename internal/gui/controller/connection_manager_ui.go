package controller

import (
	"net/url"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (cm *ConnectionManager) createInterface() {
	cm.ui = view.NewConnectionManagerUI(cm.handleQRScan, cm.showAddDialog, cm.openQuickStartDocs)
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
			Label:    i18n.Current.LanguageSpanish,
			Selected: currentLanguage == "es",
			OnTap: func() {
				cm.setLanguage("es")
			},
		},
		{
			Label:    i18n.Current.LanguageUkrainian,
			Selected: currentLanguage == "uk" || currentLanguage == "ua",
			OnTap: func() {
				cm.setLanguage("uk")
			},
		},
	})
}

func (cm *ConnectionManager) openQuickStartDocs() {
	const docsURL = "https://www.usbridge.io/docs/getting-started/quick-start-guide/"

	cm.openExternalLink(docsURL, "docs URL")
}

func (cm *ConnectionManager) openDiscordInvite() {
	const discordURL = "https://discord.gg/XwNpCrGfsB"

	cm.openExternalLink(discordURL, "Discord invite URL")
}

func (cm *ConnectionManager) openExternalLink(rawURL string, label string) {
	uri, err := url.Parse(rawURL)
	if err != nil {
		logrus.Errorf("failed to parse %s %q: %v", label, rawURL, err)
		return
	}

	app := cm.app
	if app == nil {
		app = fyne.CurrentApp()
	}
	if app == nil {
		logrus.Errorf("failed to open %s: fyne app is nil", label)
		return
	}

	go func() {
		if err := openExternalURL(app, uri); err != nil {
			logrus.Errorf("failed to open %s %q: %v", label, rawURL, err)
		}
	}()
}

func (cm *ConnectionManager) refreshConnectionsList() {
	if len(cm.connections) == 0 {
		cm.ui.SetEmptyState()
		cm.notifyConnectionsState()
		return
	}

	rows := make([]*fyne.Container, 0, len(cm.connections))
	for i, conn := range cm.connections {
		rows = append(rows, cm.createConnectionRow(conn, i))
	}
	cm.ui.SetRows(rows)
	cm.notifyConnectionsState()
}

func (cm *ConnectionManager) createConnectionRow(conn SavedConnection, idx int) *fyne.Container {
	conn.Protocol = normalizeConnectionProtocol(conn.Protocol)
	rowState := view.ConnectionRowState{
		Disabled: cm.connectionPending,
		Loading:  cm.connectionPending && cm.activeConnectionIndex == idx,
	}

	fillForm := func() {
		if cm.connectionPending {
			return
		}

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
			ProtocolOptions: []string{
				connectionProtocolBadge(models.ConnectionProtocolAuto),
				connectionProtocolBadge(models.ConnectionProtocolQUIC),
				connectionProtocolBadge(models.ConnectionProtocolWireGuard),
			},
		},
		rowState,
		view.ConnectionRowActions{
			OnSelect: fillForm,
			OnUse: func() {
				if !cm.beginConnectionFromRow(idx) {
					return
				}

				fyne.Do(func() {
					cm.hostEntry.SetText(conn.Host)
					cm.tokenEntry.SetText(conn.Token)
					cm.selectedIndex = idx
					if cm.onConnect != nil {
						protocol := normalizeConnectionProtocol(cm.connections[idx].Protocol)
						cm.onConnect(conn.Host, conn.Token, protocol, conn.WireGuardInvite)
						return
					}
					cm.SetConnectionPending(false)
				})
			},
			OnEdit: func() {
				if cm.connectionPending {
					return
				}
				cm.showEditDialog(idx)
			},
			OnProtocolChange: func(label string) {
				if cm.connectionPending {
					return
				}
				cm.updateConnectionProtocol(idx, connectionProtocolFromBadge(label))
			},
		},
	)
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

func (cm *ConnectionManager) ShowLanguageMenu(anchor fyne.CanvasObject) {
	cm.showLanguageMenu(anchor)
}

func (cm *ConnectionManager) OpenQuickStartDocs() {
	cm.openQuickStartDocs()
}

func (cm *ConnectionManager) OpenDiscordInvite() {
	cm.openDiscordInvite()
}
