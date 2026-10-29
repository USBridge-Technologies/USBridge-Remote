package controller

import (
	"strings"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/analytics"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
)

// usbSetupAddr is the KVM on its USB cable (usbridge's setupnet.KVMAddr).
const usbSetupAddr = "10.55.0.1"

// hostChoice is one address a KVM is reachable at, and over what.
type hostChoice struct {
	host string
	via  string
}

// newHostChoiceButton: the address in use, as a menu of the others (the
// KVM's Ethernet addresses, its USB cable's) that puts the one picked into
// lanEntry.
func newHostChoiceButton(lanEntry *view.StyledEntry, choices []hostChoice) fyne.CanvasObject {
	label := func() string {
		cur := strings.TrimSpace(lanEntry.Text)
		for _, c := range choices {
			if c.host == cur {
				return c.via + ": " + c.host + "  ▾"
			}
		}
		return i18n.Current.AddressOtherChoices + "  ▾"
	}
	var btn *connectionDialogSecondaryButton
	btn = newConnectionDialogWideActionButton(label(), nil, design.ColorConnectionAddFill, func() {
		items := make([]view.StyledMenuItem, 0, len(choices))
		for _, c := range choices {
			c := c
			items = append(items, view.StyledMenuItem{
				Label:          c.host,
				SecondaryLabel: c.via,
				Selected:       strings.TrimSpace(lanEntry.Text) == c.host,
				OnTap: func() {
					lanEntry.SetText(c.host)
					btn.SetLabel(label())
				},
			})
		}
		view.ShowStyledMenu(btn, items)
	})
	return btn
}

// usbHostChoices: the KVM's Ethernet addresses first (the default: they
// keep working once the cable is out), then the USB cable's.
func usbHostChoices(res usbSetupClaim) []hostChoice {
	var out []hostChoice
	for _, a := range res.LANAddrs {
		out = append(out, hostChoice{host: a, via: i18n.Current.AddressViaEthernet})
	}
	return append(out, hostChoice{host: usbSetupAddr, via: i18n.Current.AddressViaUSB})
}

// pendingSave is a KVM offered to connect to (over USB, or by a link):
// saved to the connections once a connection to it succeeds.
type pendingSave struct {
	name, internalHost, tailscaleHost, masterKey, protocol string
	register                                               bool
	source                                                 string
}

func (cm *ConnectionManager) hasConnectionWithKey(key string) bool {
	for _, c := range cm.connections {
		if c.MasterKey == key {
			return true
		}
	}
	return false
}

// SavePendingOnConnected saves the KVM offered by OfferKVMOverUSB or
// OfferLink once the client has connected to it with masterKey (and it
// isn't saved already). Called by the main window on every successful
// connect.
func (cm *ConnectionManager) SavePendingOnConnected(masterKey string) {
	fyne.Do(func() {
		p := cm.pendingSave
		if p == nil || p.masterKey != strings.TrimSpace(masterKey) {
			return
		}
		cm.pendingSave = nil
		if cm.hasConnectionWithKey(p.masterKey) {
			return
		}
		name := cm.SaveConnection(p.name, p.internalHost, p.tailscaleHost, p.masterKey, p.protocol, "", p.register, p.source)
		cm.refreshConnectionsList()
		logrus.Infof("Saved %q after connecting to it", name)
		if p.internalHost == usbSetupAddr {
			cm.followEthernetAddress(p.internalHost, p.masterKey)
		}
	})
}

// saveOnConnect makes the dialog's Connect remember the connection, saved
// by SavePendingOnConnected once it succeeds.
func (cm *ConnectionManager) saveOnConnect(protocol, source string) func(*connectionDialogSpec) {
	return func(spec *connectionDialogSpec) {
		connect := spec.onConnect
		spec.onConnect = func(name, internalHost, tailscaleHost, masterKey string, register bool) bool {
			if !connect(name, internalHost, tailscaleHost, masterKey, register) {
				return false
			}
			cm.pendingSave = &pendingSave{
				name: strings.TrimSpace(name), internalHost: strings.TrimSpace(internalHost),
				tailscaleHost: strings.TrimSpace(tailscaleHost), masterKey: strings.TrimSpace(masterKey),
				protocol: protocol, register: register, source: source,
			}
			return true
		}
	}
}

// OfferKVMOverUSB: the web client the KVM itself serves over its USB cable
// (http://10.55.0.1:8080/) asks that KVM for its key and offers it in the
// Add Connection dialog -- its Ethernet address by default, the cable's in
// the menu -- unless it's among the connections already. Connect saves it
// once connected.
func (cm *ConnectionManager) OfferKVMOverUSB() {
	go func() {
		res, err := claimKVMOverUSB()
		if err != nil {
			logrus.Infof("Over USB: no key offered (%v)", err)
			return
		}
		fyne.Do(func() {
			if cm.hasConnectionWithKey(res.MasterKey) {
				return
			}
			choices := usbHostChoices(res)
			name := res.Hostname
			if name == "" {
				name = "USBridge KVM"
			}
			cm.showPrefilledAddDialog(name, choices[0].host, "", res.MasterKey, "", false, false,
				cm.saveOnConnect("", analytics.SourceUSB),
				func(spec *connectionDialogSpec) {
					spec.feedbackText = "✓ " + i18n.Current.AddOverUSBDone
					spec.internalHostChoices = choices
				})
		})
	}()
}

// OfferLink shows a usbridge://connect link (the USB setup page's "Open
// the web client", a QR code's link) in the Add Connection dialog; Connect
// saves it once connected.
func (cm *ConnectionManager) OfferLink(internalHost, tailscaleHost, masterKey, protocol string) {
	fyne.Do(func() {
		cm.showPrefilledAddDialog("", internalHost, tailscaleHost, masterKey, protocol, false, false,
			cm.saveOnConnect(protocol, analytics.SourceLink))
	})
}
