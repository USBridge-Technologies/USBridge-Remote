package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed Icon.png
var iconPNG []byte

var AppIcon = fyne.NewStaticResource("Icon.png", iconPNG)
