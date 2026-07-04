package gui

import (
	"strings"

	"usbridge-client/internal/models"
)

const (
	connectionDraftHostPrefKey      = "connection_draft_host"
	connectionDraftMasterKeyPrefKey = "connection_draft_quic_token"
	connectionDraftProtocolPrefKey  = "connection_draft_protocol"
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

	masterKey := ""
	if mw.tokenEntry != nil {
		masterKey = mw.tokenEntry.Text
	}

	protocol := models.ConnectionProtocolAuto
	if mw.protocolSelect != nil {
		protocol = strings.TrimSpace(mw.protocolSelect.Selected)
	}
	if protocol == "" {
		protocol = models.ConnectionProtocolAuto
	}

	prefs.SetString(connectionDraftHostPrefKey, host)
	prefs.SetString(connectionDraftMasterKeyPrefKey, masterKey)
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
	masterKey := prefs.StringWithFallback(connectionDraftMasterKeyPrefKey, "")

	// Legacy fallback
	if masterKey == "" {
		masterKey = prefs.StringWithFallback("connection_draft_token", "")
	}

	// Очищаем старый дефолтный токен, если он застрял в преференсах
	if masterKey == "usbridge-secret-token" {
		masterKey = ""
		prefs.SetString(connectionDraftMasterKeyPrefKey, "")
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
		mw.tokenEntry.SetText(masterKey)
	}
	if mw.protocolSelect != nil {
		mw.protocolSelect.SetSelected(protocol)
	}
}
