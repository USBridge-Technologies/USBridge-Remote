package controller

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/api"
)

// followEthernetAddress: a KVM set up over its USB cable may not have its
// Ethernet address yet (DHCP is still answering, or the cable goes in later).
// The connection was saved with the USB setup address; the KVM's own address
// is what it should keep. Asks the KVM for its network status for a few
// minutes and, when Ethernet has an address, replaces the host with it.
func (cm *ConnectionManager) followEthernetAddress(setupHost, masterKey string) {
	if setupHost == "" || masterKey == "" {
		return
	}
	go func() {
		client := api.NewUSBClient(setupHost, 8080, 5)
		client.SetAPISecretV2([]byte(masterKey))
		for deadline := time.Now().Add(5 * time.Minute); time.Now().Before(deadline); time.Sleep(5 * time.Second) {
			st, err := client.GetNetworkStatus()
			if err != nil || st == nil || !st.Ethernet.Connected || st.Ethernet.IP == "" ||
				strings.HasPrefix(st.Ethernet.IP, "10.55.0.") {
				continue
			}
			ip := st.Ethernet.IP
			fyne.Do(func() {
				for i, c := range cm.connections {
					if c.InternalHost != setupHost || c.MasterKey != masterKey {
						continue
					}
					c.InternalHost = ip
					c.Host = fallbackText(c.InternalHost, c.TailscaleHost)
					cm.commitConnectionEdit(i, c)
					cm.refreshConnectionsList()
					logrus.Infof("Over USB: the KVM's Ethernet address %s replaces the setup address %s", ip, setupHost)
					return
				}
			})
			return
		}
	}()
}
