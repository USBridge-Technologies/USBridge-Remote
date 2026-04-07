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
func (cm *ConnectionManager) SaveConnection(name, host, token, protocol, wireGuardInvite string) string {
	if host == "" {
		logrus.Warn("Не указан IP адрес")
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

	conn := SavedConnection{Name: name, Host: host, Token: token, Protocol: protocol, WireGuardInvite: wireGuardInvite}
	cm.connections = append(cm.connections, conn)
	cm.selectedIndex = len(cm.connections) - 1
	cm.saveConnections()
	fyne.Do(func() {
		cm.refreshConnectionsList()
	})
	return name
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
		cm.connections[i].Host = strings.TrimSpace(cm.connections[i].Host)
		cm.connections[i].Token = strings.TrimSpace(cm.connections[i].Token)
		cm.connections[i].Protocol = normalizeConnectionProtocol(cm.connections[i].Protocol)
	}
}
