package gui

import (
	"fmt"
	"strings"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
)

// showStorageInfoDialog pops up a small, informational-only dropdown under
// the header's storage chip (mw.sdStorageProgress) -- Internal Storage and
// SD Card, each with a "used/total GB" + percent line, in the same popup
// shell as this header's other dropdowns (view.ShowStyledInfoDropdown) --
// no rows to select, no hover reaction, just the numbers. Replaces the old
// modal (view.NewOverlayPopup) version.
func (mw *MainWindow) showStorageInfoDialog() {
	if mw == nil || mw.window == nil || mw.sdStorageProgress == nil {
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

	// Title (not bold, smaller than the value/percent line -- an eyebrow
	// label, not something meant to compete with the actual numbers) vs.
	// value/percent still at 10px, matching every other header dropdown's
	// own row text (ShowStyledMenuTeal).
	buildBlock := func(title, value, percent string) fyne.CanvasObject {
		titleText := view.NewBrandText(strings.ToUpper(title), 8, design.ColorConnectionBadgeText, false)
		valueText := view.NewBrandText(value, 10, design.ColorTextLight, true)
		percentText := view.NewBrandText(percent, 10, design.ColorTextLight, true)
		return container.NewVBox(
			titleText,
			view.NewInset(container.NewHBox(valueText, layout.NewSpacer(), percentText), 0, 0, 0, 0),
		)
	}

	divider := canvas.NewRectangle(design.ColorStatusBarDivider)
	divider.SetMinSize(fyne.NewSize(0, 1))

	content := container.NewVBox(
		buildBlock(internalTitle, internalValue, internalPercent),
		view.NewInset(divider, 0, 0, 4, 4),
		buildBlock(sdTitle, sdValue, sdPercent),
	)

	view.ShowStyledInfoDropdown(mw.sdStorageProgress, content, 180)
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
