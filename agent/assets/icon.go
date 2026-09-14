package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed icons/Icon.png
var iconPNG []byte

var AppIcon = fyne.NewStaticResource("Icon.png", iconPNG)

// TrayIconPNG is a smaller base image for the system tray icon (composited
// with a status dot at runtime by internal/ui/tray.go) — the systray host
// resizes to a small footprint (16-32px) anyway, so starting from a 32x32
// source avoids the driver downscaling AppIcon's 512x512 original for every
// status variant.
//
//go:embed icons/appicon-32.png
var TrayIconPNG []byte
