package view

import (
	"fmt"
	"strconv"

	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// VideoQualityDialog диалог настроек качества видео
type VideoQualityDialog struct {
	parent fyne.Window

	widthEntry   *widget.Entry
	heightEntry  *widget.Entry
	fpsEntry     *widget.Entry
	qualityEntry *widget.Entry
	bitrateEntry *widget.Entry

	onApply func(width, height, fps, quality, bitrate int)
}

// NewVideoQualityDialog создает новый диалог настроек качества
func NewVideoQualityDialog(parent fyne.Window) *VideoQualityDialog {
	return &VideoQualityDialog{parent: parent}
}

// Show показывает диалог настроек качества
func (vqd *VideoQualityDialog) Show(currentWidth, currentHeight, currentFPS, currentQuality, currentBitrate int, onApply func(int, int, int, int, int)) {
	vqd.onApply = onApply

	vqd.widthEntry = widget.NewEntry()
	vqd.widthEntry.SetText(fmt.Sprintf("%d", currentWidth))
	vqd.widthEntry.SetPlaceHolder("640")

	vqd.heightEntry = widget.NewEntry()
	vqd.heightEntry.SetText(fmt.Sprintf("%d", currentHeight))
	vqd.heightEntry.SetPlaceHolder("480")

	vqd.fpsEntry = widget.NewEntry()
	vqd.fpsEntry.SetText(fmt.Sprintf("%d", currentFPS))
	vqd.fpsEntry.SetPlaceHolder("30")

	vqd.qualityEntry = widget.NewEntry()
	vqd.qualityEntry.SetText(fmt.Sprintf("%d", currentQuality))
	vqd.qualityEntry.SetPlaceHolder("80")

	vqd.bitrateEntry = widget.NewEntry()
	vqd.bitrateEntry.SetText(fmt.Sprintf("%d", currentBitrate))
	vqd.bitrateEntry.SetPlaceHolder("2000")

	settingsContainer := container.NewVBox(
		widget.NewLabel(i18n.Current.VideoQualitySettings+":"),
		widget.NewSeparator(),
		container.NewHBox(
			widget.NewLabel(i18n.Current.Width),
			vqd.widthEntry,
			widget.NewLabel(i18n.Current.UnitPx),
		),
		container.NewHBox(
			widget.NewLabel(i18n.Current.Height),
			vqd.heightEntry,
			widget.NewLabel(i18n.Current.UnitPx),
		),
		container.NewHBox(
			widget.NewLabel(i18n.Current.FPS),
			vqd.fpsEntry,
			widget.NewLabel(i18n.Current.FramesPerSecond),
		),
		container.NewHBox(
			widget.NewLabel(i18n.Current.Quality),
			vqd.qualityEntry,
			widget.NewLabel(i18n.Current.UnitPercent),
		),
		container.NewHBox(
			widget.NewLabel(i18n.Current.Bitrate),
			vqd.bitrateEntry,
			widget.NewLabel(i18n.Current.UnitKbps),
		),
		widget.NewSeparator(),
		container.NewHBox(
			widget.NewButton(i18n.Current.Apply, vqd.handleApply),
			widget.NewButton(i18n.Current.Cancel, func() {}),
		),
	)

	dialog.ShowCustom(i18n.Current.VideoQualitySettings, i18n.Current.Close, settingsContainer, vqd.parent)
}

// handleApply обрабатывает применение настроек
func (vqd *VideoQualityDialog) handleApply() {
	width, err := strconv.Atoi(vqd.widthEntry.Text)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.InvalidWidth, err), vqd.parent)
		return
	}

	height, err := strconv.Atoi(vqd.heightEntry.Text)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.InvalidHeight, err), vqd.parent)
		return
	}

	fps, err := strconv.Atoi(vqd.fpsEntry.Text)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.InvalidFPS, err), vqd.parent)
		return
	}

	quality, err := strconv.Atoi(vqd.qualityEntry.Text)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.InvalidQuality, err), vqd.parent)
		return
	}

	bitrate, err := strconv.Atoi(vqd.bitrateEntry.Text)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.InvalidBitrate, err), vqd.parent)
		return
	}

	if width < 320 || width > 1920 {
		dialog.ShowError(fmt.Errorf("%s", i18n.Current.WidthRange), vqd.parent)
		return
	}
	if height < 240 || height > 1080 {
		dialog.ShowError(fmt.Errorf("%s", i18n.Current.HeightRange), vqd.parent)
		return
	}
	if fps < 1 || fps > 60 {
		dialog.ShowError(fmt.Errorf("%s", i18n.Current.FPSRange), vqd.parent)
		return
	}
	if quality < 1 || quality > 100 {
		dialog.ShowError(fmt.Errorf("%s", i18n.Current.QualityRange), vqd.parent)
		return
	}
	if bitrate < 100 || bitrate > 10000 {
		dialog.ShowError(fmt.Errorf("%s", i18n.Current.BitrateRange), vqd.parent)
		return
	}

	if vqd.onApply != nil {
		vqd.onApply(width, height, fps, quality, bitrate)
	}

	logrus.Infof("✅ Настройки качества видео применены: %dx%d, %d FPS, качество %d%%, битрейт %d kbps",
		width, height, fps, quality, bitrate)

	dialog.ShowInformation(i18n.Current.Success, i18n.Current.VideoSettingsApplied, vqd.parent)
}
