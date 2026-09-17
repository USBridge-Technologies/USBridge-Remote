//go:build !windows

package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed Icon.png
var AppIconBytes []byte

// AppIcon is the application icon on macOS, Linux, and mobile.
var AppIcon = fyne.NewStaticResource("Icon.png", AppIconBytes)
