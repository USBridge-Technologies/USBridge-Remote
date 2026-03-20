package controller

import (
	"strings"

	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

func normalizeConnectionProtocol(protocol string) string {
	switch strings.TrimSpace(protocol) {
	case models.ConnectionProtocolQUIC:
		return models.ConnectionProtocolQUIC
	case models.ConnectionProtocolWireGuard:
		return models.ConnectionProtocolWireGuard
	default:
		return models.ConnectionProtocolAuto
	}
}

func connectionProtocolBadge(protocol string) string {
	switch normalizeConnectionProtocol(protocol) {
	case models.ConnectionProtocolWireGuard:
		return "◆ WG"
	case models.ConnectionProtocolQUIC:
		return "▲ QUIC"
	default:
		return "● AUTO"
	}
}

// SavedConnection сохраненное подключение
type SavedConnection struct {
	Name            string `json:"name"`
	Host            string `json:"host"`
	Token           string `json:"token"`
	Protocol        string `json:"protocol,omitempty"`
	WireGuardInvite string `json:"wireguard_invite,omitempty"`
}

// ConnectionManager менеджер подключений
type ConnectionManager struct {
	app    fyne.App
	window fyne.Window
	ui     *view.ConnectionManagerUI

	// Данные
	connections   []SavedConnection
	selectedIndex int

	// UI элементы
	hostEntry      *widget.Entry
	tokenEntry     *widget.Entry
	protocolSelect *widget.Select

	// QR сканер
	qrScanner *QRScanner

	// Callbacks
	onConnect        func(host, token, protocol, wireGuardInvite string)
	onLanguageChange func()
}

// NewConnectionManager создает новый менеджер подключений
func NewConnectionManager(app fyne.App, window fyne.Window, hostEntry, tokenEntry *widget.Entry, protocolSelect *widget.Select, onConnect func(host, token, protocol, wireGuardInvite string)) *ConnectionManager {
	cm := &ConnectionManager{
		app:            app,
		window:         window,
		hostEntry:      hostEntry,
		tokenEntry:     tokenEntry,
		protocolSelect: protocolSelect,
		onConnect:      onConnect,
		selectedIndex:  -1,
		connections:    make([]SavedConnection, 0),
	}

	cm.qrScanner = NewQRScanner(app,
		func(host, token, protocol, wireGuardInvite string) {
			fyne.Do(func() {
				cm.hostEntry.SetText(host)
				cm.tokenEntry.SetText(token)
				if cm.protocolSelect != nil && protocol != "" {
					cm.protocolSelect.SetSelected(protocol)
				}
			})
			if cm.onConnect != nil {
				cm.onConnect(host, token, protocol, wireGuardInvite)
			}
			logrus.Infof("QR подключение: host=%s", host)
		},
		func(name, host, token, protocol, wireGuardInvite string) {
			cm.SaveConnection(name, host, token, protocol, wireGuardInvite)
			fyne.Do(func() {
				cm.hostEntry.SetText(host)
				cm.tokenEntry.SetText(token)
				if cm.protocolSelect != nil && protocol != "" {
					cm.protocolSelect.SetSelected(protocol)
				}
			})
			logrus.Infof("QR сохранено: host=%s", host)
		},
	)

	cm.loadConnections()
	cm.createInterface()
	return cm
}
