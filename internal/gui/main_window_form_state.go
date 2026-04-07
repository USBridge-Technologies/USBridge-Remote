package gui

import (
	"strings"

	"usbridge-client/internal/models"
)

const (
	connectionDraftHostPrefKey            = "connection_draft_host"
	connectionDraftTokenPrefKey           = "connection_draft_token"
	connectionDraftProtocolPrefKey        = "connection_draft_protocol"
	connectionDraftWireGuardInvitePrefKey = "connection_draft_wireguard_invite"
)

func (mw *MainWindow) persistConnectionDraft() {
	if mw == nil || mw.app == nil {
		return
	}

	prefs := mw.app.Preferences()
	if prefs == nil {
		return
	}

	host := ""
	if mw.hostEntry != nil {
		host = strings.TrimSpace(mw.hostEntry.Text)
	}

	token := ""
	if mw.tokenEntry != nil {
		token = mw.tokenEntry.Text
	}

	protocol := models.ConnectionProtocolAuto
	if mw.protocolSelect != nil {
		protocol = strings.TrimSpace(mw.protocolSelect.Selected)
	}
	if protocol == "" {
		protocol = models.ConnectionProtocolAuto
	}

	prefs.SetString(connectionDraftHostPrefKey, host)
	prefs.SetString(connectionDraftTokenPrefKey, token)
	prefs.SetString(connectionDraftProtocolPrefKey, protocol)
	prefs.SetString(connectionDraftWireGuardInvitePrefKey, strings.TrimSpace(mw.pendingWireGuardInvite))
}

func (mw *MainWindow) restoreConnectionDraft() {
	if mw == nil || mw.app == nil {
		return
	}

	prefs := mw.app.Preferences()
	if prefs == nil {
		return
	}

	host := strings.TrimSpace(prefs.StringWithFallback(connectionDraftHostPrefKey, ""))
	token := prefs.StringWithFallback(connectionDraftTokenPrefKey, "")
	protocol := strings.TrimSpace(prefs.StringWithFallback(connectionDraftProtocolPrefKey, mw.config.ConnectionProtocol))
	if protocol == "" {
		protocol = mw.config.ConnectionProtocol
	}
	if protocol == "" {
		protocol = models.ConnectionProtocolAuto
	}

	if mw.hostEntry != nil {
		mw.hostEntry.SetText(host)
	}
	if mw.tokenEntry != nil {
		mw.tokenEntry.SetText(token)
	}
	if mw.protocolSelect != nil {
		mw.protocolSelect.SetSelected(protocol)
	}

	mw.pendingWireGuardInvite = strings.TrimSpace(
		prefs.StringWithFallback(connectionDraftWireGuardInvitePrefKey, ""),
	)
}
