package assets

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"fyne.io/fyne/v2"
)

var (
	//go:embed arrow-right-333-svgrepo-com.svg
	arrowRightIcon []byte
	//go:embed language-svgrepo-com.svg
	languageIcon []byte
	//go:embed message-chat-square-svgrepo-com.svg
	messageChatSquareIcon []byte
	//go:embed loading-svgrepo-com.svg
	loadingIcon []byte
	//go:embed question-svgrepo-com.svg
	questionIcon []byte
	//go:embed Qr-Code--Streamline-Atlas.svg
	qrCodeIcon []byte
	//go:embed Server-Connect--Streamline-Atlas.svg
	serverConnectIcon []byte
	//go:embed usb-svgrepo-com.svg
	usbTabIcon []byte
	//go:embed monitor-svgrepo-com.svg
	monitorTabIcon []byte
	//go:embed disk-floppy-save-storage-data-svgrepo-com.svg
	snapshotsTabIcon []byte
	//go:embed folder-svgrepo-com.svg
	folderIcon []byte
	//go:embed disc-svgrepo-com.svg
	discIcon []byte
	//go:embed upload-svgrepo-com.svg
	uploadIcon []byte
	//go:embed cam-svgrepo-com.svg
	cameraIcon []byte
	//go:embed keyboard-alt-1-svgrepo-com.svg
	keyboardIcon []byte
	//go:embed mouse-svgrepo-com.svg
	mouseIcon []byte
	//go:embed audio-svgrepo-com.svg
	audioIcon []byte
	//go:embed audio-mute-svgrepo-com.svg
	audioMuteIcon []byte
	//go:embed network-backup-svgrepo-com.svg
	networkIcon []byte
	//go:embed sd-card-svgrepo-com.svg
	sdCardIcon []byte
	//go:embed memory-chip-svgrepo-com.svg
	memoryChipIcon []byte
	//go:embed warning-triangle-svgrepo-com.svg
	warningTriangleIcon []byte
	//go:embed warning-svgrepo-com.svg
	warningInfoIcon []byte
	//go:embed info-svgrepo-com.svg
	infoIcon []byte
	//go:embed configuration-vertical-options-svgrepo-com.svg
	configVerticalIcon []byte
	//go:embed connection-internet-network-web-data-storage-svgrepo-com.svg
	connectionStatusIcon []byte
	//go:embed connect-svgrepo-com.svg
	connectIcon []byte
	//go:embed Power-Off-Fill--Streamline-Rounded-Fill-Material-Symbols.svg
	powerOffFillIcon []byte
	//go:embed exit-svgrepo-com.svg
	exitIcon []byte
	//go:embed off.svg
	powerOffIcon []byte
	//go:embed reset.svg
	resetIcon []byte
	//go:embed USBridge.svg
	usbridgeIcon []byte
	//go:embed linux-svgrepo-com.svg
	linuxOSIcon []byte
	//go:embed windows-svgrepo-com.svg
	windowsOSIcon []byte
	//go:embed macos-svgrepo-com.svg
	macosOSIcon []byte
	//go:embed onboarding/Front_panel.png
	onboardingStep01 []byte
	//go:embed onboarding/Front_panel1.png
	onboardingStep02 []byte
	//go:embed onboarding/Front_panel2.png
	onboardingStep03 []byte
)

var (
	ArrowLeftGray              = fyne.NewStaticResource("arrow-left-gray.svg", colorizeArrow(arrowRightIcon, "#353535", true))
	ArrowLeftWhite             = fyne.NewStaticResource("arrow-left-white.svg", colorizeArrow(arrowRightIcon, "#656565", true))
	ArrowRightGray             = fyne.NewStaticResource("arrow-right-gray.svg", colorizeArrow(arrowRightIcon, "#353535", false))
	ArrowRightWhite            = fyne.NewStaticResource("arrow-right-white.svg", colorizeArrow(arrowRightIcon, "#656565", false))
	DiscordIconDim             = fyne.NewStaticResource("message-chat-square-svgrepo-com-dim.svg", recolorStrokeIcon(messageChatSquareIcon, "#8E8E8E", "1.9"))
	DiscordIcon                = fyne.NewStaticResource("message-chat-square-svgrepo-com.svg", recolorStrokeIcon(messageChatSquareIcon, "#F5F5F5", "1.9"))
	DiscordIconActive          = fyne.NewStaticResource("message-chat-square-svgrepo-com-active.svg", recolorStrokeIcon(messageChatSquareIcon, "#93C572", "1.9"))
	LanguageIconDim            = fyne.NewStaticResource("language-svgrepo-com-dim.svg", recolorFillIcon(languageIcon, "#8E8E8E"))
	LanguageIconMuted          = fyne.NewStaticResource("language-svgrepo-com-muted.svg", recolorFillIcon(languageIcon, "#C9C9C9"))
	LanguageIcon               = fyne.NewStaticResource("language-svgrepo-com.svg", recolorFillIcon(languageIcon, "#F5F5F5"))
	LanguageIconActive         = fyne.NewStaticResource("language-svgrepo-com-active.svg", recolorFillIcon(languageIcon, "#93C572"))
	LoadingGrayFrames          = buildLoadingFrames(loadingIcon, "#111111")
	QuestionIconDim            = fyne.NewStaticResource("question-svgrepo-com-dim.svg", recolorStrokeIcon(questionIcon, "#8E8E8E", "2.6"))
	QuestionIconMuted          = fyne.NewStaticResource("question-svgrepo-com-muted.svg", recolorStrokeIcon(questionIcon, "#C9C9C9", "2.6"))
	QuestionIcon               = fyne.NewStaticResource("question-svgrepo-com.svg", recolorStrokeIcon(questionIcon, "#F5F5F5", "2.6"))
	QRCodeIcon                 = fyne.NewStaticResource("Qr-Code--Streamline-Atlas.svg", qrCodeIcon)
	QRCodeMuted                = fyne.NewStaticResource("Qr-Code--Streamline-Atlas-muted.svg", recolorStrokeIcon(qrCodeIcon, "#656565", "1.3"))
	QRCodeLight                = fyne.NewStaticResource("Qr-Code--Streamline-Atlas-light.svg", recolorStrokeIcon(qrCodeIcon, "#C9C9C9", "1.3"))
	QRCodeAccent               = fyne.NewStaticResource("Qr-Code--Streamline-Atlas-accent.svg", recolorStrokeIcon(qrCodeIcon, "#b6ea93", "1.3"))
	QRCodeBoldBlack            = fyne.NewStaticResource("Qr-Code--Streamline-Atlas-black.svg", recolorStrokeIcon(qrCodeIcon, "#111111", "1.3"))
	ServerConnectIcon          = fyne.NewStaticResource("Server-Connect--Streamline-Atlas.svg", serverConnectIcon)
	ServerConnectMuted         = fyne.NewStaticResource("Server-Connect--Streamline-Atlas-muted.svg", boldenServerIcon(serverConnectIcon, "#656565", "1.9"))
	ServerConnectBold          = fyne.NewStaticResource("Server-Connect--Streamline-Atlas-bold.svg", boldenServerIcon(serverConnectIcon, "#111111", "1.9"))
	ServerConnectGlow          = fyne.NewStaticResource("Server-Connect--Streamline-Atlas-accent-hover.svg", boldenServerIcon(serverConnectIcon, "#b6ea93", "1.9"))
	USBTabIcon                 = fyne.NewStaticResource("usb-svgrepo-com.svg", recolorFillIcon(usbTabIcon, "#F5F5F5"))
	USBTabIconActive           = fyne.NewStaticResource("usb-svgrepo-com-active.svg", recolorFillIcon(usbTabIcon, "#93C572"))
	MonitorTabIcon             = fyne.NewStaticResource("monitor-svgrepo-com.svg", recolorStrokeIcon(monitorTabIcon, "#F5F5F5", "1.9"))
	MonitorTabIconActive       = fyne.NewStaticResource("monitor-svgrepo-com-active.svg", recolorStrokeIcon(monitorTabIcon, "#93C572", "1.9"))
	SnapshotsTabIcon           = fyne.NewStaticResource("disk-floppy-save-storage-data-svgrepo-com.svg", recolorFillIcon(snapshotsTabIcon, "#F5F5F5"))
	SnapshotsTabIconActive     = fyne.NewStaticResource("disk-floppy-save-storage-data-svgrepo-com-active.svg", recolorFillIcon(snapshotsTabIcon, "#93C572"))
	FolderIcon                 = fyne.NewStaticResource("folder-svgrepo-com.svg", recolorFillIcon(folderIcon, "#C9C9C9"))
	FolderIconActive           = fyne.NewStaticResource("folder-svgrepo-com-active.svg", recolorFillIcon(folderIcon, "#93C572"))
	DiscIcon                   = fyne.NewStaticResource("disc-svgrepo-com.svg", recolorFillIcon(discIcon, "#C9C9C9"))
	DiscIconActive             = fyne.NewStaticResource("disc-svgrepo-com-active.svg", recolorFillIcon(discIcon, "#93C572"))
	UploadIcon                 = fyne.NewStaticResource("upload-svgrepo-com.svg", recolorStrokeIcon(uploadIcon, "#F5F5F5", "2"))
	UploadIconMuted            = fyne.NewStaticResource("upload-svgrepo-com-muted.svg", recolorStrokeIcon(uploadIcon, "#8E8E8E", "2"))
	CameraIcon                 = fyne.NewStaticResource("cam-svgrepo-com.svg", recolorStrokeIcon(cameraIcon, "#C9C9C9", "1.8"))
	CameraIconActive           = fyne.NewStaticResource("cam-svgrepo-com-active.svg", recolorStrokeIcon(cameraIcon, "#93C572", "1.8"))
	KeyboardIcon               = fyne.NewStaticResource("keyboard-alt-1-svgrepo-com.svg", recolorStrokeIcon(keyboardIcon, "#C9C9C9", "1.8"))
	KeyboardIconActive         = fyne.NewStaticResource("keyboard-alt-1-svgrepo-com-active.svg", recolorStrokeIcon(keyboardIcon, "#93C572", "1.8"))
	MouseIcon                  = fyne.NewStaticResource("mouse-svgrepo-com.svg", recolorFillIcon(mouseIcon, "#C9C9C9"))
	MouseIconActive            = fyne.NewStaticResource("mouse-svgrepo-com-active.svg", recolorFillIcon(mouseIcon, "#93C572"))
	AudioIcon                  = fyne.NewStaticResource("audio-svgrepo-com.svg", recolorFillIcon(audioIcon, "#C9C9C9"))
	AudioIconActive            = fyne.NewStaticResource("audio-svgrepo-com-active.svg", recolorFillIcon(audioIcon, "#93C572"))
	AudioMuteIcon              = fyne.NewStaticResource("audio-mute-svgrepo-com.svg", recolorFillIcon(audioMuteIcon, "#C9C9C9"))
	AudioMuteIconActive        = fyne.NewStaticResource("audio-mute-svgrepo-com-active.svg", recolorFillIcon(audioMuteIcon, "#d66d6d"))
	NetworkIcon                = fyne.NewStaticResource("network-backup-svgrepo-com.svg", recolorFillIcon(networkIcon, "#C9C9C9"))
	NetworkIconActive          = fyne.NewStaticResource("network-backup-svgrepo-com-active.svg", recolorFillIcon(networkIcon, "#93C572"))
	SDCardIcon                 = fyne.NewStaticResource("sd-card-svgrepo-com.svg", recolorFillIcon(sdCardIcon, "#C9C9C9"))
	SDCardIconActive           = fyne.NewStaticResource("sd-card-svgrepo-com-active.svg", recolorFillIcon(sdCardIcon, "#93C572"))
	MemoryChipIcon             = fyne.NewStaticResource("memory-chip-svgrepo-com.svg", recolorMemoryChipIcon(memoryChipIcon, "#F5F5F5"))
	WarningTriangleIcon        = fyne.NewStaticResource("warning-triangle-svgrepo-com.svg", recolorFillIcon(warningTriangleIcon, "#F2C14E"))
	WarningInfoIcon            = fyne.NewStaticResource("warning-svgrepo-com.svg", recolorFillIcon(warningInfoIcon, "#4DA3FF"))
	InfoIcon                   = fyne.NewStaticResource("info-svgrepo-com.svg", recolorFillIcon(infoIcon, "#F5F5F5"))
	InfoIconMuted              = fyne.NewStaticResource("info-svgrepo-com-muted.svg", recolorFillIcon(infoIcon, "#8E8E8E"))
	ConfigVerticalIcon         = fyne.NewStaticResource("configuration-vertical-options-svgrepo-com.svg", recolorFillIcon(configVerticalIcon, "#F5F5F5"))
	ConnectionStatusIcon       = fyne.NewStaticResource("connection-internet-network-web-data-storage-svgrepo-com.svg", recolorFillIcon(connectionStatusIcon, "#C9C9C9"))
	ConnectionStatusIconActive = fyne.NewStaticResource("connection-internet-network-web-data-storage-svgrepo-com-active.svg", recolorFillIcon(connectionStatusIcon, "#93C572"))
	ConnectIcon                = fyne.NewStaticResource("connect-svgrepo-com.svg", recolorFillIcon(connectIcon, "#93C572"))
	ConnectIconBoldBlack       = fyne.NewStaticResource("connect-svgrepo-com-black.svg", recolorFillIcon(connectIcon, "#111111"))
	ConnectIconMuted           = fyne.NewStaticResource("connect-svgrepo-com-muted.svg", recolorFillIcon(connectIcon, "#8E8E8E"))
	PowerOffFillRoundIcon      = fyne.NewStaticResource("Power-Off-Fill--Streamline-Rounded-Fill-Material-Symbols.svg", recolorFillIcon(powerOffFillIcon, "#F5F5F5"))
	PowerOffFillRoundIconMuted = fyne.NewStaticResource("Power-Off-Fill--Streamline-Rounded-Fill-Material-Symbols-muted.svg", recolorFillIcon(powerOffFillIcon, "#8E8E8E"))
	ExitIcon                   = fyne.NewStaticResource("exit-svgrepo-com.svg", recolorStrokeIcon(exitIcon, "#d66d6d", "2"))
	PowerOffIcon               = fyne.NewStaticResource("off.svg", recolorFillIcon(powerOffIcon, "#F5F5F5"))
	PowerOffIconActive         = fyne.NewStaticResource("off-active.svg", recolorFillIcon(powerOffIcon, "#111111"))
	ResetIcon                  = fyne.NewStaticResource("reset.svg", recolorFillIcon(resetIcon, "#F5F5F5"))
	ResetIconActive            = fyne.NewStaticResource("reset-active.svg", recolorFillIcon(resetIcon, "#111111"))
	ConnectionEditIconMuted    = fyne.NewStaticResource("connection-edit-muted.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#4E4E4E"><path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zm2.92 2.33H5v-.92l9.06-9.06.92.92L5.92 19.58zM20.71 7.04a1.003 1.003 0 0 0 0-1.42L18.37 3.29a1.003 1.003 0 0 0-1.42 0l-1.13 1.13 3.75 3.75 1.14-1.13z"/></svg>`))
	USBridgeOSIcon             = fyne.NewStaticResource("USBridge-os.svg", recolorMonoIcon(usbridgeIcon, "#C9C9C9", "1.8"))
	LinuxOSIcon                = fyne.NewStaticResource("linux-svgrepo-com-os.svg", recolorMonoIcon(linuxOSIcon, "#C9C9C9", "1.8"))
	WindowsOSIcon              = fyne.NewStaticResource("windows-svgrepo-com-os.svg", recolorMonoIcon(windowsOSIcon, "#C9C9C9", "1.8"))
	MacOSIcon                  = fyne.NewStaticResource("macos-svgrepo-com-os.svg", recolorMonoIcon(macosOSIcon, "#C9C9C9", "1.8"))
	OnboardingStep01           = fyne.NewStaticResource("Front_panel.png", onboardingStep01)
	OnboardingStep02           = fyne.NewStaticResource("Front_panel1.png", onboardingStep02)
	OnboardingStep03           = fyne.NewStaticResource("Front_panel2.png", onboardingStep03)
)

func colorizeArrow(source []byte, fill string, mirror bool) []byte {
	svg := strings.ReplaceAll(string(source), "#000000", fill)
	if !mirror {
		return []byte(svg)
	}

	svg = strings.Replace(svg, `<g id="Page-1"`, `<g transform="translate(11,0) scale(-1,1)" id="Page-1"`, 1)
	return []byte(svg)
}

func recolorFillIcon(source []byte, fill string) []byte {
	svg := strings.ReplaceAll(string(source), "#000000", fill)
	svg = strings.ReplaceAll(svg, "#0F0F0F", fill)
	svg = strings.ReplaceAll(svg, "#222222", fill)
	svg = strings.ReplaceAll(svg, `"black"`, fmt.Sprintf(`"%s"`, fill))
	svg = strings.ReplaceAll(svg, "fill:black", "fill:"+fill)
	return []byte(svg)
}

func recolorMemoryChipIcon(source []byte, fill string) []byte {
	svg := recolorFillIcon(source, fill)
	out := string(svg)
	out = strings.ReplaceAll(out, "<path ", fmt.Sprintf(`<path fill="%s" `, fill))
	return []byte(out)
}

func recolorStrokeIcon(source []byte, stroke string, width string) []byte {
	svg := string(source)
	strokePattern := regexp.MustCompile(`stroke="(?:#000000|#0F0F0F|#222222|black)"`)
	svg = strokePattern.ReplaceAllString(svg, fmt.Sprintf(`stroke="%s"`, stroke))
	strokeWidthPattern := regexp.MustCompile(`stroke-width="[^"]+"`)
	svg = strokeWidthPattern.ReplaceAllString(svg, fmt.Sprintf(`stroke-width="%s"`, width))
	svg = strings.ReplaceAll(svg, "stroke:black", "stroke:"+stroke)
	return []byte(svg)
}

func recolorMonoIcon(source []byte, color string, strokeWidth string) []byte {
	return recolorStrokeIcon(recolorFillIcon(source, color), color, strokeWidth)
}

func boldenServerIcon(source []byte, stroke string, width string) []byte {
	svg := strings.ReplaceAll(string(source), `stroke="#000000"`, fmt.Sprintf(`stroke="%s"`, stroke))
	svg = strings.ReplaceAll(svg, `stroke-width="1"`, fmt.Sprintf(`stroke-width="%s"`, width))
	return []byte(svg)
}

func buildLoadingFrames(_ []byte, fill string) []fyne.Resource {
	type dot struct {
		x float32
		y float32
	}

	dots := []dot{
		{8.0, 1.8},
		{11.95, 3.05},
		{14.2, 8.0},
		{11.95, 12.95},
		{8.0, 14.2},
		{4.05, 12.95},
		{1.8, 8.0},
		{4.05, 3.05},
	}
	alphas := []float32{1.0, 0.82, 0.64, 0.46, 0.32, 0.22, 0.16, 0.12}

	frames := make([]fyne.Resource, len(dots))
	for frame := range frames {
		var sb strings.Builder
		sb.WriteString(`<svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg">`)
		for idx, point := range dots {
			alpha := alphas[(idx-frame+len(dots))%len(dots)]
			sb.WriteString(fmt.Sprintf(
				`<circle cx="%.2f" cy="%.2f" r="1.55" fill="%s" fill-opacity="%.2f"/>`,
				point.x,
				point.y,
				fill,
				alpha,
			))
		}
		sb.WriteString(`</svg>`)
		frames[frame] = fyne.NewStaticResource(
			fmt.Sprintf("loading-gray-%02d.svg", frame),
			[]byte(sb.String()),
		)
	}
	return frames
}
