package view

import (
	"image/color"
	"strings"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type BackupWidgetUI struct {
	Container     *fyne.Container
	SnapshotsList *DevicesListView
	StatusLabel   *widget.Label
}

type BackupListRowSpec struct {
	Icon          fyne.Resource
	Title         string
	Subtitle      string
	Connected     bool
	ShowInfo      bool
	InfoTapped    func()
	ActionPassive bool
	ActionIcon    fyne.Resource
	ActionIconDim fyne.Resource
	ActionTapped  func()
	ActionEnabled bool
}

type backupRowTextLayout struct {
	gap float32
}

func NewBackupWidgetUI(buildRows func() []fyne.CanvasObject, onHelp func()) *BackupWidgetUI {
	statusLabel := widget.NewLabel(i18n.Current.ReadyToWork)
	list := NewDevicesListView(nil, func() []fyne.CanvasObject {
		var trailingAction fyne.CanvasObject
		if onHelp != nil {
			trailingAction = NewFooterIconButton(
				assets.QuestionIconDim,
				assets.QuestionIcon,
				fyne.NewSize(13, 13),
				onHelp,
			)
		}

		rows := []fyne.CanvasObject{}
		if buildRows != nil {
			rows = buildRows()
		}

		fill, border := color.NRGBA{R: 0x2c, G: 0x2c, B: 0x2c, A: 0xff}, design.ColorBorder
		return []fyne.CanvasObject{
			newBackupSectionCard(
				i18n.Current.BackupFlashName,
				fill,
				border,
				rows,
				trailingAction,
			),
		}
	})

	return &BackupWidgetUI{
		Container:     NewInset(list.Scroll, 8, 0, 6, 0),
		SnapshotsList: list,
		StatusLabel:   statusLabel,
	}
}

func NewBackupListRow(spec BackupListRowSpec) fyne.CanvasObject {
	icon := spec.Icon
	if icon == nil {
		icon = assets.SDCardIcon
	}

	prefixIcon := canvas.NewImageFromResource(icon)
	prefixIcon.FillMode = canvas.ImageFillContain
	prefixIcon.SetMinSize(fyne.NewSize(18, 18))

	titleText := NewBrandText(spec.Title, 14, design.ColorTextLight, true)
	if spec.Connected {
		titleText.Color = design.ColorAccent
	}
	subtitleText := NewBrandText(spec.Subtitle, 11, design.ColorTextMuted, false)
	textStack := container.New(&backupRowTextLayout{gap: 2}, titleText, subtitleText)

	left := container.NewGridWrap(fyne.NewSize(18, 18), prefixIcon)
	center := NewInset(textStack, 4, 0, 0, 0)

	controls := make([]fyne.CanvasObject, 0, 2)
	if spec.ShowInfo {
		infoBtn := newIconChromeButton(iconChromeButtonSpec{
			NormalFill:   design.ColorSurfaceLight,
			HoverFill:    design.ColorBorder,
			NormalIcon:   assets.InfoIcon,
			HoverIcon:    assets.InfoIcon,
			DisabledIcon: assets.InfoIconMuted,
			IconSize:     fyne.NewSize(18, 18),
			ButtonSize:   fyne.NewSize(deviceControlUnitWidth, deviceControlHeight),
			OnTapped:     spec.InfoTapped,
		})
		controls = append(controls, infoBtn)
	}

	if spec.ActionPassive {
		dot := canvas.NewCircle(design.ColorAccent)
		indicator := container.NewGridWrap(
			fyne.NewSize(deviceControlUnitWidth, deviceControlHeight),
			container.NewCenter(container.NewGridWrap(fyne.NewSize(12, 12), dot)),
		)
		controls = append(controls, indicator)
	} else {
		actionBtn := newIconChromeButton(iconChromeButtonSpec{
			NormalFill:   design.ColorSurfaceLight,
			HoverFill:    design.ColorBorder,
			NormalIcon:   spec.ActionIcon,
			HoverIcon:    spec.ActionIcon,
			DisabledIcon: spec.ActionIconDim,
			IconSize:     fyne.NewSize(18, 18),
			ButtonSize:   fyne.NewSize(deviceControlUnitWidth, deviceControlHeight),
			OnTapped:     spec.ActionTapped,
		})
		if spec.ActionEnabled {
			actionBtn.SetDisabled(false)
		} else {
			actionBtn.SetDisabled(true)
		}
		controls = append(controls, actionBtn)
	}

	right := container.New(&deviceRowControlsLayout{gap: deviceControlGap}, controls...)
	row := container.New(&deviceRowLayout{gap: 6}, left, center, right)
	return NewInset(row, 6, 6, 6, 4)
}

func (l *backupRowTextLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, obj := range objects {
		if obj == nil || !obj.Visible() {
			continue
		}
		min := obj.MinSize()
		obj.Move(fyne.NewPos(0, y))
		obj.Resize(fyne.NewSize(size.Width, min.Height))
		y += min.Height + l.gap
	}
}

func (l *backupRowTextLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	width := float32(0)
	height := float32(0)
	visible := 0
	for _, obj := range objects {
		if obj == nil || !obj.Visible() {
			continue
		}
		min := obj.MinSize()
		if min.Width > width {
			width = min.Width
		}
		height += min.Height
		visible++
	}
	if visible > 1 {
		height += l.gap * float32(visible-1)
	}
	return fyne.NewSize(width, height)
}

func newBackupSectionCard(
	eyebrow string,
	fill color.Color,
	border color.Color,
	rows []fyne.CanvasObject,
	trailingAction fyne.CanvasObject,
) fyne.CanvasObject {
	_ = border

	eyebrowText := NewBrandText(strings.ToUpper(eyebrow), 11, design.ColorTextMuted, true)
	var header fyne.CanvasObject
	if trailingAction != nil {
		header = NewInset(
			container.NewHBox(
				eyebrowText,
				layout.NewSpacer(),
				trailingAction,
			),
			4, 4, 0, 0,
		)
	} else {
		header = NewInset(eyebrowText, 6, 6, 0, 0)
	}

	var bodyContent fyne.CanvasObject
	if len(rows) == 0 {
		spacer := canvas.NewRectangle(color.Transparent)
		spacer.SetMinSize(fyne.NewSize(1, 12))
		bodyContent = NewInset(spacer, 1, 1, 0, 0)
	} else {
		bodyContent = NewInset(container.NewVBox(rows...), 0, 0, 0, 0)
	}

	cardContent := NewInset(bodyContent, 0, 0, 0, 0)
	card := NewInset(NewCompactSurfacePanel(cardContent, fill, design.RadiusMD+2), 0, 0, 0, 3)
	return container.NewVBox(header, card)
}
