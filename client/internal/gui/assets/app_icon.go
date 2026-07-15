package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed Icon.png
var AppIconBytes []byte

// AppIcon is the main application icon
var AppIcon = fyne.NewStaticResource("Icon.png", AppIconBytes)
