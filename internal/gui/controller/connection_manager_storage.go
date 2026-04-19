package controller

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	"github.com/sirupsen/logrus"
)

// SaveConnection сохраняет подключение напрямую
func (cm *ConnectionManager) SaveConnection(name, internalHost, tailscaleHost, token, protocol, wireGuardInvite string, tailscaleRegister bool) string {
	internalHost = strings.TrimSpace(internalHost)
	tailscaleHost = strings.TrimSpace(tailscaleHost)
	if internalHost == "" && tailscaleHost == "" {
		logrus.Warn("Не указан адрес устройства")
		return ""
	}
	if name == "" {
		existingNames := make(map[string]bool)
		for _, conn := range cm.connections {
			existingNames[conn.Name] = true
		}
		num := 1
		for {
			candidateName := fmt.Sprintf(i18n.Current.ConnectionNumber, num)
			if !existingNames[candidateName] {
				name = candidateName
				break
			}
			num++
		}
	}

	conn := SavedConnection{
		Name:              name,
		InternalHost:      internalHost,
		TailscaleHost:     tailscaleHost,
		Token:             strings.TrimSpace(token),
		Protocol:          normalizeConnectionProtocol(protocol),
		WireGuardInvite:   wireGuardInvite,
		TailscaleRegister: tailscaleRegister,
	}
	conn.Host = fallbackText(conn.InternalHost, conn.TailscaleHost)
	cm.connections = append(cm.connections, conn)
	cm.selectedIndex = len(cm.connections) - 1
	cm.saveConnections()
	fyne.Do(func() {
		cm.refreshConnectionsList()
	})
	return name
}

// RememberResolvedTailscaleHost updates an existing saved connection with the tailnet address
// discovered during bootstrap, or creates a new one when none matches.
func (cm *ConnectionManager) RememberResolvedTailscaleHost(currentHost, internalHost, tailscaleHost, token string) {
	if cm == nil {
		return
	}

	currentHost = strings.TrimSpace(currentHost)
	internalHost = strings.TrimSpace(internalHost)
	tailscaleHost = strings.TrimSpace(tailscaleHost)
	token = strings.TrimSpace(token)
	if tailscaleHost == "" {
		return
	}

	for i := range cm.connections {
		conn := cm.connections[i]
		savedInternal, savedTailscale := classifyConnectionHosts(conn)
		if currentHost != "" && (strings.TrimSpace(conn.Host) == currentHost || savedInternal == currentHost || savedTailscale == currentHost) {
			cm.connections[i].InternalHost = fallbackText(internalHost, savedInternal)
			cm.connections[i].TailscaleHost = tailscaleHost
			if token != "" {
				cm.connections[i].Token = token
			}
			cm.connections[i].Protocol = normalizeConnectionProtocol("tailscale")
			cm.connections[i].Host = fallbackText(cm.connections[i].TailscaleHost, cm.connections[i].InternalHost)
			cm.saveConnections()
			fyne.Do(func() {
				cm.refreshConnectionsList()
			})
			logrus.Infof("Updated saved connection with resolved tailscale host=%s", tailscaleHost)
			return
		}
	}

	name := cm.SaveConnection("", internalHost, tailscaleHost, token, "tailscale", "", false)
	logrus.Infof("Saved new tailscale connection %q with host=%s", name, tailscaleHost)
}

// getStorageURI возвращает URI для хранения
func (cm *ConnectionManager) getStorageURI() fyne.URI {
	uri, err := storage.Child(cm.app.Storage().RootURI(), "connections.json")
	if err != nil {
		u, _ := url.Parse("file://connections.json")
		return storage.NewFileURI(u.String())
	}
	return uri
}

// saveConnections сохраняет подключения в файл
func (cm *ConnectionManager) saveConnections() {
	data, err := json.MarshalIndent(cm.connections, "", "  ")
	if err != nil {
		logrus.Errorf("Ошибка сериализации: %v", err)
		return
	}
	storageURI := cm.getStorageURI()
	writer, err := storage.Writer(storageURI)
	if err != nil {
		logrus.Errorf("Ошибка записи: %v", err)
		return
	}
	defer writer.Close()
	if _, err := writer.Write(data); err != nil {
		logrus.Errorf("Ошибка сохранения: %v", err)
	}
}

// loadConnections загружает подключения
func (cm *ConnectionManager) loadConnections() {
	storageURI := cm.getStorageURI()
	reader, err := storage.Reader(storageURI)
	if err != nil {
		cm.connections = make([]SavedConnection, 0)
		return
	}
	defer reader.Close()

	var data []byte
	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	if err := json.Unmarshal(data, &cm.connections); err != nil {
		cm.connections = make([]SavedConnection, 0)
		return
	}

	for i := range cm.connections {
		internalHost, tailscaleHost := classifyConnectionHosts(cm.connections[i])
		cm.connections[i].InternalHost = internalHost
		cm.connections[i].TailscaleHost = tailscaleHost
		cm.connections[i].Host = fallbackText(internalHost, tailscaleHost)
		cm.connections[i].Token = strings.TrimSpace(cm.connections[i].Token)
		cm.connections[i].Protocol = normalizeConnectionProtocol(cm.connections[i].Protocol)
	}
}
