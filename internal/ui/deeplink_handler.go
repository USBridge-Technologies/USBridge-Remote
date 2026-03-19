package ui

import (
	"fmt"
	"net/url"

	"usbridge-client/internal/platform"
	"usbridge-client/internal/ui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// DeepLinkHandler обработчик deep links
type DeepLinkHandler struct {
	onConnect func(host, token, protocol, wireGuardInvite string)       // Подключиться
	onSave    func(name, host, token, protocol, wireGuardInvite string) // Только сохранить без подключения
	lastURI   string                                   // Последний обработанный URI (чтобы не обрабатывать дважды)
}

// NewDeepLinkHandler создает новый обработчик
func NewDeepLinkHandler(onConnect func(host, token, protocol, wireGuardInvite string), onSave func(name, host, token, protocol, wireGuardInvite string)) *DeepLinkHandler {
	return &DeepLinkHandler{
		onConnect: onConnect,
		onSave:    onSave,
		lastURI:   "",
	}
}

// CheckAndHandleDeepLink проверяет наличие deep link и обрабатывает его
func (h *DeepLinkHandler) CheckAndHandleDeepLink(parent fyne.Window) {
	// Получаем URI из Intent
	uri, err := platform.GetIntentDataURI()
	if err != nil {
		logrus.Errorf("❌ Failed to read deep link: %v", err)
		return
	}

	if uri == "" {
		// Нет deep link
		return
	}

	// Проверяем, не обрабатывали ли мы этот URI уже
	if h.lastURI == uri {
		// Уже обработан, пропускаем
		return
	}

	logrus.Infof("🔗 New deep link detected: %s", uri)

	// Запоминаем текущий URI
	h.lastURI = uri

	// Парсим URI
	host, token, protocol, wireGuardInvite, err := h.parseDeepLink(uri)
	if err != nil {
		logrus.Errorf("❌ Failed to parse deep link: %v", err)
		dialog.ShowError(fmt.Errorf(i18n.Current.DeepLinkError, err), parent)
		return
	}

	// Показываем диалог подтверждения
	h.showConfirmDialog(host, token, protocol, wireGuardInvite, parent)
}

// parseDeepLink парсит deep link URI
func (h *DeepLinkHandler) parseDeepLink(uri string) (host, token, protocol, wireGuardInvite string, err error) {
	// Парсим URL
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", "", "", fmt.Errorf("invalid link format: %v", err)
	}

	// Проверяем схему (только usbridge://)
	if u.Scheme != "usbridge" {
		return "", "", "", "", fmt.Errorf("unsupported scheme: %s (use usbridge://)", u.Scheme)
	}

	// Формат: usbridge://connect?host=192.168.1.1&token=secret
	if u.Host != "connect" {
		return "", "", "", "", fmt.Errorf("unsupported path: %s (use usbridge://connect)", u.Host)
	}

	// Получаем параметры
	query := u.Query()
	host = query.Get("host")
	token = query.Get("token")
	protocol = query.Get("protocol")
	wireGuardInvite = query.Get("wireguard_invite")

	// Проверяем обязательные параметры
	if host == "" {
		return "", "", "", "", fmt.Errorf("missing host parameter")
	}

	if token == "" && wireGuardInvite == "" {
		return "", "", "", "", fmt.Errorf("missing token or wireguard_invite parameter")
	}

	logrus.Infof("✅ Deep link parsed: host=%s, token=%s, protocol=%s", host, token, protocol)
	return host, token, protocol, wireGuardInvite, nil
}

// showConfirmDialog показывает диалог подтверждения подключения с возможностью сохранения
// ВАЖНО: должна вызываться из UI потока (внутри fyne.Do)
func (h *DeepLinkHandler) showConfirmDialog(host, token, protocol, wireGuardInvite string, parent fyne.Window) {
	// Создаем превью с данными
	titleLabel := widget.NewLabelWithStyle(
		"🔗 "+i18n.Current.ConnectViaLink,
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	hostLabel := widget.NewLabel(i18n.Current.DeepLinkServerAddress)
	hostValue := widget.NewLabel(host)
	hostValue.TextStyle = fyne.TextStyle{Bold: true}

	tokenLabel := widget.NewLabel(i18n.Current.DeepLinkToken)
	tokenEntry := widget.NewEntry()
	tokenEntry.SetText(token)
	tokenEntry.Disable() // Только для чтения

	infoLabel := widget.NewLabel(i18n.Current.DeepLinkConnectPrompt)
	infoLabel.Wrapping = fyne.TextWrapWord

	// Создаем кнопки (сначала создаем, чтобы использовать в callback'ах)
	var d dialog.Dialog

	connectBtn := widget.NewButton(i18n.Current.DeepLinkConnect, func() {
		logrus.Infof("✅ User chose to connect via deep link")
		if d != nil {
			d.Hide()
		}
		if h.onConnect != nil {
			h.onConnect(host, token, protocol, wireGuardInvite)
		}
	})
	connectBtn.Importance = widget.HighImportance

	saveBtn := widget.NewButton(i18n.Current.DeepLinkSave, func() {
		logrus.Infof("💾 User chose to save the connection from deep link")
		if d != nil {
			d.Hide()
		}
		// Вызываем callback для сохранения с пустым именем - будет сгенерировано автоматически
		if h.onSave != nil {
			h.onSave("", host, token, protocol, wireGuardInvite)
		}
	})
	saveBtn.Importance = widget.MediumImportance

	cancelBtn := widget.NewButton(i18n.Current.Cancel, func() {
		logrus.Info("❌ User cancelled deep link connection")
		if d != nil {
			d.Hide()
		}
	})
	cancelBtn.Importance = widget.LowImportance

	// Кнопки: Connect (Yes/primary) слева, Save, Cancel
	buttons := container.NewGridWithColumns(3,
		connectBtn,
		saveBtn,
		cancelBtn,
	)

	// Полный контент с кнопками
	fullContent := container.NewBorder(
		container.NewVBox(
			titleLabel,
			widget.NewSeparator(),
			infoLabel,
			widget.NewSeparator(),
			hostLabel,
			hostValue,
			tokenLabel,
			tokenEntry,
		), // Верх
		buttons,  // Низ
		nil, nil, // Лево, Право
		nil, // Центр
	)

	// Создаем кастомный диалог с полным контентом
	d = dialog.NewCustomWithoutButtons(i18n.Current.ConnectionTitle, fullContent, parent)
	d.Resize(fyne.NewSize(500, 380))
	d.Show()
}

// GenerateDeepLink генерирует deep link для подключения.
// Формат: usbridge://connect?host=<HOST>&token=<TOKEN>&protocol=<PROTOCOL>
func GenerateDeepLink(host, token, protocol, wireGuardInvite string) string {
	// Кодируем параметры
	params := url.Values{}
	params.Set("host", host)
	if token != "" {
		params.Set("token", token)
	}
	if protocol != "" {
		params.Set("protocol", protocol)
	}
	if wireGuardInvite != "" {
		params.Set("wireguard_invite", wireGuardInvite)
	}

	return fmt.Sprintf("usbridge://connect?%s", params.Encode())
}
