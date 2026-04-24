package controller

import (
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strings"
	"sync/atomic"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"github.com/sirupsen/logrus"
)

type QRScanner struct {
	app    fyne.App
	window fyne.Window

	onConnect func(host, token, protocol, wireGuardInvite string, quicPort int, tailscaleRegister bool)
	onSave    func(name, internalHost, tailscaleHost, token, protocol, wireGuardInvite string, quicPort int, tailscaleRegister bool)
	onPrefill func(internalHost, tailscaleHost, token, protocol, wireGuardInvite string, quicPort int, scanned bool)

	scanSession       atomic.Uint64
	scanResultHandled atomic.Bool
}

func NewQRScanner(
	app fyne.App,
	onConnect func(host, token, protocol, wireGuardInvite string, quicPort int, tailscaleRegister bool),
	onSave func(name, internalHost, tailscaleHost, token, protocol, wireGuardInvite string, quicPort int, tailscaleRegister bool),
	onPrefill func(internalHost, tailscaleHost, token, protocol, wireGuardInvite string, quicPort int, scanned bool),
) *QRScanner {
	return &QRScanner{
		app:       app,
		onConnect: onConnect,
		onSave:    onSave,
		onPrefill: onPrefill,
	}
}

func (qs *QRScanner) ShowCameraScanner(parent fyne.Window) {
	qs.window = parent
	qs.ShowCameraScannerNative(parent)
}

func (qs *QRScanner) beginScanSession() uint64 {
	session := qs.scanSession.Add(1)
	qs.scanResultHandled.Store(false)
	return session
}

func (qs *QRScanner) isScanSessionActive(session uint64) bool {
	return qs.scanSession.Load() == session
}

func (qs *QRScanner) tryHandleScanResult(session uint64) bool {
	if !qs.isScanSessionActive(session) {
		return false
	}
	return qs.scanResultHandled.CompareAndSwap(false, true)
}

func (qs *QRScanner) scanQRCode(img image.Image, parent fyne.Window) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorProcessingImage, err), parent)
		logrus.Errorf("Error creating bitmap: %v", err)
		return
	}

	qrReader := qrcode.NewQRCodeReader()
	result, err := qrReader.Decode(bmp, nil)
	if err != nil {
		view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.QRCodeNotFound), parent)
		logrus.Errorf("Error decoding QR: %v", err)
		return
	}

	qrText := result.GetText()
	logrus.Infof("QR code scanned: %s", qrText)
	qs.parseAndApply(qrText, parent)
}

func (qs *QRScanner) parseAndApply(qrText string, parent fyne.Window) {
	internalHost, tailscaleHost, token, protocol, wireGuardInvite, quicPort, err := parseQRContents(qrText)
	if err != nil {
		view.ShowErrorDialog(errors.New(fmt.Sprintf(i18n.Current.InvalidQRFormat, qrText)), parent)
		return
	}

	host := resolveScannedHost(protocol, internalHost, tailscaleHost)
	if host == "" {
		view.ShowErrorDialog(errors.New(i18n.Current.HostCannotBeEmpty), parent)
		return
	}

	fyne.Do(func() {
		if qs.onPrefill != nil {
			logrus.Infof("Opening prefilled connection dialog from QR: internal=%s tailscale=%s quicPort=%d", internalHost, tailscaleHost, quicPort)
			qs.onPrefill(internalHost, tailscaleHost, token, protocol, wireGuardInvite, quicPort, true)
			return
		}

		qs.showPreview(internalHost, tailscaleHost, token, protocol, wireGuardInvite, quicPort, parent)
	})
}

func parseQRContents(qrText string) (internalHost, tailscaleHost, token, protocol, wireGuardInvite string, quicPort int, err error) {
	qrText = strings.TrimSpace(qrText)
	if qrText == "" {
		return "", "", "", "", "", 0, fmt.Errorf("empty QR code")
	}

	if strings.HasPrefix(qrText, "usbridge://") {
		u, parseErr := url.Parse(qrText)
		if parseErr != nil {
			return "", "", "", "", "", 0, parseErr
		}
		if u.Scheme != "usbridge" || u.Host != "connect" {
			return "", "", "", "", "", 0, fmt.Errorf("unsupported deep link format")
		}

		query := u.Query()
		internalHost = strings.TrimSpace(query.Get("internal_host"))
		tailscaleHost = strings.TrimSpace(query.Get("tailscale_host"))
		host := strings.TrimSpace(query.Get("host"))
		if internalHost == "" && tailscaleHost == "" {
			if isLikelyTailnetHost(host) {
				tailscaleHost = host
			} else {
				internalHost = host
			}
		}
		token = strings.TrimSpace(query.Get("token"))
		protocol = strings.TrimSpace(query.Get("protocol"))
		wireGuardInvite = strings.TrimSpace(query.Get("wireguard_invite"))

		if query.Get("quic_port") != "" {
			fmt.Sscanf(query.Get("quic_port"), "%d", &quicPort)
		}

		if (internalHost != "" || tailscaleHost != "") && (token != "" || wireGuardInvite != "") {
			return internalHost, tailscaleHost, token, protocol, wireGuardInvite, quicPort, nil
		}
		return "", "", "", "", "", 0, fmt.Errorf("host or auth data is missing in the link")
	}

	parts := strings.SplitN(qrText, ":", 2)
	if len(parts) != 2 {
		return "", "", "", "", "", 0, fmt.Errorf("expected format host:token or usbridge://connect?host=X&token=Y")
	}

	host := strings.TrimSpace(parts[0])
	token = strings.TrimSpace(parts[1])
	if isLikelyTailnetHost(host) {
		tailscaleHost = host
	} else {
		internalHost = host
	}
	return internalHost, tailscaleHost, token, "", "", 0, nil
}

func (qs *QRScanner) showPreview(internalHost, tailscaleHost, token, protocol, wireGuardInvite string, quicPort int, parent fyne.Window) {
	host := resolveScannedHost(protocol, internalHost, tailscaleHost)

	tokenLabel := widget.NewLabel(i18n.Current.TokenLabel)
	tokenValue := widget.NewEntry()
	tokenValue.SetText(token)
	tokenValue.Disable()

	infoLabel := widget.NewLabel(i18n.Current.ScanSuccess)
	infoLabel.Wrapping = fyne.TextWrapWord

	var d dialog.Dialog

	saveBtn := widget.NewButton(i18n.Current.DeepLinkSave, func() {
		if d != nil {
			d.Hide()
		}
		if qs.onSave != nil {
			qs.onSave(host, internalHost, tailscaleHost, token, protocol, wireGuardInvite, quicPort, false)
			logrus.Infof("QR saved: internal=%s tailscale=%s quicPort=%d", internalHost, tailscaleHost, quicPort)
		}
	})
	saveBtn.Importance = widget.MediumImportance

	connectBtn := widget.NewButton(i18n.Current.DeepLinkConnect, func() {
		if d != nil {
			d.Hide()
		}
		if qs.onConnect != nil {
			qs.onConnect(host, token, protocol, wireGuardInvite, quicPort, false)
			logrus.Infof("QR connect: host=%s quicPort=%d", host, quicPort)
		}
	})
	connectBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton(i18n.Current.Cancel, func() {
		if d != nil {
			d.Hide()
		}
	})

	buttons := container.NewGridWithColumns(3, connectBtn, saveBtn, cancelBtn)
	content := container.NewBorder(
		container.NewVBox(
			infoLabel,
			widget.NewSeparator(),
			widget.NewLabel("Internal Address"),
			disabledPreviewEntry(internalHost),
			widget.NewLabel("Tailscale Address"),
			disabledPreviewEntry(tailscaleHost),
			tokenLabel,
			tokenValue,
		),
		buttons,
		nil, nil, nil,
	)

	d = dialog.NewCustomWithoutButtons(i18n.Current.QRCodeScanned, content, parent)
	d.Resize(fyne.NewSize(450, 320))
	d.Show()
}

func disabledPreviewEntry(value string) *widget.Entry {
	entry := widget.NewEntry()
	entry.SetText(strings.TrimSpace(value))
	entry.Disable()
	return entry
}

func resolveScannedHost(protocol, internalHost, tailscaleHost string) string {
	if strings.TrimSpace(protocol) == "tailscale" && strings.TrimSpace(tailscaleHost) != "" {
		return strings.TrimSpace(tailscaleHost)
	}
	if strings.TrimSpace(protocol) == "quic" && strings.TrimSpace(internalHost) != "" {
		return strings.TrimSpace(internalHost)
	}
	if strings.TrimSpace(tailscaleHost) != "" {
		return strings.TrimSpace(tailscaleHost)
	}
	return strings.TrimSpace(internalHost)
}

func ShowTestQRCode(parent fyne.Window) {
	qrText := "192.168.88.244:supersecret"

	info := widget.NewLabel(fmt.Sprintf(i18n.Current.QRExampleText, qrText))
	info.Wrapping = fyne.TextWrapWord

	copyBtn := widget.NewButton(i18n.Current.CopyText, func() {
		parent.Clipboard().SetContent(qrText)
		dialog.ShowInformation(i18n.Current.Done, i18n.Current.TextCopiedToClipboard, parent)
	})

	content := container.NewVBox(info, copyBtn)
	dialog.NewCustom(i18n.Current.TestQRCode, i18n.Current.Close, content, parent).Show()
}

func CreateQRPreview(qrText string) fyne.CanvasObject {
	label := widget.NewLabel(fmt.Sprintf(i18n.Current.QRCodeLabel, qrText))
	label.Wrapping = fyne.TextWrapWord

	return container.NewVBox(
		widget.NewLabelWithStyle(i18n.Current.QRCodeForConnection, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		label,
	)
}
