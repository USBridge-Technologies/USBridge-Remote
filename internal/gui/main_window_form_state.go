package gui

import (
	"strings"

	"usbridge-client/internal/models"
)

const (
	connectionDraftHostPrefKey            = "connection_draft_host"
	connectionDraftQUICTokenPrefKey       = "connection_draft_quic_token"
	connectionDraftProtocolPrefKey        = "connection_draft_protocol"
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

	quicToken := ""
	if mw.tokenEntry != nil {
		quicToken = mw.tokenEntry.Text
	}

	protocol := models.ConnectionProtocolAuto
	if mw.protocolSelect != nil {
		protocol = strings.TrimSpace(mw.protocolSelect.Selected)
	}
	if protocol == "" {
		protocol = models.ConnectionProtocolAuto
	}

	prefs.SetString(connectionDraftHostPrefKey, host)
	prefs.SetString(connectionDraftQUICTokenPrefKey, quicToken)
	prefs.SetString(connectionDraftProtocolPrefKey, protocol)
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
	quicToken := prefs.StringWithFallback(connectionDraftQUICTokenPrefKey, "")
	
	// Legacy fallback
	if quicToken == "" {
		quicToken = prefs.StringWithFallback("connection_draft_token", "")
	}

	// Очищаем старый дефолтный токен, если он застрял в преференсах
	if quicToken == "usbridge-secret-token" {
		quicToken = ""
		prefs.SetString(connectionDraftQUICTokenPrefKey, "")
	}

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
		mw.tokenEntry.SetText(quicToken)
	}
	if mw.protocolSelect != nil {
		mw.protocolSelect.SetSelected(protocol)
	}
}
