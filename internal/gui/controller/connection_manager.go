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
		return "wgrd"
	case models.ConnectionProtocolQUIC:
		return "quic"
	default:
		return models.ConnectionProtocolAuto
	}
}

func connectionProtocolFromBadge(label string) string {
	switch strings.TrimSpace(strings.ToLower(label)) {
	case "wgrd":
		return models.ConnectionProtocolWireGuard
	case models.ConnectionProtocolQUIC:
		return models.ConnectionProtocolQUIC
	default:
		return models.ConnectionProtocolAuto
	}
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

	connections           []SavedConnection
	selectedIndex         int
	connectionPending     bool
	activeConnectionIndex int
	syncingForm           bool

	hostEntry      *widget.Entry
	tokenEntry     *widget.Entry
	protocolSelect *widget.Select

	qrScanner *QRScanner

	onConnect                func(host, token, protocol, wireGuardInvite string)
	onLanguageChange         func()
	onConnectionsStateChange func(bool)
}

func (cm *ConnectionManager) ResolveToken(host, currentToken string) string {
	token := strings.TrimSpace(currentToken)
	if token != "" {
		return token
	}

	normalizedHost := strings.TrimSpace(host)
	if normalizedHost == "" {
		return ""
	}

	if cm.selectedIndex >= 0 && cm.selectedIndex < len(cm.connections) {
		conn := cm.connections[cm.selectedIndex]
		if strings.TrimSpace(conn.Host) == normalizedHost {
			return strings.TrimSpace(conn.Token)
		}
	}

	return ""
}

func NewConnectionManager(app fyne.App, window fyne.Window, hostEntry, tokenEntry *widget.Entry, protocolSelect *widget.Select, onConnect func(host, token, protocol, wireGuardInvite string)) *ConnectionManager {
	cm := &ConnectionManager{
		app:                   app,
		window:                window,
		hostEntry:             hostEntry,
		tokenEntry:            tokenEntry,
		protocolSelect:        protocolSelect,
		onConnect:             onConnect,
		selectedIndex:         -1,
		connections:           make([]SavedConnection, 0),
		activeConnectionIndex: -1,
	}

	cm.qrScanner = NewQRScanner(
		app,
		func(host, token, protocol, wireGuardInvite string) {
			fyne.Do(func() {
				cm.ClearSelection()
				cm.applyConnectionToForm(host, token, protocol)
			})
			if cm.onConnect != nil {
				cm.onConnect(host, token, protocol, wireGuardInvite)
			}
			logrus.Infof("QR connect: host=%s", host)
		},
		func(name, host, token, protocol, wireGuardInvite string) {
			cm.SaveConnection(name, host, token, protocol, wireGuardInvite)
			fyne.Do(func() {
				cm.applyConnectionToForm(host, token, protocol)
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

func (cm *ConnectionManager) beginConnectionFromRow(idx int) bool {
	if cm.connectionPending {
		return false
	}

	cm.setConnectionPendingState(true, idx)
	return true
}

func (cm *ConnectionManager) SetConnectionPending(pending bool) {
	activeIndex := cm.activeConnectionIndex
	if !pending {
		activeIndex = -1
	}
	cm.setConnectionPendingState(pending, activeIndex)
}

func (cm *ConnectionManager) HandleFormEdited(host, token, protocol string) {
	if cm == nil || cm.syncingForm || cm.selectedIndex < 0 || cm.selectedIndex >= len(cm.connections) {
		return
	}

	host = strings.TrimSpace(host)
	token = strings.TrimSpace(token)
	protocol = normalizeConnectionProtocol(protocol)

	current := cm.connections[cm.selectedIndex]
	if strings.TrimSpace(current.Host) == host &&
		strings.TrimSpace(current.Token) == token &&
		normalizeConnectionProtocol(current.Protocol) == protocol {
		return
	}

	cm.selectedIndex = -1
}

func (cm *ConnectionManager) ClearSelection() {
	if cm == nil {
		return
	}

	cm.selectedIndex = -1
}

func (cm *ConnectionManager) SelectConnection(idx int) {
	if cm == nil || idx < 0 || idx >= len(cm.connections) {
		return
	}

	cm.selectedIndex = idx
	conn := cm.connections[idx]
	cm.applyConnectionToForm(conn.Host, conn.Token, conn.Protocol)
}

func (cm *ConnectionManager) applyConnectionToForm(host, token, protocol string) {
	cm.syncingForm = true
	defer func() {
		cm.syncingForm = false
	}()

	if cm.hostEntry != nil {
		cm.hostEntry.SetText(strings.TrimSpace(host))
	}
	if cm.tokenEntry != nil {
		cm.tokenEntry.SetText(strings.TrimSpace(token))
	}
	if cm.protocolSelect != nil {
		cm.protocolSelect.SetSelected(normalizeConnectionProtocol(protocol))
	}
}

func (cm *ConnectionManager) setConnectionPendingState(pending bool, activeIndex int) {
	if pending && (activeIndex < 0 || activeIndex >= len(cm.connections)) {
		activeIndex = -1
	}
	if !pending {
		activeIndex = -1
	}

	if cm.connectionPending == pending && cm.activeConnectionIndex == activeIndex {
		return
	}

	cm.connectionPending = pending
	cm.activeConnectionIndex = activeIndex

	if cm.ui != nil {
		fyne.Do(func() {
			cm.ui.SetActionButtonsDisabled(pending)
			cm.refreshConnectionsList()
		})
	}
}
