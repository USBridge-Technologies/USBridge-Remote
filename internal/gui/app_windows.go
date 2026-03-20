//go:build windows

package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

// На Windows не используем app.NewWithID, чтобы не восстанавливать потенциально
// "битые" сохранённые координаты окна ( off‑screen / невидимое окно ).
func newFyneApp() fyne.App {
	return app.New()
}
