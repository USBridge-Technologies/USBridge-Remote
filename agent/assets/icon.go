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
	proIconFill    = "#9c58f9"
	onProIconFill  = "#f5f5f5"
)

// GitHubIcon is the octocat mark for the USB driver chip (muted chrome).
// GitHubIconTeal is the Info menu's Software/Hardware row.
var GitHubIcon = tintedSVG("github.svg", githubSVG, chipIconFill)
var GitHubIconTeal = tintedSVG("github-teal.svg", githubSVG, headerIconFill)

// LanguageIconTeal / InfoIconTeal / OpenExternalIconTeal are the settings
// and Info menus (teal, same as the client's header dropdowns).
var (
	LanguageIconTeal     = tintedSVG("language-teal.svg", languageSVG, headerIconFill)
	InfoIconTeal         = tintedSVG("info-teal.svg", infoSVG, headerIconFill)
	OpenExternalIconTeal = tintedSVG("open-external-teal.svg", openExternalSVG, headerIconFill)
)

// GoogleLogo is the colorful G for the Account "Log in with Google" chip.
var GoogleLogo = fyne.NewStaticResource("google-logo.webp", googleLogoBytes)

// StarProIcon is the Protocol-card Pro/Enterprise glyph (#9c58f9).
// StarChipIcon matches Change-chip chrome; StarOnProIcon is the hover fill.
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
