package view

import (
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// ShowMobileSettingsMenu is the phone Connections gear panel: a larger
// Grid/List segmented control (same look as the desktop header toggle)
// plus icon rows for Info / Community / Language.
func ShowMobileSettingsMenu(anchor fyne.CanvasObject, mode string, onViewMode func(string), onInfo, onCommunity, onLanguage func()) {
	if anchor == nil {
		return
	}

	var popup *dropdownPopup
	hideThen := func(fn func()) {
		if popup != nil {
			popup.Hide()
		}
		if fn != nil {
			fn()
		}
	}

	row := func(label string, icon fyne.Resource, onTap func()) *dropdownItem {
		item := newDropdownItem(label, "", false, func() {
			hideThen(onTap)
		})
		item.textColor = design.ColorConnectionBadgeText
		item.textSize = 13
		item.minHeight = 38
		item.iconRes = icon
		item.iconSide = 16
		return item
	}

	toggle := newMobileSettingsViewModeToggle(mode, onViewMode)
	rule := canvas.NewRectangle(color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff})
	rule.SetMinSize(fyne.NewSize(1, 1))

	content := container.NewVBox(
		NewInset(toggle, 2, 2, 2, 6),
		rule,
		row("Info", assets.QuestionIconTeal, onInfo),
		row("Community", assets.DiscordIconTeal, onCommunity),
		row("Language", assets.LanguageIconTeal, onLanguage),
	)
	popup = showStyledPanel(anchor, content, 220, false)
}

func newMobileSettingsViewModeToggle(initialMode string, onChange func(mode string)) fyne.CanvasObject {
	var gridBtn, listBtn *iconChromeButton

	setActive := func(listActive bool) {
		gridIcon, listIcon := assets.GridViewIconMuted, assets.ListViewIconAccent
		gridColor, listColor := design.ColorConnectionsSectionMutedText, design.ColorConnectionsSectionIcon
		gridFill, listFill := design.ColorGray900, design.ColorSurfaceLight
		if !listActive {
			gridIcon, listIcon = assets.GridViewIconAccent, assets.ListViewIconMuted
			gridColor, listColor = design.ColorConnectionsSectionIcon, design.ColorConnectionsSectionMutedText
			gridFill, listFill = design.ColorSurfaceLight, design.ColorGray900
		}
		gridBtn.spec.NormalFill = gridFill
		gridBtn.SetLabelColor(gridColor)
		gridBtn.SetIcons(gridIcon, gridIcon, gridIcon)
		listBtn.spec.NormalFill = listFill
		listBtn.SetLabelColor(listColor)
		listBtn.SetIcons(listIcon, listIcon, listIcon)
	}

	const rowH float32 = 28
	iconSize := fyne.NewSize(12, 12)
	gridBtn = newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   design.ColorGray900,
		HoverFill:    design.ColorBorder,
		Stroke:       color.Transparent,
		NormalIcon:   assets.GridViewIconMuted,
		IconSize:     iconSize,
		ButtonSize:   fyne.NewSize(0, rowH),
		CornerRadius: 5,
		LabelSize:    12,
		LabelBold:    true,
	})
	gridBtn.SetText(i18n.Current.ViewModeGrid)
	listBtn = newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   design.ColorSurfaceLight,
		HoverFill:    design.ColorBorder,
		Stroke:       color.Transparent,
		NormalIcon:   assets.ListViewIconAccent,
		IconSize:     iconSize,
		ButtonSize:   fyne.NewSize(0, rowH),
		CornerRadius: 5,
		LabelSize:    12,
		LabelBold:    true,
	})
	listBtn.SetText(i18n.Current.ViewModeList)
	gridBtn.SetOnTapped(func() {
		setActive(false)
		if onChange != nil {
			onChange("grid")
		}
	})
	listBtn.SetOnTapped(func() {
		setActive(true)
		if onChange != nil {
			onChange("list")
		}
	})
	setActive(initialMode != "grid")

	track := canvas.NewRectangle(design.ColorGray900)
	track.CornerRadius = design.RadiusMD
	track.StrokeColor = design.ColorHeaderAccentLine
	track.StrokeWidth = 1

	buttons := container.NewGridWithColumns(2, gridBtn, listBtn)
	return container.NewStack(track, NewInset(buttons, 2, 2, 2, 2))
}
