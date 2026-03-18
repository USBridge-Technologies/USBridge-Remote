package ui

import (
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strings"

	"usbridge-client/internal/ui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"github.com/sirupsen/logrus"
)

// QRScanner структура для сканирования QR-кодов
type QRScanner struct {
	app    fyne.App
	window fyne.Window

	// Callbacks
	onConnect func(host, token string)       // Подключиться
	onSave    func(name, host, token string) // Сохранить (name = host если пусто)
}

// NewQRScanner создает новый сканер QR-кодов
func NewQRScanner(app fyne.App, onConnect func(host, token string), onSave func(name, host, token string)) *QRScanner {
	return &QRScanner{
		app:       app,
		onConnect: onConnect,
		onSave:    onSave,
	}
}

// ShowCameraScanner показывает интерфейс сканирования с камеры
// Использует платформо-специфичную реализацию (см. qr_scanner_android.go и qr_scanner_desktop.go)
func (qs *QRScanner) ShowCameraScanner(parent fyne.Window) {
	qs.window = parent

	// Вызываем платформо-специфичную реализацию
	qs.ShowCameraScannerNative(parent)
}

// scanQRCode сканирует QR-код из изображения
// Вызывается только из main thread (fyne.Do или dialog callback)
func (qs *QRScanner) scanQRCode(img image.Image, parent fyne.Window) {
	// Конвертируем изображение в формат для gozxing
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.ErrorProcessingImage, err), parent)
		logrus.Errorf("Error creating bitmap: %v", err)
		return
	}

	// Создаем QR-декодер
	qrReader := qrcode.NewQRCodeReader()

	// Decode QR code
	result, err := qrReader.Decode(bmp, nil)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.QRCodeNotFound), parent)
		logrus.Errorf("Error decoding QR: %v", err)
		return
	}

	// Получаем текст из QR-кода
	qrText := result.GetText()
	logrus.Infof("QR code scanned: %s", qrText)

	// Парсим данные в формате host:token
	qs.parseAndApply(qrText, parent)
}

// parseAndApply парсит QR и показывает диалог
// Поддерживает форматы: "host:token" и "usbridge://connect?host=X&token=Y"
// Вызывается только из main thread (fyne.Do или dialog callback) — не использовать fyne.DoAndWait,
// иначе дедлок на Android при вызове из callback.
func (qs *QRScanner) parseAndApply(qrText string, parent fyne.Window) {
	host, token, err := parseQRContents(qrText)
	if err != nil {
		dialog.ShowError(errors.New(fmt.Sprintf(i18n.Current.InvalidQRFormat, qrText)), parent)
		return
	}

	if host == "" {
		dialog.ShowError(errors.New(i18n.Current.HostCannotBeEmpty), parent)
		return
	}

	qs.showPreview(host, token, parent)
}

// parseQRContents парсит содержимое QR в host и token
func parseQRContents(qrText string) (host, token string, err error) {
	qrText = strings.TrimSpace(qrText)
	if qrText == "" {
		return "", "", fmt.Errorf("empty QR code")
	}

	// Формат usbridge://connect?host=X&token=Y
	if strings.HasPrefix(qrText, "usbridge://") {
		u, parseErr := url.Parse(qrText)
		if parseErr != nil {
			return "", "", parseErr
		}
		if u.Scheme != "usbridge" || u.Host != "connect" {
			return "", "", fmt.Errorf("unsupported deep link format")
		}
		query := u.Query()
		host = strings.TrimSpace(query.Get("host"))
		token = strings.TrimSpace(query.Get("token"))
		if host != "" && token != "" {
			return host, token, nil
		}
		return "", "", fmt.Errorf("host or token is missing in the link")
	}

	// Формат host:token
	parts := strings.SplitN(qrText, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected format host:token or usbridge://connect?host=X&token=Y")
	}
	host = strings.TrimSpace(parts[0])
	token = strings.TrimSpace(parts[1])
	return host, token, nil
}

// showPreview показывает диалог с кнопками: Сохранить, Подключиться, Отменить
func (qs *QRScanner) showPreview(host, token string, parent fyne.Window) {
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
			qs.onSave(host, host, token) // название = адрес хоста
			logrus.Infof("QR saved: host=%s", host)
		}
	})
	saveBtn.Importance = widget.MediumImportance

	connectBtn := widget.NewButton(i18n.Current.DeepLinkConnect, func() {
		if d != nil {
			d.Hide()
		}
		if qs.onConnect != nil {
			qs.onConnect(host, token)
			logrus.Infof("QR connect: host=%s", host)
		}
	})
	connectBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton(i18n.Current.Cancel, func() {
		if d != nil {
			d.Hide()
		}
	})

	// Connect (primary) слева, Save, Cancel
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

// ShowTestQRCode показывает тестовый QR-код для демонстрации (для разработки)
func ShowTestQRCode(parent fyne.Window) {
	qrText := "192.168.88.244:supersecret"

	info := widget.NewLabel(fmt.Sprintf(i18n.Current.QRExampleText, qrText))
	info.Wrapping = fyne.TextWrapWord

	copyBtn := widget.NewButton(i18n.Current.CopyText, func() {
		parent.Clipboard().SetContent(qrText)
		dialog.ShowInformation(i18n.Current.Done, i18n.Current.TextCopiedToClipboard, parent)
	})

	content := container.NewVBox(
		info,
		copyBtn,
	)

	dialog.NewCustom(i18n.Current.TestQRCode, i18n.Current.Close, content, parent).Show()
}

// CreateQRPreview создает виджет с превью QR-кода (если нужно показать QR в приложении)
func CreateQRPreview(qrText string) fyne.CanvasObject {
	label := widget.NewLabel(fmt.Sprintf(i18n.Current.QRCodeLabel, qrText))
	label.Wrapping = fyne.TextWrapWord

	return container.NewVBox(
		widget.NewLabelWithStyle(i18n.Current.QRCodeForConnection, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		label,
	)
}
