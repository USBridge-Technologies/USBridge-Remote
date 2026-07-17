//go:build !android && !ios
// +build !android,!ios

package controller

import (
	"image"
	"image/color"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/makiuchi-d/gozxing"
)

// Shared popup UI for the desktop QR camera scanner. Used by both the
// gstreamer-based capture (macOS/Linux) and the Media Foundation-based
// capture (Windows) — the layout has no platform-specific behavior.

func showEmbeddedQRScannerPopup(parent fyne.Window, videoImg *canvas.Image, onClose func()) *widget.PopUp {
	title := canvas.NewText("Scan device qr code", design.ColorTextLight)
	title.TextSize = 18
	title.TextStyle.Bold = true
	title.Alignment = fyne.TextAlignCenter

	closeBtn := newConnectionDialogIconButton(theme.CancelIcon(), onClose)

	header := container.New(&qrScannerHeaderLayout{}, title, closeBtn)

	videoBg := canvas.NewRectangle(color.NRGBA{R: 0x08, G: 0x08, B: 0x08, A: 0xf2})
	videoBg.CornerRadius = 14

	videoBorder := canvas.NewRectangle(color.Transparent)
	videoBorder.CornerRadius = 14
	videoBorder.StrokeColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x18}
	videoBorder.StrokeWidth = 1

	videoCard := container.NewStack(
		videoBg,
		view.NewInset(container.NewMax(videoImg), 14, 14, 14, 14),
		videoBorder,
	)

	cardBg := canvas.NewRectangle(color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xe9})
	cardBg.CornerRadius = 18
	cardBg.StrokeColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x22}
	cardBg.StrokeWidth = 1

	card := container.NewStack(
		cardBg,
		container.New(&qrScannerCardLayout{}, header, videoCard),
	)

	return view.NewOverlayPopup(parent, view.OverlayPopupSpec{
		Panel:    card,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, _ fyne.CanvasObject) fyne.Size {
			return qrScannerPanelSize(canvasSize)
		},
	})
}

type qrScannerCardLayout struct{}

func (l *qrScannerCardLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	header := objects[0]
	video := objects[1]

	padding := clampFloat32(minFloat32(size.Width, size.Height)*0.04, 18, 26)
	headerHeight := float32(40)
	headerWidth := size.Width - padding*2
	if headerWidth < 0 {
		headerWidth = 0
	}

	header.Move(fyne.NewPos(padding, padding))
	header.Resize(fyne.NewSize(headerWidth, headerHeight))

	videoTop := padding + headerHeight + 14
	availableWidth := size.Width - padding*2
	availableHeight := size.Height - videoTop - padding
	if availableWidth < 0 {
		availableWidth = 0
	}
	if availableHeight < 0 {
		availableHeight = 0
	}

	videoWidth := availableWidth
	videoHeight := videoWidth * 3 / 4
	if videoHeight > availableHeight {
		videoHeight = availableHeight
		videoWidth = videoHeight * 4 / 3
	}
	if videoWidth > availableWidth {
		videoWidth = availableWidth
		videoHeight = videoWidth * 3 / 4
	}
	if videoWidth < 0 {
		videoWidth = 0
	}
	if videoHeight < 0 {
		videoHeight = 0
	}

	video.Move(fyne.NewPos((size.Width-videoWidth)/2, videoTop+(availableHeight-videoHeight)/2))
	video.Resize(fyne.NewSize(videoWidth, videoHeight))
}

func (l *qrScannerCardLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(320, 280)
}

type qrScannerHeaderLayout struct{}

func (l *qrScannerHeaderLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	title := objects[0]
	closeBtn := objects[1]

	closeSize := fyne.NewSize(40, 40)
	closeX := size.Width - closeSize.Width
	if closeX < 0 {
		closeX = 0
	}
	closeBtn.Move(fyne.NewPos(closeX, 0))
	closeBtn.Resize(closeSize)

	title.Move(fyne.NewPos(0, 0))
	title.Resize(size)
}

func (l *qrScannerHeaderLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}

	titleMin := objects[0].MinSize()
	closeMin := objects[1].MinSize()
	height := maxFloat32(titleMin.Height, closeMin.Height)
	width := maxFloat32(titleMin.Width, closeMin.Width*2)
	return fyne.NewSize(width, height)
}

func qrScannerPanelSize(canvasSize fyne.Size) fyne.Size {
	margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 32)
	maxWidth := canvasSize.Width - margin*2
	maxHeight := canvasSize.Height - margin*2
	if maxWidth <= 0 {
		maxWidth = canvasSize.Width
	}
	if maxHeight <= 0 {
		maxHeight = canvasSize.Height
	}

	padding := clampFloat32(minFloat32(maxWidth, maxHeight)*0.04, 18, 26)
	headerHeight := float32(40)
	gap := float32(14)
	videoMaxWidth := maxFloat32(0, maxWidth-padding*2)
	videoMaxHeight := maxFloat32(0, maxHeight-padding*2-headerHeight-gap)

	videoWidth := minFloat32(680, videoMaxWidth)
	videoHeight := videoWidth * 3 / 4
	if videoHeight > videoMaxHeight {
		videoHeight = videoMaxHeight
		videoWidth = videoHeight * 4 / 3
	}
	if videoWidth > videoMaxWidth {
		videoWidth = videoMaxWidth
		videoHeight = videoWidth * 3 / 4
	}

	panelMinWidth := minFloat32(320, maxWidth)
	panelMinHeight := minFloat32(260, maxHeight)
	panelWidth := clampFloat32(videoWidth+padding*2, panelMinWidth, maxWidth)
	panelHeight := clampFloat32(padding*2+headerHeight+gap+videoHeight, panelMinHeight, maxHeight)
	return fyne.NewSize(panelWidth, panelHeight)
}

// decodeQRImage tries to find and decode a QR code in img, returning its text on success.
func decodeQRImage(reader gozxing.Reader, img image.Image) (string, bool) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", false
	}
	result, err := reader.Decode(bmp, nil)
	if err != nil {
		return "", false
	}
	return result.GetText(), true
}
