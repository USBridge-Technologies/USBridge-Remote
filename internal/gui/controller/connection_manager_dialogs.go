package controller

import (
	"fmt"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

func (cm *ConnectionManager) setLanguage(langCode string) {
	cm.app.Preferences().SetString("language", langCode)
	i18n.SetLanguage(langCode)
	logrus.Infof("Language changed to: %s", langCode)
	if cm.onLanguageChange != nil {
		cm.onLanguageChange()
	}
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
	copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		txt := tokenEntry.Text
		if txt != "" && window != nil {
			window.Clipboard().SetContent(txt)
		}
	})
	copyBtn.Importance = widget.LowImportance

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

// showEditDialog показывает диалог редактирования
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

	content := container.NewVBox(form, widget.NewSeparator(), container.NewGridWithColumns(2, applyBtn, cancelBtn))
	d = dialog.NewCustomWithoutButtons(i18n.Current.EditConnectionTitle, content, cm.window)
	d.Show()
}

// showAddDialog показывает диалог добавления
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

	content := container.NewVBox(form, widget.NewSeparator(), container.NewGridWithColumns(2, applyBtn, cancelBtn))
	d = dialog.NewCustomWithoutButtons(i18n.Current.AddConnectionTitle, content, cm.window)
	d.Show()
}

// handleDeleteConnection удаляет подключение по индексу
func (cm *ConnectionManager) handleDeleteConnection(idx int) {
	if idx < 0 || idx >= len(cm.connections) {
		return
	}
	deletedName := cm.connections[idx].Name

	view.ShowConfirmYesLeft(
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
