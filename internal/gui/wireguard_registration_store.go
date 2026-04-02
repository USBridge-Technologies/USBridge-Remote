package gui

import (
	"encoding/json"
	"strings"

	"usbridge-client/internal/models"
)

const wireGuardRegistrationsPrefKey = "wireguard.registrations.v1"

type wireGuardRegistration struct {
	Host       string                            `json:"host"`
	PrivateKey string                            `json:"private_key"`
	Bootstrap  models.WireGuardBootstrapResponse `json:"bootstrap"`
}

func normalizeWireGuardRegistrationHost(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
}

func (mw *MainWindow) loadWireGuardRegistrations() map[string]wireGuardRegistration {
	raw := mw.app.Preferences().StringWithFallback(wireGuardRegistrationsPrefKey, "")
	if strings.TrimSpace(raw) == "" {
		return map[string]wireGuardRegistration{}
	}

	var registrations map[string]wireGuardRegistration
	if err := json.Unmarshal([]byte(raw), &registrations); err != nil {
		return map[string]wireGuardRegistration{}
	}
	if registrations == nil {
		return map[string]wireGuardRegistration{}
	}
	return registrations
}

func (mw *MainWindow) saveWireGuardRegistrations(registrations map[string]wireGuardRegistration) {
	if registrations == nil {
		registrations = map[string]wireGuardRegistration{}
	}
	data, err := json.Marshal(registrations)
	if err != nil {
		return
	}
	mw.app.Preferences().SetString(wireGuardRegistrationsPrefKey, string(data))
}

func (mw *MainWindow) getWireGuardRegistration(host string) (wireGuardRegistration, bool) {
	key := normalizeWireGuardRegistrationHost(host)
	if key == "" {
		return wireGuardRegistration{}, false
	}

	reg, ok := mw.loadWireGuardRegistrations()[key]
	if !ok {
		return wireGuardRegistration{}, false
	}
	if !isValidWireGuardRegistration(reg) {
		mw.deleteWireGuardRegistration(host)
		return wireGuardRegistration{}, false
	}
	return reg, true
}

func (mw *MainWindow) storeWireGuardRegistration(host, privateKey string, bootstrap *models.WireGuardBootstrapResponse) {
	key := normalizeWireGuardRegistrationHost(host)
	if key == "" || bootstrap == nil {
		return
	}

	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" {
		privateKey = strings.TrimSpace(bootstrap.ClientPrivateKey)
	}
	if privateKey == "" {
		return
	}

	record := wireGuardRegistration{
		Host:       key,
		PrivateKey: privateKey,
		Bootstrap:  *bootstrap,
	}
	record.Bootstrap.ClientPrivateKey = ""
	if !isValidWireGuardRegistration(record) {
		return
	}

	registrations := mw.loadWireGuardRegistrations()
	registrations[key] = record
	mw.saveWireGuardRegistrations(registrations)
}

func (mw *MainWindow) deleteWireGuardRegistration(host string) {
	key := normalizeWireGuardRegistrationHost(host)
	if key == "" {
		return
	}
	registrations := mw.loadWireGuardRegistrations()
	if _, ok := registrations[key]; !ok {
		return
	}
	delete(registrations, key)
	mw.saveWireGuardRegistrations(registrations)
}

func isValidWireGuardRegistration(reg wireGuardRegistration) bool {
	if strings.TrimSpace(reg.PrivateKey) == "" {
		return false
	}
	bootstrap := reg.Bootstrap
	if strings.TrimSpace(bootstrap.ServerPublicKey) == "" {
		return false
	}
	if strings.TrimSpace(bootstrap.ServerEndpointHost) == "" || bootstrap.ServerEndpointPort <= 0 {
		return false
	}
	if strings.TrimSpace(bootstrap.ServerAddress) == "" || strings.TrimSpace(bootstrap.ClientAddress) == "" {
		return false
	}
	return true
}
