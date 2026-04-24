package controller

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

func normalizeConnectionProtocol(protocol string) string {
	switch strings.TrimSpace(protocol) {
	case models.ConnectionProtocolQUIC:
		return models.ConnectionProtocolQUIC
	case models.ConnectionProtocolTailscale:
		return models.ConnectionProtocolTailscale
	case models.ConnectionProtocolWireGuard:
		return models.ConnectionProtocolTailscale
	default:
		return models.ConnectionProtocolAuto
	}
}

func connectionProtocolBadge(protocol string) string {
	switch normalizeConnectionProtocol(protocol) {
	case models.ConnectionProtocolTailscale:
		return "ts"
	case models.ConnectionProtocolQUIC:
		return "quic"
	default:
		return models.ConnectionProtocolAuto
	}
}

func connectionProtocolFromBadge(label string) string {
	switch strings.TrimSpace(strings.ToLower(label)) {
	case "ts":
		return models.ConnectionProtocolTailscale
	case models.ConnectionProtocolQUIC:
		return models.ConnectionProtocolQUIC
	default:
		return models.ConnectionProtocolAuto
	}
}

type SavedConnection struct {
	Name              string `json:"name"`
	InternalHost      string `json:"internal_host,omitempty"`
	TailscaleHost     string `json:"tailscale_host,omitempty"`
	QUICPort          int    `json:"quic_port,omitempty"`
	Host              string `json:"host,omitempty"`
	Token             string `json:"token"`
	Protocol          string `json:"protocol,omitempty"`
	WireGuardInvite   string `json:"wireguard_invite,omitempty"`
	TailscaleRegister bool   `json:"tailscale_register,omitempty"`
	RemoteOS          string `json:"remote_os,omitempty"`
}

type ConnectionManager struct {
	app    fyne.App
	window fyne.Window
	config *models.AppConfig
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
	ts        *service.TailscaleService
	tsStatus  *service.TailscaleStatus

	onConnect                func(host, token, protocol, wireGuardInvite string, quicPort int, tailscaleRegister bool)
	onSelect                 func(wireGuardInvite string, tailscaleRegister bool)
	onLanguageChange         func()
	onConnectionsStateChange func(bool)
	tsPollStop               chan struct{}
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
		internalHost, tailscaleHost := classifyConnectionHosts(conn)
		if strings.TrimSpace(conn.Host) == normalizedHost ||
			internalHost == normalizedHost ||
			tailscaleHost == normalizedHost {
			return strings.TrimSpace(conn.Token)
		}
	}

	return ""
}

func (cm *ConnectionManager) ResolveInternalHost(host string) string {
	normalizedHost := strings.TrimSpace(host)
	if normalizedHost == "" {
		return ""
	}

	for _, conn := range cm.connections {
		internalHost, tailscaleHost := classifyConnectionHosts(conn)
		if internalHost == "" {
			continue
		}
		if strings.TrimSpace(conn.Host) == normalizedHost || tailscaleHost == normalizedHost || internalHost == normalizedHost {
			return internalHost
		}
	}
	return ""
}

func NewConnectionManager(app fyne.App, window fyne.Window, config *models.AppConfig, hostEntry, tokenEntry *widget.Entry, protocolSelect *widget.Select, ts *service.TailscaleService, onConnect func(host, token, protocol, wireGuardInvite string, quicPort int, tailscaleRegister bool), onSelect func(wireGuardInvite string, tailscaleRegister bool)) *ConnectionManager {
	cm := &ConnectionManager{
		app:                   app,
		window:                window,
		config:                config,
		hostEntry:             hostEntry,
		tokenEntry:            tokenEntry,
		protocolSelect:        protocolSelect,
		onConnect:             onConnect,
		onSelect:              onSelect,
		selectedIndex:         -1,
		connections:           make([]SavedConnection, 0),
		activeConnectionIndex: -1,
		ts:                    ts,
	}
	if cm.ts == nil {
		cm.ts = service.NewTailscaleService(config.TailscaleUserspace)
	}


	cm.qrScanner = NewQRScanner(
		app,
		func(host, token, protocol, wireGuardInvite string, quicPort int, tailscaleRegister bool) {
			fyne.Do(func() {
				cm.ClearSelection()
				cm.applyConnectionToForm(host, token, protocol)
			})
			if cm.onConnect != nil {
				cm.onConnect(host, token, protocol, wireGuardInvite, quicPort, tailscaleRegister)
			}
			logrus.Infof("QR connect: host=%s quicPort=%d", host, quicPort)
		},
		func(name, internalHost, tailscaleHost, token, protocol, wireGuardInvite string, quicPort int, tailscaleRegister bool) {
			cm.SaveConnection(name, internalHost, tailscaleHost, token, protocol, wireGuardInvite, quicPort, tailscaleRegister)
			fyne.Do(func() {
				cm.applyConnectionToForm(resolveScannedHost(protocol, internalHost, tailscaleHost), token, protocol)
			})
			logrus.Infof("QR saved directly: internal=%s tailscale=%s quicPort=%d", internalHost, tailscaleHost, quicPort)
		},
		func(internalHost, tailscaleHost, token, protocol, wireGuardInvite string, quicPort int, scanned bool) {
			cm.showPrefilledAddDialog("", internalHost, tailscaleHost, token, protocol, wireGuardInvite, quicPort, scanned)
		},
	)

	cm.loadConnections()
	cm.createInterface()
	cm.startTailscaleStatusPolling()
	return cm
}

func (cm *ConnectionManager) startTailscaleAuthAction() {
	if cm.ts == nil {
		return
	}
	go func() {
		status, err := cm.ts.Status(context.Background())
		if err == nil && status != nil && status.LoggedIn {
			logrus.Info("tailscale client ui: logout button pressed")
			cm.setTailscaleStateAsync(
				"Tailscale: signing out",
				"Google: disconnecting account",
				"Address: unavailable",
				"Sign Out",
			)
			if logoutErr := cm.ts.Logout(context.Background()); logoutErr != nil {
				logrus.WithError(logoutErr).Error("tailscale client ui: Logout failed")
			}
			cm.refreshTailscaleStatus()
			return
		}

		cm.setTailscaleStateAsync(
			"Tailscale: starting login",
			"Google: waiting for browser sign-in",
			"Address: unavailable until login completes",
			"Sign In With Google",
		)
		logrus.Info("tailscale client ui: login button pressed")
		authURL, err := cm.ts.StartLogin(context.Background())
		if err != nil {
			logrus.WithError(err).Error("tailscale client ui: StartLogin failed")
			cm.setTailscaleStateAsync(
				"Tailscale: login failed",
				fmt.Sprintf("Google: %v", err),
				"Address: unavailable",
				"Sign In With Google",
			)
			return
		}
		if strings.TrimSpace(authURL) != "" {
			logrus.Infof("tailscale client ui: auth URL received %s", authURL)
			cm.setTailscaleStateAsync(
				"Tailscale: auth URL received",
				"Google: opening browser",
				authURL,
				"Sign In With Google",
			)
			cm.openExternalLink(authURL, "Tailscale login URL")
			cm.setTailscaleStateAsync(
				"Tailscale: browser opened",
				"Google: complete sign-in in browser",
				"Address: waiting for tailnet assignment",
				"Sign In With Google",
			)
		} else {
			logrus.Info("tailscale client ui: StartLogin returned without auth URL")
		}
		cm.refreshTailscaleStatus()
	}()
}

func (cm *ConnectionManager) handleTailscaleToggleAction() {
	if cm.tsStatus != nil && cm.tsStatus.LoggedIn {
		view.ShowConfirmYesLeft(
			i18n.Current.Confirmation,
			i18n.Current.TailscaleLogoutConfirm,
			func(confirmed bool) {
				if confirmed {
					cm.startTailscaleAuthAction()
				}
			},
			cm.window,
		)
		return
	}

	cm.startTailscaleAuthAction()
}

func (cm *ConnectionManager) refreshTailscaleStatus() {
	if cm.ts == nil || cm.ui == nil {
		return
	}
	go func() {
		status, err := cm.ts.Status(context.Background())
		if err != nil {
			cm.tsStatus = nil
			cm.setTailscaleStateAsync(
				fmt.Sprintf("Tailscale: %v", err),
				"Google: not connected",
				"Address: unavailable",
				"Sign In With Google",
			)
			return
		}
		cm.tsStatus = status
		if !status.LoggedIn {
			cm.setTailscaleStateAsync("Tailscale: signed out", "Google: sign in required", "Address: sign in to get your tailnet address", "Sign In With Google")
			return
		}

		address := ""
		if dns := strings.TrimSpace(status.Self.DNSName); dns != "" {
			address = dns
		} else if ip := strings.TrimSpace(status.Self.IP4); ip != "" {
			address = ip
		} else {
			address = status.Self.HostName
		}

		cm.setTailscaleStateAsync(
			fmt.Sprintf("Tailscale: %s", strings.ToLower(strings.TrimSpace(status.Backend))),
			fmt.Sprintf("Google: %s", fallbackText(status.Self.UserLogin, "connected")),
			fmt.Sprintf("Address: %s (%s)", address, ternary(status.Userspace, "embedded", "system")),
			"Sign Out",
		)
	}()
}

func (cm *ConnectionManager) startTailscaleStatusPolling() {
	if cm == nil || cm.ts == nil {
		return
	}
	if cm.tsPollStop != nil {
		close(cm.tsPollStop)
	}
	stop := make(chan struct{})
	cm.tsPollStop = stop

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				cm.refreshTailscaleStatus()
			case <-stop:
				return
			}
		}
	}()
}

func (cm *ConnectionManager) setTailscaleStateAsync(status, account, address, authLabel string) {
	if cm == nil || cm.ui == nil {
		return
	}
	fyne.Do(func() {
		if cm.ui != nil {
			cm.ui.SetTailscaleState(status, account, address, authLabel)
		}
	})
}

func ternary[T any](condition bool, ifTrue, ifFalse T) T {
	if condition {
		return ifTrue
	}
	return ifFalse
}

func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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

func (cm *ConnectionManager) HandleFormEdited(host, token, protocol string) bool {
	if cm == nil || cm.syncingForm {
		return false
	}
	if cm.selectedIndex < 0 {
		return true // Selection already cleared
	}

	host = strings.TrimSpace(host)
	token = strings.TrimSpace(token)
	protocol = normalizeConnectionProtocol(protocol)

	current := cm.connections[cm.selectedIndex]
	if strings.TrimSpace(current.Host) == host &&
		strings.TrimSpace(current.Token) == token &&
		normalizeConnectionProtocol(current.Protocol) == protocol {
		return false
	}

	cm.selectedIndex = -1
	return true
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
	cm.applyConnectionToForm(cm.resolveHostForProtocol(conn, conn.Protocol), conn.Token, conn.Protocol)

	if cm.onSelect != nil {
		cm.onSelect(conn.WireGuardInvite, conn.TailscaleRegister)
	}
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

func (cm *ConnectionManager) resolveHostForProtocol(conn SavedConnection, protocol string) string {
	internalHost, tailscaleHost := classifyConnectionHosts(conn)
	switch normalizeConnectionProtocol(protocol) {
	case models.ConnectionProtocolTailscale:
		return fallbackText(tailscaleHost, internalHost)
	case models.ConnectionProtocolQUIC:
		return fallbackText(internalHost, tailscaleHost)
	default:
		return fallbackText(tailscaleHost, internalHost)
	}
}

func classifyConnectionHosts(conn SavedConnection) (internalHost, tailscaleHost string) {
	internalHost = strings.TrimSpace(conn.InternalHost)
	tailscaleHost = strings.TrimSpace(conn.TailscaleHost)
	legacyHost := strings.TrimSpace(conn.Host)

	if internalHost == "" && tailscaleHost == "" {
		if isLikelyTailnetHost(legacyHost) {
			tailscaleHost = legacyHost
		} else {
			internalHost = legacyHost
		}
	}
	if internalHost == "" && !isLikelyTailnetHost(tailscaleHost) && strings.TrimSpace(tailscaleHost) != "" {
		internalHost = tailscaleHost
	}
	if tailscaleHost == "" && isLikelyTailnetHost(internalHost) {
		tailscaleHost = internalHost
		internalHost = ""
	}
	return strings.TrimSpace(internalHost), strings.TrimSpace(tailscaleHost)
}

func isLikelyTailnetHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.HasSuffix(strings.ToLower(host), ".ts.net") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return netip.MustParsePrefix("100.64.0.0/10").Contains(addr)
}

func splitHostByType(host string) (internalHost, tailscaleHost string) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", ""
	}
	if isLikelyTailnetHost(host) {
		return "", host
	}
	return host, ""
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
