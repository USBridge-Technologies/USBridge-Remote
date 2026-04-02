package view

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

type ResolutionPreset struct {
	Label string
	Mode  models.VideoCaptureMode
}

type VideoStartDialog struct {
	dialog *widget.PopUp
	parent fyne.Window

	streamModes      []models.VideoTransportMode
	captureModes     []models.VideoCaptureMode
	modeLabels       map[string]string
	resolutionLabels map[string]models.VideoCaptureMode

	modeSelect        *widget.RadioGroup
	modeDescription   *widget.Label
	resolutionSelect  *widget.Select
	fpsSelect         *widget.Select
	bitrateSlider     *widget.Slider
	fpsValueLabel     *widget.Label
	bitrateValueLabel *widget.Label
	bitrateBlock      *fyne.Container
	jpegHint          *widget.Label
	deviceLabel       *widget.Label

	startBtn  *widget.Button
	cancelBtn *widget.Button
	extraBtn  *widget.Button

	onApply func(request *models.VideoStartRequest)
}

func NewVideoStartDialog(parent fyne.Window) *VideoStartDialog {
	vsd := &VideoStartDialog{
		parent:           parent,
		modeLabels:       make(map[string]string),
		resolutionLabels: make(map[string]models.VideoCaptureMode),
	}
	vsd.createInterface()
	return vsd
}

func (vsd *VideoStartDialog) createInterface() {
	vsd.modeDescription = widget.NewLabel("")
	vsd.modeDescription.Wrapping = fyne.TextWrapWord

	vsd.modeSelect = widget.NewRadioGroup(nil, func(string) {
		vsd.refreshModeUI()
	})

	vsd.resolutionSelect = widget.NewSelect(nil, func(string) {
		vsd.refreshAvailableModes()
		vsd.refreshFPSOptions()
	})

	vsd.fpsSelect = widget.NewSelect(nil, nil)
	vsd.fpsValueLabel = widget.NewLabel("")

	vsd.bitrateSlider = widget.NewSlider(1000, 12000)
	vsd.bitrateSlider.Step = 500
	vsd.bitrateSlider.Value = 2000
	vsd.bitrateValueLabel = widget.NewLabel("")
	vsd.bitrateSlider.OnChanged = func(value float64) {
		vsd.bitrateValueLabel.SetText(fmt.Sprintf("%.1f %s", value/1000, i18n.Current.UnitMbps))
	}

	vsd.jpegHint = widget.NewLabel(i18n.Current.VideoJPEGRTPHint)
	vsd.jpegHint.Wrapping = fyne.TextWrapWord
	vsd.deviceLabel = widget.NewLabel("")
	vsd.deviceLabel.Wrapping = fyne.TextWrapWord

	vsd.startBtn = widget.NewButton("▶️ "+i18n.Current.StartVideo, vsd.handleStart)
	vsd.startBtn.Importance = widget.HighImportance
	vsd.cancelBtn = widget.NewButton(i18n.Current.Cancel, vsd.handleCancel)
	vsd.extraBtn = widget.NewButton("", nil)
	vsd.extraBtn.Hide()

	vsd.bitrateBlock = container.NewVBox(
		container.NewBorder(nil, nil,
			widget.NewLabel("💾 "+i18n.Current.Bitrate),
			vsd.bitrateValueLabel,
			nil,
		),
		vsd.bitrateSlider,
	)

	form := container.NewVBox(
		widget.NewLabelWithStyle("🎥 "+i18n.Current.VideoParameters, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		vsd.deviceLabel,
		widget.NewSeparator(),
		widget.NewLabelWithStyle(i18n.Current.StreamMode, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		vsd.modeSelect,
		vsd.modeDescription,
		widget.NewSeparator(),
		container.NewVBox(
			widget.NewLabelWithStyle("📐 "+i18n.Current.Resolution, fyne.TextAlignLeading, fyne.TextStyle{}),
			vsd.resolutionSelect,
		),
		container.NewVBox(
			container.NewBorder(nil, nil, widget.NewLabel("🎬 "+i18n.Current.FrameRate), vsd.fpsValueLabel, nil),
			vsd.fpsSelect,
		),
		vsd.bitrateBlock,
		vsd.jpegHint,
		vsd.extraBtn,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, vsd.startBtn, vsd.cancelBtn),
	)

	vsd.dialog = widget.NewModalPopUp(form, vsd.parent.Canvas())
	vsd.dialog.Resize(fyne.NewSize(460, 430))
	vsd.bitrateSlider.OnChanged(vsd.bitrateSlider.Value)
}

func (vsd *VideoStartDialog) Configure(info *models.VideoInfoData, defaultWidth, defaultHeight, defaultFPS int, defaultBitrate string) {
	vsd.streamModes = nil
	vsd.captureModes = nil
	vsd.modeLabels = make(map[string]string)
	vsd.resolutionLabels = make(map[string]models.VideoCaptureMode)

	if info != nil && len(info.SupportedModes) > 0 {
		vsd.streamModes = append(vsd.streamModes, info.SupportedModes...)
	}
	if len(vsd.streamModes) == 0 {
		vsd.streamModes = []models.VideoTransportMode{
			{
				ID:                models.VideoModeH264,
				Name:              i18n.Current.VideoModeH264Name,
				Description:       i18n.Current.VideoModeH264Description,
				Transport:         "rtp",
				Encoding:          "h264",
				ServerDecodesJPEG: true,
			},
			{
				ID:                models.VideoModeJPEGRTP,
				Name:              i18n.Current.VideoModeJPEGName,
				Description:       i18n.Current.VideoModeJPEGDescription,
				Transport:         "rtp",
				Encoding:          "jpeg",
				ServerDecodesJPEG: false,
			},
			{
				ID:                models.VideoModeRawYUYV,
				Name:              i18n.Current.VideoModeRawYUYVName,
				Description:       i18n.Current.VideoModeRawYUYVDescription,
				Transport:         "rtp",
				Encoding:          "raw",
				ServerDecodesJPEG: false,
			},
		}
	}

	if info != nil && len(info.CaptureModes) > 0 {
		vsd.captureModes = append(vsd.captureModes, info.CaptureModes...)
	}
	if len(vsd.captureModes) == 0 {
		vsd.captureModes = []models.VideoCaptureMode{
			{Width: defaultWidth, Height: defaultHeight, FPS: []int{defaultFPS}, PixelFormat: "MJPG"},
		}
	}

	sort.Slice(vsd.captureModes, func(i, j int) bool {
		li := vsd.captureModes[i].Width * vsd.captureModes[i].Height
		lj := vsd.captureModes[j].Width * vsd.captureModes[j].Height
		if li != lj {
			return li < lj
		}
		return vsd.captureModes[i].Width < vsd.captureModes[j].Width
	})

	modeOptions := make([]string, 0, len(vsd.streamModes))
	for _, mode := range vsd.streamModes {
		label := mode.Name
		if label == "" {
			label = mode.ID
		}
		vsd.modeLabels[label] = mode.ID
		modeOptions = append(modeOptions, label)
	}
	vsd.modeSelect.Options = modeOptions
	vsd.modeSelect.Refresh()

	resolutionOptions := make([]string, 0, len(vsd.captureModes))
	defaultResolutionLabel := ""
	hasMultipleFormats := false
	formatsSeen := map[string]bool{}
	for _, captureMode := range vsd.captureModes {
		formatsSeen[captureMode.PixelFormat] = true
	}
	hasMultipleFormats = len(formatsSeen) > 1

	for _, captureMode := range vsd.captureModes {
		label := fmt.Sprintf("%dx%d", captureMode.Width, captureMode.Height)
		if captureMode.PixelFormat != "" && (hasMultipleFormats || true) {
			label = fmt.Sprintf("%s [%s]", label, captureMode.PixelFormat)
		}
		if len(captureMode.FPS) > 0 {
			label = fmt.Sprintf("%s (%s)", label, formatFPSRange(captureMode.FPS))
		}
		vsd.resolutionLabels[label] = captureMode
		resolutionOptions = append(resolutionOptions, label)
		if captureMode.Width == defaultWidth && captureMode.Height == defaultHeight {
			defaultResolutionLabel = label
		}
	}
	vsd.resolutionSelect.Options = resolutionOptions
	vsd.resolutionSelect.Refresh()

	if defaultResolutionLabel == "" && len(resolutionOptions) > 0 {
		defaultResolutionLabel = resolutionOptions[0]
	}
	if defaultResolutionLabel != "" {
		vsd.resolutionSelect.SetSelected(defaultResolutionLabel)
	}

	selectedMode := models.VideoModeH264
	if info != nil && info.Mode != "" {
		selectedMode = info.Mode
	}
	if selectedMode == "" {
		selectedMode = models.VideoModeH264
	}
	vsd.refreshAvailableModes()
	vsd.setSelectedMode(selectedMode)

	if bitrate, ok := parseBitrate(defaultBitrate); ok {
		vsd.bitrateSlider.SetValue(float64(bitrate))
	} else {
		vsd.bitrateSlider.SetValue(2000)
	}

	vsd.refreshFPSOptions()
	vsd.setDefaultFPS(defaultFPS)
	vsd.refreshModeUI()
}

func (vsd *VideoStartDialog) Show(onApply func(request *models.VideoStartRequest)) {
	vsd.onApply = onApply
	vsd.startBtn.Enable()
	vsd.cancelBtn.Enable()
	vsd.dialog.Show()
}

func (vsd *VideoStartDialog) SetDeviceLabel(text string) {
	vsd.deviceLabel.SetText(text)
	if text == "" {
		vsd.deviceLabel.Hide()
		return
	}
	vsd.deviceLabel.Show()
}

func (vsd *VideoStartDialog) SetPrimaryAction(label string) {
	if label == "" {
		label = i18n.Current.StartVideo
	}
	vsd.startBtn.SetText("▶️ " + label)
}

func (vsd *VideoStartDialog) SetExtraAction(label string, onTap func()) {
	if label == "" || onTap == nil {
		vsd.extraBtn.Hide()
		vsd.extraBtn.OnTapped = nil
		return
	}
	vsd.extraBtn.SetText(label)
	vsd.extraBtn.OnTapped = onTap
	vsd.extraBtn.Show()
}

func (vsd *VideoStartDialog) Hide() {
	vsd.dialog.Hide()
	vsd.startBtn.Enable()
	vsd.cancelBtn.Enable()
	vsd.startBtn.SetText("▶️ " + i18n.Current.StartVideo)
}

func (vsd *VideoStartDialog) refreshFPSOptions() {
	mode, ok := vsd.resolutionLabels[vsd.resolutionSelect.Selected]
	if !ok {
		return
	}

	options := make([]string, 0, len(mode.FPS))
	for _, fps := range mode.FPS {
		options = append(options, strconv.Itoa(fps))
	}
	if len(options) == 0 {
		options = []string{"30"}
	}
	vsd.fpsSelect.Options = options
	vsd.fpsSelect.Refresh()
	if vsd.fpsSelect.Selected == "" {
		vsd.fpsSelect.SetSelected(options[0])
	}
	vsd.fpsValueLabel.SetText(vsd.fpsSelect.Selected + " " + i18n.Current.FramesPerSecond)
	vsd.fpsSelect.OnChanged = func(value string) {
		vsd.fpsValueLabel.SetText(value + " " + i18n.Current.FramesPerSecond)
	}
}

func (vsd *VideoStartDialog) setDefaultFPS(defaultFPS int) {
	if defaultFPS <= 0 {
		return
	}
	for _, option := range vsd.fpsSelect.Options {
		if option == strconv.Itoa(defaultFPS) {
			vsd.fpsSelect.SetSelected(option)
			return
		}
	}
}

func (vsd *VideoStartDialog) refreshModeUI() {
	modeID := vsd.selectedModeID()
	description := ""
	for _, mode := range vsd.streamModes {
		if mode.ID == modeID {
			description = mode.Description
			break
		}
	}
	vsd.modeDescription.SetText(description)

	switch modeID {
	case models.VideoModeJPEGRTP:
		vsd.bitrateBlock.Hide()
		vsd.jpegHint.SetText(i18n.Current.VideoJPEGRTPHint)
		vsd.jpegHint.Show()
	case models.VideoModeRawYUYV:
		vsd.bitrateBlock.Hide()
		vsd.jpegHint.SetText(i18n.Current.VideoRawYUYVHint)
		vsd.jpegHint.Show()
	default:
		vsd.bitrateBlock.Show()
		vsd.jpegHint.Hide()
	}
}

func (vsd *VideoStartDialog) selectedModeID() string {
	if id, ok := vsd.modeLabels[vsd.modeSelect.Selected]; ok {
		return id
	}
	return models.VideoModeH264
}

func (vsd *VideoStartDialog) handleStart() {
	vsd.startBtn.Disable()
	vsd.cancelBtn.Disable()
	vsd.startBtn.SetText("⏳ " + i18n.Current.Starting)

	selectedMode, ok := vsd.resolutionLabels[vsd.resolutionSelect.Selected]
	if !ok {
		selectedMode = models.VideoCaptureMode{Width: 800, Height: 600, FPS: []int{30}}
	}

	fps, err := strconv.Atoi(vsd.fpsSelect.Selected)
	if err != nil || fps <= 0 {
		fps = 30
	}

	request := &models.VideoStartRequest{
		VideoWidth:         selectedMode.Width,
		VideoHeight:        selectedMode.Height,
		VideoFPS:           fps,
		VideoQuality:       80,
		VideoBitrate:       fmt.Sprintf("%.0fK", vsd.bitrateSlider.Value),
		VideoMode:          vsd.selectedModeID(),
		CapturePixelFormat: selectedMode.PixelFormat,
	}

	logrus.Infof("🎥 Starting video: mode=%s %dx%d @ %d fps, bitrate %s",
		request.VideoMode, request.VideoWidth, request.VideoHeight, request.VideoFPS, request.VideoBitrate)

	vsd.Hide()
	if vsd.onApply != nil {
		go vsd.onApply(request)
	}
}

func (vsd *VideoStartDialog) handleCancel() {
	logrus.Info("❌ Video start cancelled")
	vsd.Hide()
}

func formatFPSRange(values []int) string {
	if len(values) == 0 {
		return "fps?"
	}
	if len(values) == 1 {
		return fmt.Sprintf("%d fps", values[0])
	}
	return fmt.Sprintf("%d-%d fps", values[0], values[len(values)-1])
}

func (vsd *VideoStartDialog) refreshAvailableModes() {
	selectedCaptureMode, ok := vsd.resolutionLabels[vsd.resolutionSelect.Selected]
	selectedFormat := ""
	if ok {
		selectedFormat = normalizePixelFormat(selectedCaptureMode.PixelFormat)
	}

	allowed := allowedModesForPixelFormat(selectedFormat)
	modeOptions := make([]string, 0, len(vsd.streamModes))
	for _, mode := range vsd.streamModes {
		if len(allowed) > 0 && !allowed[mode.ID] {
			continue
		}
		label := mode.Name
		if label == "" {
			label = mode.ID
		}
		modeOptions = append(modeOptions, label)
	}

	previous := vsd.selectedModeID()
	vsd.modeSelect.Options = modeOptions
	vsd.modeSelect.Refresh()
	vsd.setSelectedMode(previous)
}

func (vsd *VideoStartDialog) setSelectedMode(modeID string) {
	for label, id := range vsd.modeLabels {
		if id == modeID {
			for _, option := range vsd.modeSelect.Options {
				if option == label {
					vsd.modeSelect.SetSelected(label)
					return
				}
			}
		}
	}
	if len(vsd.modeSelect.Options) > 0 {
		vsd.modeSelect.SetSelected(vsd.modeSelect.Options[0])
	}
}

func allowedModesForPixelFormat(format string) map[string]bool {
	switch normalizePixelFormat(format) {
	case "MJPG", "MJPEG", "JPEG":
		return map[string]bool{
			models.VideoModeH264:    true,
			models.VideoModeJPEGRTP: true,
		}
	case "YUYV", "YUYV422", "YUY2":
		return map[string]bool{
			models.VideoModeH264:    true,
			models.VideoModeJPEGRTP: true,
			models.VideoModeRawYUYV: true,
		}
	default:
		return map[string]bool{
			models.VideoModeH264: true,
		}
	}
}

func normalizePixelFormat(format string) string {
	return strings.TrimSpace(strings.ToUpper(format))
}

func parseBitrate(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	last := value[len(value)-1]
	switch last {
	case 'M':
		v, err := strconv.ParseFloat(value[:len(value)-1], 64)
		if err != nil {
			return 0, false
		}
		return int(v * 1000), true
	case 'K':
		v, err := strconv.ParseFloat(value[:len(value)-1], 64)
		if err != nil {
			return 0, false
		}
		return int(v), true
	default:
		v, err := strconv.Atoi(value)
		if err != nil {
			return 0, false
		}
		return v, true
	}
}
