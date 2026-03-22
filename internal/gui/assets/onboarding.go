package assets

import (
	_ "embed"
	"fmt"
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
	//go:embed onboarding/Front_panel.png
	onboardingStep01 []byte
	//go:embed onboarding/Front_panel1.png
	onboardingStep02 []byte
	//go:embed onboarding/Front_panel2.png
	onboardingStep03 []byte
)

var (
	ArrowLeftGray     = fyne.NewStaticResource("arrow-left-gray.svg", colorizeArrow(arrowRightIcon, "#353535", true))
	ArrowLeftWhite    = fyne.NewStaticResource("arrow-left-white.svg", colorizeArrow(arrowRightIcon, "#656565", true))
	ArrowRightGray    = fyne.NewStaticResource("arrow-right-gray.svg", colorizeArrow(arrowRightIcon, "#353535", false))
	ArrowRightWhite   = fyne.NewStaticResource("arrow-right-white.svg", colorizeArrow(arrowRightIcon, "#656565", false))
	DiscordIconDim    = fyne.NewStaticResource("message-chat-square-svgrepo-com-dim.svg", recolorStrokeIcon(messageChatSquareIcon, "#8E8E8E", "1.9"))
	DiscordIcon       = fyne.NewStaticResource("message-chat-square-svgrepo-com.svg", recolorStrokeIcon(messageChatSquareIcon, "#F5F5F5", "1.9"))
	LanguageIconDim   = fyne.NewStaticResource("language-svgrepo-com-dim.svg", recolorFillIcon(languageIcon, "#8E8E8E"))
	LanguageIconMuted = fyne.NewStaticResource("language-svgrepo-com-muted.svg", recolorFillIcon(languageIcon, "#C9C9C9"))
	LanguageIcon      = fyne.NewStaticResource("language-svgrepo-com.svg", recolorFillIcon(languageIcon, "#F5F5F5"))
	LoadingGrayFrames = buildLoadingFrames(loadingIcon, "#111111")
	QuestionIconDim   = fyne.NewStaticResource("question-svgrepo-com-dim.svg", recolorStrokeIcon(questionIcon, "#8E8E8E", "2.6"))
	QuestionIconMuted = fyne.NewStaticResource("question-svgrepo-com-muted.svg", recolorStrokeIcon(questionIcon, "#C9C9C9", "2.6"))
	QuestionIcon      = fyne.NewStaticResource("question-svgrepo-com.svg", recolorStrokeIcon(questionIcon, "#F5F5F5", "2.6"))
	QRCodeIcon        = fyne.NewStaticResource("Qr-Code--Streamline-Atlas.svg", qrCodeIcon)
	QRCodeMuted       = fyne.NewStaticResource("Qr-Code--Streamline-Atlas-muted.svg", recolorStrokeIcon(qrCodeIcon, "#656565", "1.3"))
	QRCodeLight       = fyne.NewStaticResource("Qr-Code--Streamline-Atlas-light.svg", recolorStrokeIcon(qrCodeIcon, "#C9C9C9", "1.3"))
	QRCodeAccent      = fyne.NewStaticResource("Qr-Code--Streamline-Atlas-accent.svg", recolorStrokeIcon(qrCodeIcon, "#b6ea93", "1.3"))
	QRCodeBoldBlack   = fyne.NewStaticResource("Qr-Code--Streamline-Atlas-black.svg", recolorStrokeIcon(qrCodeIcon, "#111111", "1.3"))
	ServerConnectIcon = fyne.NewStaticResource("Server-Connect--Streamline-Atlas.svg", serverConnectIcon)
	ServerConnectBold = fyne.NewStaticResource("Server-Connect--Streamline-Atlas-bold.svg", boldenServerIcon(serverConnectIcon, "#111111", "1.9"))
	ServerConnectGlow = fyne.NewStaticResource("Server-Connect--Streamline-Atlas-accent-hover.svg", boldenServerIcon(serverConnectIcon, "#b6ea93", "1.9"))
	OnboardingStep01  = fyne.NewStaticResource("Front_panel.png", onboardingStep01)
	OnboardingStep02  = fyne.NewStaticResource("Front_panel1.png", onboardingStep02)
	OnboardingStep03  = fyne.NewStaticResource("Front_panel2.png", onboardingStep03)
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
	return []byte(strings.ReplaceAll(string(source), "#000000", fill))
}

func recolorStrokeIcon(source []byte, stroke string, width string) []byte {
	svg := strings.ReplaceAll(string(source), `stroke="#000000"`, fmt.Sprintf(`stroke="%s"`, stroke))
	svg = strings.ReplaceAll(svg, `stroke-width="1"`, fmt.Sprintf(`stroke-width="%s"`, width))
	svg = strings.ReplaceAll(svg, `stroke-width="2"`, fmt.Sprintf(`stroke-width="%s"`, width))
	return []byte(svg)
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
