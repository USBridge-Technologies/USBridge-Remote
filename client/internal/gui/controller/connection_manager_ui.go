package controller

import (
	"strings"

	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
)

func (cm *ConnectionManager) createInterface() {
	cm.ui = view.NewConnectionManagerUI(
		cm.handleQRScan,
		cm.showAddDialog,
		cm.openQuickStartDocs,
		cm.openHardwarePromo,
		cm.handleTailscaleToggleAction,
	)
	cm.refreshConnectionsList()
	cm.initTailscaleMode()
}

func (cm *ConnectionManager) initTailscaleMode() {
	// Tailscale always runs in userspace (tsnet) mode on every platform — no
	// system VPN daemon involved.

	// Do NOT eagerly start tsnet here just because TailscaleEnabled defaults to
	// true. Starting tsnet while unauthenticated triggers StartLoginInteractive,
	// which pops an auth browser window on top of the app on every launch/connect
	// — even for a plain LAN/direct connection the user never asked to route via
	// Tailscale. tsnet is started lazily instead: by the explicit "Sign In With
	// Google" button (startTailscaleAuthAction) or by a connection attempt that
	// actually targets a tailnet host. Status() will not auto-start tsnet if the
	// server hasn't been explicitly started yet.
	//
	// Run off the main goroutine: this runs during startup before the Fyne
	// event loop is pumping, and refreshTailscaleStatus ends up calling
	// fyne.Do, which errors if invoked directly from the main goroutine.
	go cm.refreshTailscaleStatus()
}

func (cm *ConnectionManager) showLanguageMenu(anchor fyne.CanvasObject) {
	currentLanguage := cm.app.Preferences().StringWithFallback("language", "en")
	view.ShowStyledMenu(anchor, []view.StyledMenuItem{
		{
			Label:    "English",
			Selected: currentLanguage == "en",
			OnTap: func() {
				cm.setLanguage("en")
			},
		},
		{
			Label:    "Español",
			Selected: currentLanguage == "es",
			OnTap: func() {
				cm.setLanguage("es")
			},
		},
		{
			Label:    "Українська",
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

func (cm *ConnectionManager) openHardwarePromo() {
	const promoURL = "https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0"

	cm.openExternalLink(promoURL, "hardware promo URL")
}

// RefreshList forces an immediate re-render of the connections list.
// Call this whenever the connection manager becomes visible so stale
// "TS: none" entries are replaced with the latest saved Tailscale addresses.
func (cm *ConnectionManager) RefreshList() {
	if cm == nil {
		return
	}
	cm.refreshConnectionsList()
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
	internalHost, tailscaleHost := classifyConnectionHosts(conn)
	rowState := view.ConnectionRowState{
		Disabled: cm.connectionPending,
		Loading:  cm.connectionPending && cm.activeConnectionIndex == idx,
	}

	fillForm := func() {
		if cm.connectionPending {
			return
		}

		fyne.Do(func() {
			cm.SelectConnection(idx)
		})
	}

	return view.NewConnectionRow(
		view.ConnectionRowData{
			Name:           conn.Name,
			AddressSummary: formatConnectionAddressSummary(internalHost, tailscaleHost),
			ProtocolBadge:  connectionProtocolBadge(conn.Protocol),
			ProtocolOptions: []string{
				connectionProtocolBadge(models.ConnectionProtocolAuto),
				connectionProtocolBadge(models.ConnectionProtocolTailscale),
				connectionProtocolBadge(models.ConnectionProtocolDirect),
			},
			RegisterChecked: conn.TailscaleRegister,
			RegisterVisible: internalHost != "" && tailscaleHost == "",
			RemoteOS:        conn.RemoteOS,
		},
		rowState,
		view.ConnectionRowActions{
			OnSelect: fillForm,
			OnUse: func() {
				if !cm.beginConnectionFromRow(idx) {
					return
				}

				fyne.Do(func() {
					cm.SelectConnection(idx)
					if cm.onConnect != nil {
						conn := cm.connections[idx]
						protocol := normalizeConnectionProtocol(conn.Protocol)
						host := cm.resolveHostForProtocol(conn, protocol)
						cm.onConnect(host, conn.MasterKey, conn.FRPToken, protocol, conn.QUICPort, conn.TailscaleRegister)
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
			OnRegisterChange: func(checked bool) {
				if cm.connectionPending {
					return
				}
				cm.connections[idx].TailscaleRegister = checked
				cm.saveConnections()
				// If this is the currently selected connection, update the main check too
				if cm.selectedIndex == idx && cm.onSelect != nil {
					cm.onSelect(checked)
				}
			},
		},
	)
}

func (cm *ConnectionManager) updateConnectionProtocol(idx int, protocol string) {
	cm.connections[idx].Protocol = protocol
	cm.saveConnections()
	cm.refreshConnectionsList()
}

func formatConnectionAddressSummary(internalHost, tailscaleHost string) string {
	internalHost = strings.TrimSpace(internalHost)
	tailscaleHost = strings.TrimSpace(tailscaleHost)
	if internalHost == "" {
		internalHost = "none"
	}
	if tailscaleHost == "" {
		tailscaleHost = "none"
	}
	return "LAN: " + internalHost + "\nTS: " + tailscaleHost
}

func (cm *ConnectionManager) ShowLanguageMenu(anchor fyne.CanvasObject) {
	cm.showLanguageMenu(anchor)
}
