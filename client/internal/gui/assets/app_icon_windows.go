//go:build windows

package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed Icon-windows.png
var AppIconBytes []byte

// AppIcon is the Windows window/taskbar icon: a 256px rounded mark
// (see Icon-windows.png). Other platforms keep Icon.png.
var AppIcon = fyne.NewStaticResource("Icon-windows.png", AppIconBytes)
