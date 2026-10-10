package gui

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
)

// KVM settings (gear menu): the KVM's Ethernet and Wi-Fi addresses and its
// event log -- what the on-device menu has, for boards without a screen
// (a NanoKVM, or any board added over the USB cable). Talks to the KVM's
// /api/network/* and /api/events (usbridge web/network_api.go).

var kvmSettingsOpen atomic.Bool

const kvmSettingsWidth = 460

func (mw *MainWindow) showKVMSettingsDialog() {
	client := mw.usbClient
	if client == nil || !kvmSettingsOpen.CompareAndSwap(false, true) {
		return
	}
	done := make(chan struct{})
	closeFn := func() {
		close(done)
		kvmSettingsOpen.Store(false)
	}

	eth := newKVMIPForm()
	wifiIP := newKVMIPForm()
	ethStatus := widget.NewLabel("…")
	ethStatus.Wrapping = fyne.TextWrapWord
	wifiStatus := widget.NewLabel("…")
	wifiStatus.Wrapping = fyne.TextWrapWord
	ethMsg := widget.NewLabel("")
	ethMsg.Wrapping = fyne.TextWrapWord
	wifiMsg := widget.NewLabel("")
	wifiMsg.Wrapping = fyne.TextWrapWord

	// Ethernet.
	ethApply := view.NewDialogApplyButton(i18n.Current.KVMNetApply, func() {
		cfg, err := eth.config()
		if err != nil {
			ethMsg.SetText(err.Error())
			return
		}
		ethMsg.SetText("…")
		go func() {
			err := client.SetEthernetIP(cfg)
			fyne.Do(func() { ethMsg.SetText(kvmResultText(err)) })
		}()
	})
	ethForm := container.NewVBox(
		ethStatus,
		view.NewDialogVSpace(6),
		eth.object(),
		view.NewDialogVSpace(8),
		container.NewHBox(ethApply),
		ethMsg,
	)
	ethNone := view.NewDialogHint(i18n.Current.KVMNetNoEthernet, 0)
	ethPage := container.NewStack(ethForm, ethNone)
	ethNone.Hide()

	// Wi-Fi.
	var networks []models.WiFiNetwork
	netPick := view.NewDialogPicker(nil, "", nil)
	password := view.NewStyledEntry()
	password.Password = true
	password.SetPlaceHolder(i18n.Current.KVMWiFiPassword)
	scanLabel := i18n.Current.KVMWiFiScan
	var scanBtn fyne.CanvasObject
	scanning := false
	scanBtn = view.NewDialogCancelButton(scanLabel, func() {
		if scanning {
			return
		}
		scanning = true
		wifiMsg.SetText(i18n.Current.KVMWiFiScanning)
		go func() {
			found, err := client.ScanWiFi()
			fyne.Do(func() {
				scanning = false
				if err != nil {
					wifiMsg.SetText(err.Error())
					return
				}
				networks = found
				var opts []string
				for _, n := range found {
					opts = append(opts, kvmWiFiLabel(n))
				}
				netPick.SetOptions(opts)
				if len(opts) > 0 {
					netPick.SetSelected(opts[0])
					wifiMsg.SetText("")
				} else {
					netPick.SetSelected("")
					wifiMsg.SetText(i18n.Current.KVMWiFiNoNetworks)
				}
			})
		}()
	})
	pickedSSID := func() string {
		for _, n := range networks {
			if kvmWiFiLabel(n) == netPick.Selected {
				return n.SSID
			}
		}
		return ""
	}
	connectBtn := view.NewDialogApplyButton(i18n.Current.KVMWiFiConnect, func() {
		ssid := pickedSSID()
		if ssid == "" {
			wifiMsg.SetText(i18n.Current.KVMWiFiPickNetwork)
			return
		}
		pass := password.Text
		wifiMsg.SetText(i18n.Current.KVMNetConnecting)
		go func() {
			err := client.ConnectWiFi(ssid, pass)
			fyne.Do(func() {
				if err != nil {
					wifiMsg.SetText(err.Error())
				}
			})
		}()
	})
	disconnectBtn := view.NewDialogCancelButton(i18n.Current.KVMWiFiDisconnect, func() {
		go func() {
			err := client.DisconnectWiFi(false)
			fyne.Do(func() { wifiMsg.SetText(kvmResultText(err)) })
		}()
	})
	forgetBtn := view.NewDialogCancelButton(i18n.Current.KVMWiFiForget, func() {
		go func() {
			err := client.DisconnectWiFi(true)
			fyne.Do(func() { wifiMsg.SetText(kvmResultText(err)) })
		}()
	})
	wifiApply := view.NewDialogApplyButton(i18n.Current.KVMNetApply, func() {
		cfg, err := wifiIP.config()
		if err != nil {
			wifiMsg.SetText(err.Error())
			return
		}
		go func() {
			err := client.SetWiFiIP(cfg)
			fyne.Do(func() { wifiMsg.SetText(kvmResultText(err)) })
		}()
	})
	wifiForm := container.NewVBox(
		wifiStatus,
		view.NewDialogVSpace(6),
		view.NewDialogField(i18n.Current.KVMWiFiNetwork, container.NewBorder(nil, nil, nil, scanBtn, netPick)),
		view.NewDialogVSpace(4),
		view.NewDialogField(i18n.Current.KVMWiFiPassword, password),
		view.NewDialogVSpace(8),
		container.NewHBox(connectBtn, disconnectBtn, forgetBtn),
		view.NewDialogVSpace(10),
		wifiIP.object(),
		view.NewDialogVSpace(8),
		container.NewHBox(wifiApply),
		wifiMsg,
	)
	wifiNone := view.NewDialogHint(i18n.Current.KVMNetNoWiFi, 0)
	wifiPage := container.NewStack(wifiForm, wifiNone)
	wifiNone.Hide()

	// Event log.
	var events []models.KVMEvent
	detail := widget.NewLabel("")
	detail.Wrapping = fyne.TextWrapWord
	list := widget.NewList(
		func() int { return len(events) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < len(events) {
				e := events[i]
				o.(*widget.Label).SetText(fmt.Sprintf("%s  %-8s %s", e.Timestamp.Local().Format("01-02 15:04:05"), e.Type, e.ShortDesc))
			}
		},
	)
	list.OnSelected = func(i widget.ListItemID) {
		if i < len(events) {
			e := events[i]
			text := e.Timestamp.Local().Format("2006-01-02 15:04:05") + "  " + e.Type + "\n" + firstNonBlank(e.DetailDesc, e.ShortDesc)
			if strings.TrimSpace(e.ExtraData) != "" {
				text += "\n" + e.ExtraData
			}
			detail.SetText(text)
		}
	}
	listBox := canvas.NewRectangle(nil)
	listBox.SetMinSize(fyne.NewSize(kvmSettingsWidth-40, 260))
	eventsMsg := widget.NewLabel("")
	loadEvents := func() {
		eventsMsg.SetText("…")
		go func() {
			ev, err := client.GetKVMEvents(500)
			fyne.Do(func() {
				if err != nil {
					eventsMsg.SetText(err.Error())
					return
				}
				events = ev
				list.UnselectAll()
				list.Refresh()
				detail.SetText("")
				if len(ev) == 0 {
					eventsMsg.SetText(i18n.Current.KVMEventsEmpty)
				} else {
					eventsMsg.SetText(fmt.Sprintf("%d", len(ev)))
				}
			})
		}()
	}
	eventsPage := container.NewVBox(
		container.NewBorder(nil, nil, eventsMsg, view.NewDialogCancelButton(i18n.Current.KVMEventsRefresh, loadEvents)),
		container.NewStack(listBox, list),
		detail,
	)

	powerPage, loadPower := kvmPowerPage(client)
	updatesPage, loadUpdates := kvmUpdatesPage(client)
	sdPage, loadSD := kvmSDPage(client, mw.window)

	// Section switch. loaders run when a section is shown; pollers also
	// on every refresh tick while it's the one shown.
	sections := []string{i18n.Current.KVMNetEthernet, i18n.Current.KVMNetWiFi, i18n.Current.KVMPower,
		i18n.Current.KVMUpdates, i18n.Current.KVMSDCard, i18n.Current.KVMEventLog}
	pages := []fyne.CanvasObject{ethPage, wifiPage, powerPage, updatesPage, sdPage, eventsPage}
	loaders := []func(){nil, nil, loadPower, loadUpdates, loadSD, loadEvents}
	pollers := []func(){nil, nil, nil, loadUpdates, loadSD, nil}
	var shown atomic.Int32
	show := func(name string) {
		for i, s := range sections {
			if s == name {
				pages[i].Show()
				shown.Store(int32(i))
				if loaders[i] != nil {
					loaders[i]()
				}
			} else {
				pages[i].Hide()
			}
		}
	}
	sectionPick := view.NewDialogPicker(sections, sections[0], show)
	show(sections[0])

	// Status, refreshed while the dialog is open. The forms are filled
	// from the KVM's saved settings once, not on every refresh (that
	// would overwrite what's being typed).
	formsFilled := false
	applyStatus := func(st *models.NetworkStatus) {
		if st.Ethernet.Available {
			ethForm.Show()
			ethNone.Hide()
			ethStatus.SetText(kvmEthernetStatusText(st.Ethernet))
		} else {
			ethForm.Hide()
			ethNone.Show()
		}
		if st.WiFi.Available {
			wifiForm.Show()
			wifiNone.Hide()
			wifiStatus.SetText(kvmWiFiStatusText(st.WiFi))
			if st.WiFi.LastError != "" && !st.WiFi.Connecting && wifiMsg.Text == i18n.Current.KVMNetConnecting {
				wifiMsg.SetText(st.WiFi.LastError)
			} else if st.WiFi.Connected && wifiMsg.Text == i18n.Current.KVMNetConnecting {
				wifiMsg.SetText(i18n.Current.KVMNetConnected + ": " + st.WiFi.SSID)
			}
		} else {
			wifiForm.Hide()
			wifiNone.Show()
		}
		if !formsFilled {
			formsFilled = true
			eth.set(st.Ethernet.Config)
			wifiIP.set(st.WiFi.Config)
		}
	}
	refresh := func() {
		st, err := client.GetNetworkStatus()
		fyne.Do(func() {
			if err != nil {
				ethStatus.SetText(err.Error())
				wifiStatus.SetText(err.Error())
				return
			}
			applyStatus(st)
		})
	}
	go func() {
		refresh()
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				refresh()
				i := shown.Load()
				fyne.Do(func() {
					if p := pollers[i]; p != nil {
						p()
					}
				})
			}
		}
	}()

	body := container.NewVBox(
		view.NewDialogField(i18n.Current.KVMNetSection, sectionPick),
		view.NewDialogVSpace(10),
		container.NewStack(pages...),
	)
	view.ShowBrandFormDialog(view.BrandFormDialogSpec{
		Parent:     mw.window,
		Title:      i18n.Current.MenuKVMSettings,
		Body:       container.NewVScroll(body),
		CancelText: i18n.Current.Close,
		ApplyText:  i18n.Current.Close,
		OnCancel:   closeFn,
		OnApply:    closeFn,
		MinWidth:   kvmSettingsWidth,
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			w := minF(kvmSettingsWidth+40, canvasSize.Width-24)
			h := minF(640, canvasSize.Height-24)
			return fyne.NewSize(w, h)
		},
	})
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func firstNonBlank(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func kvmResultText(err error) string {
	if err != nil {
		return err.Error()
	}
	return i18n.Current.KVMNetApplied
}

func kvmWiFiLabel(n models.WiFiNetwork) string {
	label := fmt.Sprintf("%s  (%d%%", n.SSID, n.Signal)
	if n.Security != "" {
		label += ", " + n.Security
	}
	return label + ")"
}

func kvmEthernetStatusText(e models.EthernetStatus) string {
	state := i18n.Current.KVMNetNoCable
	if e.Connected {
		state = i18n.Current.KVMNetConnected
		if e.Speed != "" {
			state += " · " + e.Speed
		}
	}
	lines := []string{e.Interface + ": " + state}
	if e.IP != "" {
		lines = append(lines, "IP "+e.IP)
	}
	if e.Gateway != "" {
		lines = append(lines, i18n.Current.KVMNetGateway+" "+e.Gateway)
	}
	if e.MAC != "" {
		lines = append(lines, "MAC "+e.MAC)
	}
	return strings.Join(lines, "\n")
}

func kvmWiFiStatusText(w models.WiFiStatus) string {
	state := i18n.Current.KVMNetNotConnected
	switch {
	case w.Connecting:
		state = i18n.Current.KVMNetConnecting
	case w.Connected:
		state = fmt.Sprintf("%s: %s (%d%%)", i18n.Current.KVMNetConnected, w.SSID, w.Signal)
	}
	lines := []string{w.Interface + ": " + state}
	if w.IP != "" {
		lines = append(lines, "IP "+w.IP)
	}
	return strings.Join(lines, "\n")
}

// kvmIPForm is the DHCP / static address form (Ethernet and Wi-Fi).
type kvmIPForm struct {
	mode                      *view.HeaderDropdown
	ip, netmask, gateway, dns *view.StyledEntry
	staticFields              *fyne.Container
}

func newKVMIPForm() *kvmIPForm {
	f := &kvmIPForm{
		ip:      view.NewStyledEntry(),
		netmask: view.NewStyledEntry(),
		gateway: view.NewStyledEntry(),
		dns:     view.NewStyledEntry(),
	}
	f.ip.SetPlaceHolder("192.168.1.100")
	f.netmask.SetPlaceHolder("255.255.255.0")
	f.gateway.SetPlaceHolder("192.168.1.1")
	f.dns.SetPlaceHolder("1.1.1.1")
	f.staticFields = container.NewVBox(
		view.NewDialogField(i18n.Current.KVMNetIP, f.ip),
		view.NewDialogField(i18n.Current.KVMNetNetmask, f.netmask),
		view.NewDialogField(i18n.Current.KVMNetGateway, f.gateway),
		view.NewDialogField(i18n.Current.KVMNetDNS, f.dns),
	)
	f.staticFields.Hide()
	f.mode = view.NewDialogPicker(
		[]string{i18n.Current.KVMNetDHCP, i18n.Current.KVMNetStatic},
		i18n.Current.KVMNetDHCP,
		func(v string) {
			if v == i18n.Current.KVMNetStatic {
				f.staticFields.Show()
			} else {
				f.staticFields.Hide()
			}
		},
	)
	return f
}

func (f *kvmIPForm) object() fyne.CanvasObject {
	return container.NewVBox(view.NewDialogField(i18n.Current.KVMNetAddressMode, f.mode), f.staticFields)
}

func (f *kvmIPForm) set(c models.NetIPConfig) {
	f.ip.SetText(c.IP)
	f.netmask.SetText(c.Netmask)
	f.gateway.SetText(c.Gateway)
	f.dns.SetText(c.DNS)
	if c.Mode == "static" {
		f.mode.SetSelected(i18n.Current.KVMNetStatic)
		f.staticFields.Show()
	} else {
		f.mode.SetSelected(i18n.Current.KVMNetDHCP)
		f.staticFields.Hide()
	}
}

func (f *kvmIPForm) config() (models.NetIPConfig, error) {
	if f.mode.Selected != i18n.Current.KVMNetStatic {
		return models.NetIPConfig{Mode: "dhcp"}, nil
	}
	c := models.NetIPConfig{
		Mode:    "static",
		IP:      strings.TrimSpace(f.ip.Text),
		Netmask: strings.TrimSpace(f.netmask.Text),
		Gateway: strings.TrimSpace(f.gateway.Text),
		DNS:     strings.TrimSpace(f.dns.Text),
	}
	if c.IP == "" {
		return c, fmt.Errorf("%s?", i18n.Current.KVMNetIP)
	}
	return c, nil
}
