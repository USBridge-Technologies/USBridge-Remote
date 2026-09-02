package gui

import (
	"fmt"
	"image/color"
	"strings"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (mw *MainWindow) showStorageInfoDialog() {
	if mw == nil || mw.window == nil {
		return
	}

	internalTitle := "Internal storage"
	internalValue := "0/0 GB"
	internalPercent := "0%"
	sdTitle := "SD card"
	sdValue := "0/0 GB"
	sdPercent := "0%"

	if mw.storageStatus != nil {
		if mw.storageStatus.BootDeviceIsSD {
			// Booted from the SD slot: there's no separate onboard eMMC in
			// play right now, and no free slot left for an external card
			// either -- /mnt/emmc (what the API calls "EMMC") is actually
			// *this same SD card's own* extra partition. Attribute it to
			// "SD Card" instead of "Internal storage", and show the second
			// block as not applicable rather than a confusing "0/0 GB" that
			// looks like a missing/unmounted card.
			internalTitle = "SD Card (booted from this card)"
			if mw.storageStatus.EMMC.Total > 0 {
				internalValue = models.FormatStorageSizeOnly(mw.storageStatus.EMMC.Used, mw.storageStatus.EMMC.Total)
				internalPercent = fmt.Sprintf("%.1f%%", mw.storageStatus.EMMC.Percent)
			}
			sdTitle = "External SD Card"
			sdValue = "N/A"
			sdPercent = "—"
		} else {
			if mw.storageStatus.EMMC.Total > 0 {
				internalValue = models.FormatStorageSizeOnly(mw.storageStatus.EMMC.Used, mw.storageStatus.EMMC.Total)
				internalPercent = fmt.Sprintf("%.1f%%", mw.storageStatus.EMMC.Percent)
			}
			if mw.storageStatus.SDCard.Total > 0 {
				sdValue = models.FormatStorageSizeOnly(mw.storageStatus.SDCard.Used, mw.storageStatus.SDCard.Total)
				sdPercent = fmt.Sprintf("%.1f%%", mw.storageStatus.SDCard.Percent)
			}
		}
	} else {
		// Fallback to old behavior if new status is not yet available
		internalUsed, internalTotal := int64(0), int64(0)
		sdUsed, sdTotal := int64(0), int64(0)
		currentUsed := mw.currentStorageTotal - mw.currentStorageAvailable
		if currentUsed < 0 {
			currentUsed = 0
		}

		if strings.HasPrefix(mw.currentStorageDir, "/mnt/sdcard/") {
			sdUsed, sdTotal = currentUsed, mw.currentStorageTotal
		} else {
			internalUsed, internalTotal = currentUsed, mw.currentStorageTotal
		}

		if internalTotal > 0 {
			internalValue = models.FormatStorageSizeOnly(internalUsed, internalTotal)
			internalPercent = formatStoragePercent(internalUsed, internalTotal)
		}
		if sdTotal > 0 {
			sdValue = models.FormatStorageSizeOnly(sdUsed, sdTotal)
			sdPercent = formatStoragePercent(sdUsed, sdTotal)
		}
	}

	buildBlock := func(title, value, percent string) fyne.CanvasObject {
		titleText := view.NewBrandText(strings.ToUpper(title), 11, design.ColorTextMuted, true)
		valueText := view.NewBrandText(value, 16, design.ColorTextLight, true)
		percentText := view.NewBrandText(percent, 16, design.ColorAccent, true)
		return view.NewCompactSurfacePanel(
			view.NewInset(container.NewVBox(
				titleText,
				view.NewInset(container.NewHBox(
					valueText,
					layout.NewSpacer(),
					percentText,
				), 0, 0, 6, 0),
			), 12, 12, 12, 12),
			design.ColorGray950,
			design.RadiusMD,
		)
	}

	var popup *widget.PopUp
	closePopup := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	title := view.NewBrandText("Storage Info", 19, design.ColorTextLight, true)
	title.Alignment = fyne.TextAlignCenter
	closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), closePopup)
	closeBtn.Importance = widget.LowImportance
	titleBar := container.NewBorder(nil, nil, nil, closeBtn, container.NewCenter(title))

	okBtn := widget.NewButton("OK", closePopup)
	okBtn.Importance = widget.MediumImportance

	body := container.NewBorder(
		nil,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(160, 44), okBtn)),
		nil,
		nil,
		container.NewVBox(
			titleBar,
			view.NewInset(container.NewVBox(
				buildBlock(internalTitle, internalValue, internalPercent),
				view.NewInset(buildBlock(sdTitle, sdValue, sdPercent), 0, 0, 10, 0),
			), 0, 0, 16, 8),
		),
	)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	panel := container.NewStack(
		bg,
		view.NewInset(body, 18, 18, 16, 16),
		border,
	)

	popup = view.NewOverlayPopup(mw.window, view.OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := float32(24)
			maxWidth := canvasSize.Width - margin*2
			maxHeight := canvasSize.Height - margin*2
			if maxWidth <= 0 {
				maxWidth = canvasSize.Width
			}
			if maxHeight <= 0 {
				maxHeight = canvasSize.Height
			}
			panelMin := panel.MinSize()
			panelWidth := minFloat32(maxFloat32(panelMin.Width, 420), maxWidth)
			panelHeight := minFloat32(maxFloat32(panelMin.Height, 280), maxHeight)
			return fyne.NewSize(panelWidth, panelHeight)
		},
	})
	popup.Show()
}

func formatStoragePercent(used, total int64) string {
	if total <= 0 {
		return "0%"
	}
	pct := int(float64(used) * 100 / float64(total))
	if pct < 0 {
		pct = 0
	}
	return fmt.Sprintf("%d%%", pct)
}
