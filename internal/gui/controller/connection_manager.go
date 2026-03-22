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
	return normalizeConnectionProtocol(protocol)
}

func connectionProtocolFromBadge(label string) string {
	return normalizeConnectionProtocol(label)
}

type SavedConnection struct {
	Name            string `json:"name"`
	Host            string `json:"host"`
	Token           string `json:"token"`
	Protocol        string `json:"protocol,omitempty"`
	WireGuardInvite string `json:"wireguard_invite,omitempty"`
}

type ConnectionManager struct {
	app    fyne.App
	window fyne.Window
	ui     *view.ConnectionManagerUI

	connections   []SavedConnection
	selectedIndex int

	hostEntry      *widget.Entry
	tokenEntry     *widget.Entry
	protocolSelect *widget.Select

	qrScanner *QRScanner

	onConnect                func(host, token, protocol, wireGuardInvite string)
	onLanguageChange         func()
	onConnectionsStateChange func(bool)
}

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

	cm.qrScanner = NewQRScanner(
		app,
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
			logrus.Infof("QR connect: host=%s", host)
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
			logrus.Infof("QR saved directly: host=%s", host)
		},
		func(host, token, protocol, wireGuardInvite string) {
			cm.showPrefilledAddDialog("", host, token, protocol, wireGuardInvite, true)
		},
	)

	cm.loadConnections()
	cm.createInterface()
	return cm
}

func (cm *ConnectionManager) SetConnectionsStateCallback(callback func(bool)) {
	cm.onConnectionsStateChange = callback
	cm.notifyConnectionsState()
}

func (cm *ConnectionManager) notifyConnectionsState() {
	if cm.onConnectionsStateChange != nil {
		cm.onConnectionsStateChange(len(cm.connections) > 0)
	}
}
