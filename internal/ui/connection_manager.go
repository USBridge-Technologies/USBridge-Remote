package ui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"usbridge-client/internal/models"
	"usbridge-client/internal/ui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

func normalizeConnectionProtocol(protocol string) string {
	switch strings.TrimSpace(protocol) {
	case models.ConnectionProtocolQUIC:
		return models.ConnectionProtocolQUIC
	case models.ConnectionProtocolWireGuard:
		return models.ConnectionProtocolWireGuard
	default:
		return models.ConnectionProtocolAuto
	}
}

func connectionProtocolBadge(protocol string) string {
	switch normalizeConnectionProtocol(protocol) {
	case models.ConnectionProtocolWireGuard:
		return "◆ WG"
	case models.ConnectionProtocolQUIC:
		return "▲ QUIC"
	default:
		return "● AUTO"
	}
}

// SavedConnection сохраненное подключение
type SavedConnection struct {
	Name            string `json:"name"`
	Host            string `json:"host"`
	Token           string `json:"token"`
	Protocol        string `json:"protocol,omitempty"`
	WireGuardInvite string `json:"wireguard_invite,omitempty"`
}

// ConnectionManager менеджер подключений
type ConnectionManager struct {
	container *fyne.Container
	app       fyne.App
	window    fyne.Window

	// Данные
	connections   []SavedConnection
	selectedIndex int

	// UI элементы
	connectionsScroll *container.Scroll
	connectionsBox    *fyne.Container
	hostEntry         *widget.Entry
	tokenEntry        *widget.Entry
	protocolSelect    *widget.Select
	qrBtn             *widget.Button
	addBtn            *widget.Button

	// QR сканер
	qrScanner *QRScanner

	// Callbacks
	onConnect        func(host, token, protocol, wireGuardInvite string)
	onLanguageChange func()
}

// NewConnectionManager создает новый менеджер подключений
func NewConnectionManager(app fyne.App, window fyne.Window, hostEntry, tokenEntry *widget.Entry, protocolSelect *widget.Select, onConnect func(host, token, protocol, wireGuardInvite string)) *ConnectionManager {
	cm := &ConnectionManager{
		app:            app,
		window:         window,
		hostEntry:      hostEntry,
		tokenEntry:     tokenEntry,
		protocolSelect: protocolSelect,
		onConnect:      onConnect,
		selectedIndex:  -1,
		connections:    make([]SavedConnection, 0),
	}

	cm.qrScanner = NewQRScanner(app,
		func(host, token, protocol, wireGuardInvite string) {
			fyne.Do(func() {
				cm.hostEntry.SetText(host)
				cm.tokenEntry.SetText(token)
				if cm.protocolSelect != nil && protocol != "" {
					cm.protocolSelect.SetSelected(protocol)
				}
			})
			if cm.onConnect != nil {
				cm.onConnect(host, token, protocol, wireGuardInvite)
			}
			logrus.Infof("QR подключение: host=%s", host)
		},
		func(name, host, token, protocol, wireGuardInvite string) {
			cm.SaveConnection(name, host, token, protocol, wireGuardInvite)
			fyne.Do(func() {
				cm.hostEntry.SetText(host)
				cm.tokenEntry.SetText(token)
				if cm.protocolSelect != nil && protocol != "" {
					cm.protocolSelect.SetSelected(protocol)
				}
			})
			logrus.Infof("QR сохранено: host=%s", host)
		},
	)

	cm.loadConnections()
	cm.createInterface()
	return cm
}

// createInterface создает интерфейс менеджера
func (cm *ConnectionManager) createInterface() {
	savedLabel := widget.NewLabelWithStyle(i18n.Current.SavedConnections, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	// Кнопка языка — справа, заметная
	langBtn := widget.NewButton("🌐", nil)
	langBtn.Importance = widget.MediumImportance
	langBtn.OnTapped = func() {
		cm.showLanguageMenu(langBtn)
	}

	// QR — с подписью
	cm.qrBtn = widget.NewButton(i18n.Current.QRScannerButton, cm.handleQRScan)
	cm.qrBtn.Importance = widget.MediumImportance

	// Add connection
	cm.addBtn = widget.NewButton("➕ "+i18n.Current.AddConnectionTitle, cm.showAddDialog)
	cm.addBtn.Importance = widget.MediumImportance

	// Верхняя строка: заголовок слева, язык справа
	header := container.NewBorder(nil, nil, savedLabel, langBtn)

	// Контейнер для списка
	cm.connectionsBox = container.NewVBox()
	cm.refreshConnectionsList()

	cm.connectionsScroll = container.NewScroll(cm.connectionsBox)
	cm.connectionsScroll.SetMinSize(fyne.NewSize(0, 220))

	// Нижняя строка: QR + Add connection
	actionBar := container.NewHBox(cm.qrBtn, cm.addBtn)

	mainContent := container.NewPadded(
		container.NewBorder(
			header,
			actionBar, // внизу
			nil, nil,
			cm.connectionsScroll,
		),
	)

	cm.container = container.NewPadded(mainContent)
}

// showLanguageMenu показывает меню выбора языка (компактный popup)
func (cm *ConnectionManager) showLanguageMenu(btn *widget.Button) {
	menu := fyne.NewMenu("",
		fyne.NewMenuItem(i18n.Current.LanguageEnglish, func() { cm.setLanguage("en") }),
		fyne.NewMenuItem(i18n.Current.LanguageRussian, func() { cm.setLanguage("ru") }),
	)
	popup := widget.NewPopUpMenu(menu, cm.window.Canvas())
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(btn)
	popup.ShowAtPosition(pos.Add(fyne.NewPos(0, btn.Size().Height)))
}

func (cm *ConnectionManager) setLanguage(langCode string) {
	cm.app.Preferences().SetString("language", langCode)
	i18n.SetLanguage(langCode)
	logrus.Infof("Language changed to: %s", langCode)
	if cm.onLanguageChange != nil {
		cm.onLanguageChange()
	}
}

// refreshConnectionsList перерисовывает список подключений
func (cm *ConnectionManager) refreshConnectionsList() {
	cm.connectionsBox.RemoveAll()

	if len(cm.connections) == 0 {
		emptyLabel := widget.NewLabel(i18n.Current.NoSavedConnections)
		emptyLabel.Alignment = fyne.TextAlignCenter
		cm.connectionsBox.Add(container.NewCenter(emptyLabel))
	} else {
		for i, conn := range cm.connections {
			conn := conn
			idx := i
			row := cm.createConnectionRow(conn, idx)
			cm.connectionsBox.Add(row)
		}
	}

	cm.connectionsBox.Refresh()
}

// createConnectionRow создает карточку для подключения
func (cm *ConnectionManager) createConnectionRow(conn SavedConnection, idx int) *fyne.Container {
	conn.Protocol = normalizeConnectionProtocol(conn.Protocol)

	fillForm := func() {
		fyne.Do(func() {
			cm.hostEntry.SetText(conn.Host)
			cm.tokenEntry.SetText(conn.Token)
			cm.selectedIndex = idx
		})
	}

	// Название — крупным жирным шрифтом
	nameText := canvas.NewText(conn.Name, theme.Color(theme.ColorNameForeground))
	nameText.TextSize = theme.TextSubHeadingSize()
	nameText.TextStyle.Bold = true
	nameSelectBtn := widget.NewButton("", fillForm)
	nameSelectBtn.Importance = widget.LowImportance
	nameRow := container.NewStack(nameText, container.NewMax(nameSelectBtn))

	// Адрес — кликабельная область для заполнения формы
	hostLabel := widget.NewLabel(conn.Host)
	hostLabel.TextStyle.Italic = true
	hostSelectBtn := widget.NewButton("", fillForm)
	hostSelectBtn.Importance = widget.LowImportance
	hostLabelWithClick := container.NewStack(hostLabel, container.NewMax(hostSelectBtn))

	protocolBtn := widget.NewButton(connectionProtocolBadge(conn.Protocol), nil)
	protocolBtn.Importance = widget.LowImportance
	protocolBtn.OnTapped = func() {
		menu := fyne.NewMenu("",
			fyne.NewMenuItem(connectionProtocolBadge(models.ConnectionProtocolAuto), func() {
				cm.connections[idx].Protocol = models.ConnectionProtocolAuto
				protocolBtn.SetText(connectionProtocolBadge(models.ConnectionProtocolAuto))
				cm.saveConnections()
				cm.refreshConnectionsList()
			}),
			fyne.NewMenuItem(connectionProtocolBadge(models.ConnectionProtocolQUIC), func() {
				cm.connections[idx].Protocol = models.ConnectionProtocolQUIC
				protocolBtn.SetText(connectionProtocolBadge(models.ConnectionProtocolQUIC))
				cm.saveConnections()
				cm.refreshConnectionsList()
			}),
			fyne.NewMenuItem(connectionProtocolBadge(models.ConnectionProtocolWireGuard), func() {
				cm.connections[idx].Protocol = models.ConnectionProtocolWireGuard
				protocolBtn.SetText(connectionProtocolBadge(models.ConnectionProtocolWireGuard))
				cm.saveConnections()
				cm.refreshConnectionsList()
			}),
		)
		popup := widget.NewPopUpMenu(menu, cm.window.Canvas())
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(protocolBtn)
		popup.ShowAtPosition(pos.Add(fyne.NewPos(0, protocolBtn.Size().Height)))
	}

	// Кнопка со стрелкой — заполняет форму и сразу подключается
	useBtn := widget.NewButton("→", func() {
		fyne.Do(func() {
			cm.hostEntry.SetText(conn.Host)
			cm.tokenEntry.SetText(conn.Token)
			cm.selectedIndex = idx
			if cm.onConnect != nil {
				protocol := normalizeConnectionProtocol(cm.connections[idx].Protocol)
				cm.onConnect(conn.Host, conn.Token, protocol, conn.WireGuardInvite)
			}
		})
	})
	useBtn.Importance = widget.MediumImportance

	editBtn := widget.NewButton(i18n.Current.EditButton, func() {
		cm.showEditDialog(idx)
	})
	editBtn.Importance = widget.LowImportance

	deleteBtn := widget.NewButton("🗑️", func() {
		cm.handleDeleteConnection(idx)
	})
	deleteBtn.Importance = widget.LowImportance

	// Центральная область (между адресом и кнопками) — тоже кликабельна для заполнения
	centerSelectBtn := widget.NewButton("", fillForm)
	centerSelectBtn.Importance = widget.LowImportance
	centerArea := container.NewStack(layout.NewSpacer(), container.NewMax(centerSelectBtn))

	// Нижняя строка: стрелка + адрес + пустое пространство + кнопки
	bottomRow := container.NewBorder(nil, nil,
		container.NewHBox(useBtn, protocolBtn, hostLabelWithClick),
		container.NewHBox(editBtn, deleteBtn),
		centerArea)

	card := widget.NewCard("", "", container.NewVBox(
		container.NewPadded(nameRow),
		bottomRow,
	))
	return container.NewPadded(card)
}

// formField создаёт поле формы: подпись сверху, поле ввода снизу
func formField(label string, w fyne.CanvasObject) *widget.FormItem {
	return &widget.FormItem{
		Text:   "",
		Widget: container.NewVBox(widget.NewLabel(label), w),
	}
}

// createTokenFieldWithButtons создаёт поле токена с кнопками копирования и показа/скрытия
func createTokenFieldWithButtons(tokenEntry *widget.Entry, window fyne.Window) *fyne.Container {
	// Кнопка копирования токена
	copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		txt := tokenEntry.Text
		if txt != "" && window != nil {
			window.Clipboard().SetContent(txt)
		}
	})
	copyBtn.Importance = widget.LowImportance

	// Кнопка показать/скрыть (tap-to-reveal)
	var visibilityBtn *widget.Button
	visibilityBtn = widget.NewButtonWithIcon("", theme.VisibilityIcon(), func() {
		tokenEntry.Password = !tokenEntry.Password
		if tokenEntry.Password {
			visibilityBtn.SetIcon(theme.VisibilityIcon())
		} else {
			visibilityBtn.SetIcon(theme.VisibilityOffIcon())
		}
	})
	visibilityBtn.Importance = widget.LowImportance

	return container.NewBorder(nil, nil, nil, container.NewHBox(copyBtn, visibilityBtn), tokenEntry)
}

// showEditDialog показывает диалог редактирования (Apply слева, Cancel справа)
func (cm *ConnectionManager) showEditDialog(idx int) {
	if idx < 0 || idx >= len(cm.connections) {
		return
	}
	conn := cm.connections[idx]

	nameEntry := widget.NewEntry()
	nameEntry.SetText(conn.Name)
	nameEntry.SetPlaceHolder(i18n.Current.ConnectionNamePlaceholder)

	hostEntry := widget.NewEntry()
	hostEntry.SetText(conn.Host)
	hostEntry.SetPlaceHolder(i18n.Current.ServerAddress)

	tokenEntry := widget.NewEntry()
	tokenEntry.SetText(conn.Token)
	tokenEntry.SetPlaceHolder(i18n.Current.Token)
	tokenEntry.Password = true

	tokenRow := createTokenFieldWithButtons(tokenEntry, cm.window)

	form := container.NewVBox(
		container.NewVBox(widget.NewLabel(i18n.Current.ConnectionNamePlaceholder), nameEntry),
		container.NewVBox(widget.NewLabel(i18n.Current.ServerAddress), hostEntry),
		container.NewVBox(widget.NewLabel(i18n.Current.Token), tokenRow),
	)

	var d dialog.Dialog
	applyBtn := widget.NewButton(i18n.Current.Apply, func() {
		name := nameEntry.Text
		host := hostEntry.Text
		token := tokenEntry.Text
		if name == "" || host == "" {
			logrus.Warn("Название и адрес обязательны")
			return
		}
		cm.connections[idx] = SavedConnection{Name: name, Host: host, Token: token, Protocol: conn.Protocol, WireGuardInvite: conn.WireGuardInvite}
		cm.saveConnections()
		fyne.Do(func() {
			cm.hostEntry.SetText(host)
			cm.tokenEntry.SetText(token)
			if cm.protocolSelect != nil && conn.Protocol != "" {
				cm.protocolSelect.SetSelected(conn.Protocol)
			}
			cm.refreshConnectionsList()
		})
		logrus.Infof("Обновлено подключение: %s", name)
		if d != nil {
			d.Hide()
		}
	})
	applyBtn.Importance = widget.HighImportance
	applyBtn.SetIcon(theme.ConfirmIcon())

	cancelBtn := widget.NewButton(i18n.Current.Cancel, func() {
		if d != nil {
			d.Hide()
		}
	})
	cancelBtn.SetIcon(theme.CancelIcon())

	// Apply слева, Cancel справа
	buttons := container.NewGridWithColumns(2, applyBtn, cancelBtn)
	content := container.NewVBox(form, widget.NewSeparator(), buttons)

	d = dialog.NewCustomWithoutButtons(i18n.Current.EditConnectionTitle, content, cm.window)
	d.Show()
}

// showAddDialog показывает диалог добавления (Apply слева, Cancel справа)
func (cm *ConnectionManager) showAddDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder(i18n.Current.ConnectionNamePlaceholder)

	hostEntry := widget.NewEntry()
	hostEntry.SetText(cm.hostEntry.Text)
	hostEntry.SetPlaceHolder(i18n.Current.ServerAddress)

	tokenEntry := widget.NewEntry()
	tokenEntry.SetText(cm.tokenEntry.Text)
	tokenEntry.SetPlaceHolder(i18n.Current.Token)
	tokenEntry.Password = true

	tokenRow := createTokenFieldWithButtons(tokenEntry, cm.window)

	form := container.NewVBox(
		container.NewVBox(widget.NewLabel(i18n.Current.ConnectionNamePlaceholder), nameEntry),
		container.NewVBox(widget.NewLabel(i18n.Current.ServerAddress), hostEntry),
		container.NewVBox(widget.NewLabel(i18n.Current.Token), tokenRow),
	)

	var d dialog.Dialog
	applyBtn := widget.NewButton(i18n.Current.Apply, func() {
		name := nameEntry.Text
		host := hostEntry.Text
		token := tokenEntry.Text
		protocol := ""
		if cm.protocolSelect != nil {
			protocol = cm.protocolSelect.Selected
		}
		cm.SaveConnection(name, host, token, protocol, "")
		fyne.Do(func() {
			cm.hostEntry.SetText(host)
			cm.tokenEntry.SetText(token)
			cm.refreshConnectionsList()
		})
		if d != nil {
			d.Hide()
		}
	})
	applyBtn.Importance = widget.HighImportance
	applyBtn.SetIcon(theme.ConfirmIcon())

	cancelBtn := widget.NewButton(i18n.Current.Cancel, func() {
		if d != nil {
			d.Hide()
		}
	})
	cancelBtn.SetIcon(theme.CancelIcon())

	// Apply слева, Cancel справа
	buttons := container.NewGridWithColumns(2, applyBtn, cancelBtn)
	content := container.NewVBox(form, widget.NewSeparator(), buttons)

	d = dialog.NewCustomWithoutButtons(i18n.Current.AddConnectionTitle, content, cm.window)
	d.Show()
}

// handleDeleteConnection удаляет подключение по индексу (с подтверждением)
func (cm *ConnectionManager) handleDeleteConnection(idx int) {
	if idx < 0 || idx >= len(cm.connections) {
		return
	}
	deletedName := cm.connections[idx].Name

	ShowConfirmYesLeft(
		i18n.Current.DeleteConnectionTitle,
		fmt.Sprintf(i18n.Current.DeleteConnectionConfirm, deletedName),
		func(confirmed bool) {
			if !confirmed {
				return
			}
			fyne.Do(func() {
				cm.window.Canvas().Focus(nil)
			})
			cm.connections = append(cm.connections[:idx], cm.connections[idx+1:]...)
			cm.selectedIndex = -1
			cm.saveConnections()
			fyne.Do(func() {
				cm.hostEntry.SetText("")
				cm.tokenEntry.SetText("")
				cm.refreshConnectionsList()
			})
			logrus.Infof("Удалено подключение: %s", deletedName)
		},
		cm.window,
	)
}

// handleQRScan обрабатывает нажатие на кнопку QR-сканера
func (cm *ConnectionManager) handleQRScan() {
	logrus.Info("Открытие QR-сканера")
	cm.qrScanner.ShowCameraScanner(cm.window)
}

// GetContainer возвращает контейнер
func (cm *ConnectionManager) GetContainer() *fyne.Container {
	return cm.container
}

// SetLanguageChangeCallback устанавливает callback для изменения языка
func (cm *ConnectionManager) SetLanguageChangeCallback(callback func()) {
	cm.onLanguageChange = callback
}

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
	}
}
