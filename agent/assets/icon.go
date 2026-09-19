package assets

import (
	_ "embed"
	"strings"

	"fyne.io/fyne/v2"
	_ "golang.org/x/image/webp"
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

//go:embed LogoUSBridge2.0-open.svg
var logoUSBridgeOpen []byte

//go:embed LogoUSBridge2.0-free.svg
var logoUSBridgeFree []byte

//go:embed LogoUSBridge2.0-pro.svg
var logoUSBridgePro []byte

// LogoUSBridgeLockup* are the header lockups per protocol theme.
var (
	LogoUSBridgeLockupOpen = fyne.NewStaticResource("LogoUSBridge2.0-open.svg", logoUSBridgeOpen)
	LogoUSBridgeLockupFree = fyne.NewStaticResource("LogoUSBridge2.0-free.svg", logoUSBridgeFree)
	LogoUSBridgeLockupPro  = fyne.NewStaticResource("LogoUSBridge2.0-pro.svg", logoUSBridgePro)
	LogoUSBridgeLockup     = LogoUSBridgeLockupFree
)

//go:embed github-svgrepo-com.svg
var githubSVG []byte

//go:embed language-svgrepo-com.svg
var languageSVG []byte

//go:embed info-svgrepo-com.svg
var infoSVG []byte

//go:embed open-external-svgrepo-com.svg
var openExternalSVG []byte

//go:embed windows-svgrepo-com.svg
var windowsSVG []byte

//go:embed macos-svgrepo-com.svg
var macosSVG []byte

//go:embed linux-svgrepo-com.svg
var linuxSVG []byte

//go:embed star-svgrepo-com.svg
var starSVG []byte

//go:embed google-logo.webp
var googleLogoBytes []byte

const (
	headerIconFill = "#41e0c3"
	chipIconFill   = "#c3c6b4"
	proIconFill    = "#b39ef1"
	onProIconFill  = "#0b0f12"
)

// settingsGearSVG is a filled gear so tintedSVG can recolor it the same
// way as language/info.
var settingsGearSVG = []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#000000" d="M19.14 12.94c.04-.31.06-.63.06-.94s-.02-.63-.06-.94l2.03-1.58a.49.49 0 0 0 .12-.61l-1.92-3.32a.49.49 0 0 0-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94L14.4 2.81A.49.49 0 0 0 13.92 2.4h-3.84a.49.49 0 0 0-.48.41L9.25 5.35c-.59.24-1.13.57-1.62.94L5.24 5.33a.49.49 0 0 0-.59.22L2.74 8.87a.49.49 0 0 0 .12.61l2.03 1.58c-.04.31-.06.63-.06.94s.02.63.06.94l-2.03 1.58a.49.49 0 0 0-.12.61l1.92 3.32c.12.22.37.3.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.04.24.24.41.48.41h3.84c.24 0 .44-.17.48-.41l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32a.49.49 0 0 0-.12-.61l-2.03-1.58zM12 15.6A3.6 3.6 0 1 1 12 8.4a3.6 3.6 0 0 1 0 7.2z"/></svg>`)

// GitHubIcon is the octocat mark for the USB driver chip and the light
// settings Info menu (muted chrome).
var GitHubIcon = tintedSVG("github.svg", githubSVG, chipIconFill)

// LanguageIconLight / InfoIconLight / OpenExternalIconLight / SettingsIconLight
// are the settings dropdown (light chrome, not teal).
var (
	LanguageIconLight     = tintedSVG("language-light.svg", languageSVG, chipIconFill)
	InfoIconLight         = tintedSVG("info-light.svg", infoSVG, chipIconFill)
	OpenExternalIconLight = tintedSVG("open-external-light.svg", openExternalSVG, chipIconFill)
	SettingsIconLight     = tintedSVG("settings-light.svg", settingsGearSVG, chipIconFill)
)

// GoogleLogo is the colorful G for the Account "Log in with Google" chip.
var GoogleLogo = fyne.NewStaticResource("google-logo.webp", googleLogoBytes)

// StarProIcon is the Protocol-card Pro/Enterprise glyph (#b39ef1).
// StarChipIcon matches Change-chip chrome; StarOnProIcon is the Buy fill.
var (
	StarProIcon   = tintedStar("star-pro.svg", starSVG, proIconFill)
	StarChipIcon  = tintedStar("star-chip.svg", starSVG, chipIconFill)
	StarOnProIcon = tintedStar("star-on-pro.svg", starSVG, onProIconFill)
)

// WindowsIcon, MacOSIcon, LinuxIcon are the Status-card header glyphs
// (teal, same as the other panel icons).
var (
	WindowsIcon = tintedSVG("windows.svg", windowsSVG, headerIconFill)
	MacOSIcon   = tintedSVG("macos.svg", macosSVG, headerIconFill)
	LinuxIcon   = tintedSVG("linux.svg", linuxSVG, headerIconFill)
)

func tintedSVG(name string, src []byte, fill string) fyne.Resource {
	s := string(src)
	s = strings.ReplaceAll(s, `fill="#000000"`, `fill="`+fill+`"`)
	s = strings.ReplaceAll(s, `fill="#0F0F0F"`, `fill="`+fill+`"`)
	return fyne.NewStaticResource(name, []byte(s))
}

func tintedStar(name string, src []byte, fill string) fyne.Resource {
	s := string(src)
	s = strings.ReplaceAll(s, `fill="none"`, `fill="`+fill+`"`)
	s = strings.ReplaceAll(s, `stroke="#000000"`, `stroke="`+fill+`"`)
	return fyne.NewStaticResource(name, []byte(s))
}
