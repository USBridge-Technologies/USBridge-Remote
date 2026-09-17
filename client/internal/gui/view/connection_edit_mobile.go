package view

import "fyne.io/fyne/v2"

// Phone edit chrome is larger than the desktop card/panel: fields fill
// the row (no 160px right-anchored gap after the label), and the
// copy/paste + Delete/Cancel/Apply hits stay finger-sized.

func connectionEditActionButtonSize() fyne.Size {
	if UseMobileConnections() {
		return fyne.NewSize(34, 34)
	}
	return fyne.NewSize(26, 26)
}

func connectionEditActionIconSize() fyne.Size {
	if UseMobileConnections() {
		return fyne.NewSize(16, 16)
	}
	return fyne.NewSize(13, 13)
}

func connectionEditCancelIconSize() fyne.Size {
	if UseMobileConnections() {
		return fyne.NewSize(14, 14)
	}
	return fyne.NewSize(11, 11)
}

func connectionEditNameTextSize(desktop float32) float32 {
	if UseMobileConnections() {
		return desktop + 2
	}
	return desktop
}

func connectionEditFieldActionButtonSize() float32 {
	if UseMobileConnections() {
		return 24
	}
	return 15
}

func connectionEditFieldActionIconSize() float32 {
	if UseMobileConnections() {
		return 14
	}
	return 9
}

func connectionEditStatLabelSize() float32 {
	if UseMobileConnections() {
		return 12
	}
	return 10
}

func connectionEditEntryTextSize(desktop float32) float32 {
	if UseMobileConnections() {
		return desktop + 2
	}
	return desktop
}

func connectionEditEntryWidth(requested float32) float32 {
	if UseMobileConnections() {
		return 0
	}
	return requested
}

func connectionEditActionsColWidth() float32 {
	return connectionEditFieldActionButtonSize() * 2
}

func connectionEditLabelColWidth() float32 {
	return fyne.MeasureText("Token", connectionEditStatLabelSize(), fyne.TextStyle{Monospace: true}).Width + 8
}
