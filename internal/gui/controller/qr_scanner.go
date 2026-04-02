package controller

import (
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strings"

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

	onConnect func(host, token, protocol, wireGuardInvite string)
	onSave    func(name, host, token, protocol, wireGuardInvite string)
	onPrefill func(host, token, protocol, wireGuardInvite string)
}

func NewQRScanner(
	app fyne.App,
	onConnect func(host, token, protocol, wireGuardInvite string),
	onSave func(name, host, token, protocol, wireGuardInvite string),
	onPrefill func(host, token, protocol, wireGuardInvite string),
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
		view.ShowErrorDialog(fmt.Errorf(i18n.Current.QRCodeNotFound), parent)
		logrus.Errorf("Error decoding QR: %v", err)
		return
	}

	qrText := result.GetText()
	logrus.Infof("QR code scanned: %s", qrText)
	qs.parseAndApply(qrText, parent)
}

func (qs *QRScanner) parseAndApply(qrText string, parent fyne.Window) {
	host, token, protocol, wireGuardInvite, err := parseQRContents(qrText)
	if err != nil {
		view.ShowErrorDialog(errors.New(fmt.Sprintf(i18n.Current.InvalidQRFormat, qrText)), parent)
		return
	}

	if host == "" {
		view.ShowErrorDialog(errors.New(i18n.Current.HostCannotBeEmpty), parent)
		return
	}

	fyne.Do(func() {
		if qs.onPrefill != nil {
			logrus.Infof("Opening prefilled connection dialog from QR: host=%s", host)
			qs.onPrefill(host, token, protocol, wireGuardInvite)
			return
		}

		qs.showPreview(host, token, protocol, wireGuardInvite, parent)
	})
}

func parseQRContents(qrText string) (host, token, protocol, wireGuardInvite string, err error) {
	qrText = strings.TrimSpace(qrText)
	if qrText == "" {
		return "", "", "", "", fmt.Errorf("empty QR code")
	}

	if strings.HasPrefix(qrText, "usbridge://") {
		u, parseErr := url.Parse(qrText)
		if parseErr != nil {
			return "", "", "", "", parseErr
		}
		if u.Scheme != "usbridge" || u.Host != "connect" {
			return "", "", "", "", fmt.Errorf("unsupported deep link format")
		}

		query := u.Query()
		host = strings.TrimSpace(query.Get("host"))
		token = strings.TrimSpace(query.Get("token"))
		protocol = strings.TrimSpace(query.Get("protocol"))
		wireGuardInvite = strings.TrimSpace(query.Get("wireguard_invite"))
		if host != "" && (token != "" || wireGuardInvite != "") {
			return host, token, protocol, wireGuardInvite, nil
		}
		return "", "", "", "", fmt.Errorf("host or auth data is missing in the link")
	}

	parts := strings.SplitN(qrText, ":", 2)
	if len(parts) != 2 {
		return "", "", "", "", fmt.Errorf("expected format host:token or usbridge://connect?host=X&token=Y")
	}

	host = strings.TrimSpace(parts[0])
	token = strings.TrimSpace(parts[1])
	return host, token, "", "", nil
}

func (qs *QRScanner) showPreview(host, token, protocol, wireGuardInvite string, parent fyne.Window) {
	hostLabel := widget.NewLabel(i18n.Current.ServerAddressLabel)
	hostValue := widget.NewLabel(host)
	hostValue.TextStyle = fyne.TextStyle{Bold: true}

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
			qs.onSave(host, host, token, protocol, wireGuardInvite)
			logrus.Infof("QR saved: host=%s", host)
		}
	})
	saveBtn.Importance = widget.MediumImportance

	connectBtn := widget.NewButton(i18n.Current.DeepLinkConnect, func() {
		if d != nil {
			d.Hide()
		}
		if qs.onConnect != nil {
			qs.onConnect(host, token, protocol, wireGuardInvite)
			logrus.Infof("QR connect: host=%s", host)
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
			hostLabel,
			hostValue,
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
