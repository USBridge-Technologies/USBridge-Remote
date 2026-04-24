package gui

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/platform"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// DeepLinkHandler обработчик deep links
type DeepLinkHandler struct {
	onConnect func(host, quicToken, protocol string, quicPort int, tailscaleRegister bool)                              // Подключиться
	onSave    func(name, internalHost, tailscaleHost, quicToken, protocol string, quicPort int, tailscaleRegister bool) // Только сохранить без подключения
	lastURI   string                                                                                                   // Последний обработанный URI (чтобы не обрабатывать дважды)
}

// NewDeepLinkHandler создает новый обработчик
func NewDeepLinkHandler(onConnect func(host, quicToken, protocol string, quicPort int, tailscaleRegister bool), onSave func(name, internalHost, tailscaleHost, quicToken, protocol string, quicPort int, tailscaleRegister bool)) *DeepLinkHandler {
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
	internalHost, tailscaleHost, quicToken, protocol, quicPort, err := h.parseDeepLink(uri)
	if err != nil {
		logrus.Errorf("❌ Failed to parse deep link: %v", err)
		view.ShowErrorDialog(fmt.Errorf(i18n.Current.DeepLinkError, err), parent)
		return
	}

	// Показываем диалог подтверждения
	h.showConfirmDialog(internalHost, tailscaleHost, quicToken, protocol, quicPort, parent)
}

// parseDeepLink парсит deep link URI
func (h *DeepLinkHandler) parseDeepLink(uri string) (internalHost, tailscaleHost, quicToken, protocol string, quicPort int, err error) {
	// Парсим URL
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", "", "", 0, fmt.Errorf("invalid link format: %v", err)
	}

	// Проверяем схему (только usbridge://)
	if u.Scheme != "usbridge" {
		return "", "", "", "", 0, fmt.Errorf("unsupported scheme: %s (use usbridge://)", u.Scheme)
	}

	// Формат: usbridge://connect?host=192.168.1.1&quic_token=secret
	if u.Host != "connect" {
		return "", "", "", "", 0, fmt.Errorf("unsupported path: %s (use usbridge://connect)", u.Host)
	}

	// Получаем параметры
	query := u.Query()
	internalHost = query.Get("internal_host")
	tailscaleHost = query.Get("tailscale_host")
	host := query.Get("host")
	if internalHost == "" && tailscaleHost == "" {
		if isLikelyTailnetHost(host) {
			tailscaleHost = host
		} else {
			internalHost = host
		}
	}
	quicToken = query.Get("quic_token")
	if quicToken == "" {
		quicToken = query.Get("token")
	}
	protocol = query.Get("protocol")

	if query.Get("quic_port") != "" {
		fmt.Sscanf(query.Get("quic_port"), "%d", &quicPort)
	}

	// Проверяем обязательные параметры
	if internalHost == "" && tailscaleHost == "" {
		return "", "", "", "", 0, fmt.Errorf("missing host parameter")
	}

	if quicToken == "" {
		return "", "", "", "", 0, fmt.Errorf("missing quic_token parameter")
	}

	logrus.Infof("✅ Deep link parsed: internal=%s tailscale=%s quicToken=%s protocol=%s", internalHost, tailscaleHost, maskSensitiveToken(quicToken), protocol)
	return internalHost, tailscaleHost, quicToken, protocol, quicPort, nil
}

// showConfirmDialog показывает диалог подтверждения подключения с возможностью сохранения
// ВАЖНО: должна вызываться из UI потока (внутри fyne.Do)
func (h *DeepLinkHandler) showConfirmDialog(internalHost, tailscaleHost, quicToken, protocol string, quicPort int, parent fyne.Window) {
	host := resolveDeepLinkHost(protocol, internalHost, tailscaleHost)
	// Создаем превью с данными
	titleLabel := widget.NewLabelWithStyle(
		"🔗 "+i18n.Current.ConnectViaLink,
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	quicTokenLabel := widget.NewLabel(i18n.Current.DeepLinkToken)
	quicTokenEntry := widget.NewEntry()
	quicTokenEntry.SetText(quicToken)
	quicTokenEntry.Disable() // Только для чтения

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
			h.onConnect(host, quicToken, protocol, quicPort, false)
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
			h.onSave("", internalHost, tailscaleHost, quicToken, protocol, quicPort, false)
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
			widget.NewLabel("Internal Address"),
			disabledDeepLinkEntry(internalHost),
			widget.NewLabel("Tailscale Address"),
			disabledDeepLinkEntry(tailscaleHost),
			quicTokenLabel,
			quicTokenEntry,
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
// Формат: usbridge://connect?internal_host=<HOST>&tailscale_host=<HOST>&quic_token=<TOKEN>&protocol=<PROTOCOL>
func GenerateDeepLink(internalHost, tailscaleHost, quicToken, protocol string, quicPort int) string {
	// Кодируем параметры
	params := url.Values{}
	if internalHost != "" {
		params.Set("internal_host", internalHost)
	}
	if tailscaleHost != "" {
		params.Set("tailscale_host", tailscaleHost)
	}
	if quicToken != "" {
		params.Set("quic_token", quicToken)
	}
	if protocol != "" {
		params.Set("protocol", protocol)
	}
	if quicPort > 0 {
		params.Set("quic_port", fmt.Sprintf("%d", quicPort))
	}

	return fmt.Sprintf("usbridge://connect?%s", params.Encode())
}

func disabledDeepLinkEntry(value string) *widget.Entry {
	entry := widget.NewEntry()
	entry.SetText(value)
	entry.Disable()
	return entry
}

func resolveDeepLinkHost(protocol, internalHost, tailscaleHost string) string {
	if protocol == "tailscale" && tailscaleHost != "" {
		return tailscaleHost
	}
	if protocol == "quic" && internalHost != "" {
		return internalHost
	}
	if tailscaleHost != "" {
		return tailscaleHost
	}
	return internalHost
}

func isLikelyTailnetHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.HasSuffix(strings.ToLower(host), ".ts.net") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return netip.MustParsePrefix("100.64.0.0/10").Contains(addr)
}
