package ui

import (
	"fmt"
	"strconv"

	"usbridge-client/internal/models"
	"usbridge-client/internal/ui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// ResolutionPreset предустановленное разрешение
type ResolutionPreset struct {
	Name   string
	Width  int
	Height int
}

// VideoStartDialog диалог настройки параметров запуска видео
type VideoStartDialog struct {
	dialog *widget.PopUp
	parent fyne.Window

	// Предустановленные разрешения
	resolutions      []ResolutionPreset
	resolutionSelect *widget.Select

	// Ползунки для параметров
	fpsSlider     *widget.Slider
	qualitySlider *widget.Slider
	bitrateSlider *widget.Slider

	// Лейблы для отображения значений
	fpsLabel     *widget.Label
	qualityLabel *widget.Label
	bitrateLabel *widget.Label

	// Кнопки
	startBtn  *widget.Button
	cancelBtn *widget.Button

	// Callback для применения настроек
	onApply func(request *models.VideoStartRequest)
}

// NewVideoStartDialog создает новый диалог запуска видео
func NewVideoStartDialog(parent fyne.Window) *VideoStartDialog {
	vsd := &VideoStartDialog{
		parent: parent,
		resolutions: []ResolutionPreset{
			{"800×600 (SVGA)", 800, 600},   // По умолчанию
			{"640×480 (VGA)", 640, 480},
			{"1024×768 (XGA)", 1024, 768},
			{"1280×720 (HD)", 1280, 720},
			{"1600×900 (HD+)", 1600, 900},
			{"1920×1080 (FHD)", 1920, 1080},
			{"320×240 (QVGA)", 320, 240},
		},
	}

	vsd.createInterface()
	return vsd
}

// createInterface создает интерфейс диалога
func (vsd *VideoStartDialog) createInterface() {
	// Создаем выпадающий список разрешений
	resolutionOptions := make([]string, len(vsd.resolutions))
	for i, res := range vsd.resolutions {
		resolutionOptions[i] = res.Name
	}
	vsd.resolutionSelect = widget.NewSelect(resolutionOptions, nil)
	// Значение по умолчанию будет установлено через SetDefaults() перед показом

	// FPS
	vsd.fpsSlider = widget.NewSlider(15, 60)
	vsd.fpsSlider.Value = 30
	vsd.fpsSlider.Step = 5
	vsd.fpsLabel = widget.NewLabel("30 fps")

	vsd.fpsSlider.OnChanged = func(value float64) {
		vsd.fpsLabel.SetText(fmt.Sprintf("%.0f fps", value))
	}

	// Качество
	vsd.qualitySlider = widget.NewSlider(50, 100)
	vsd.qualitySlider.Value = 80
	vsd.qualitySlider.Step = 10
	vsd.qualityLabel = widget.NewLabel("80%")

	vsd.qualitySlider.OnChanged = func(value float64) {
		vsd.qualityLabel.SetText(fmt.Sprintf("%.0f%%", value))
	}

	// Битрейт
	vsd.bitrateSlider = widget.NewSlider(1000, 8000)
	vsd.bitrateSlider.Value = 2000
	vsd.bitrateSlider.Step = 500
	vsd.bitrateLabel = widget.NewLabel("2.0 Mbps")

	vsd.bitrateSlider.OnChanged = func(value float64) {
		vsd.bitrateLabel.SetText(fmt.Sprintf("%.1f Mbps", value/1000))
	}

	// Кнопки
	vsd.startBtn = widget.NewButton("▶️ "+i18n.Current.StartVideo, vsd.handleStart)
	vsd.startBtn.Importance = widget.HighImportance

	vsd.cancelBtn = widget.NewButton(i18n.Current.Cancel, vsd.handleCancel)

	// Создаем компактную форму
	form := container.NewVBox(
		// Заголовок
		widget.NewLabelWithStyle("🎥 "+i18n.Current.VideoParameters, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),

		// Разрешение
		container.NewVBox(
			widget.NewLabelWithStyle("📐 "+i18n.Current.Resolution, fyne.TextAlignLeading, fyne.TextStyle{Bold: false}),
			vsd.resolutionSelect,
		),

		// FPS
		container.NewVBox(
			container.NewBorder(nil, nil,
				widget.NewLabel("🎬 "+i18n.Current.FrameRate),
				vsd.fpsLabel,
				nil,
			),
			vsd.fpsSlider,
		),

		// Качество
		container.NewVBox(
			container.NewBorder(nil, nil,
				widget.NewLabel("✨ "+i18n.Current.Quality),
				vsd.qualityLabel,
				nil,
			),
			vsd.qualitySlider,
		),

		// Битрейт
		container.NewVBox(
			container.NewBorder(nil, nil,
				widget.NewLabel("💾 "+i18n.Current.Bitrate),
				vsd.bitrateLabel,
				nil,
			),
			vsd.bitrateSlider,
		),

		widget.NewSeparator(),

		// Кнопки
		container.NewGridWithColumns(2,
			vsd.startBtn,
			vsd.cancelBtn,
		),
	)

	// Создаем всплывающее окно (переработанный диалог запуска видео)
	vsd.dialog = widget.NewModalPopUp(form, vsd.parent.Canvas())
	vsd.dialog.Resize(fyne.NewSize(420, 480))
}

// Show показывает диалог
func (vsd *VideoStartDialog) Show(onApply func(request *models.VideoStartRequest)) {
	vsd.onApply = onApply

	// Сбрасываем состояние кнопок перед показом
	vsd.startBtn.Enable()
	vsd.cancelBtn.Enable()
	vsd.startBtn.SetText("▶️ " + i18n.Current.StartVideo)

	vsd.dialog.Show()
}

// Hide скрывает диалог
func (vsd *VideoStartDialog) Hide() {
	vsd.dialog.Hide()

	// Сбрасываем состояние кнопок после скрытия
	vsd.startBtn.Enable()
	vsd.cancelBtn.Enable()
	vsd.startBtn.SetText("▶️ " + i18n.Current.StartVideo)
}

// handleStart обрабатывает нажатие кнопки запуска
func (vsd *VideoStartDialog) handleStart() {
	// Немедленно отключаем кнопки для визуального отклика
	vsd.startBtn.Disable()
	vsd.cancelBtn.Disable()
	vsd.startBtn.SetText("⏳ " + i18n.Current.Starting)

	// Находим выбранное разрешение
	selectedResolution := vsd.resolutionSelect.Selected
	var resolution ResolutionPreset

	for _, res := range vsd.resolutions {
		if res.Name == selectedResolution {
			resolution = res
			break
		}
	}

	// Создаем запрос с текущими значениями
	request := &models.VideoStartRequest{
		VideoWidth:   resolution.Width,
		VideoHeight:  resolution.Height,
		VideoFPS:     int(vsd.fpsSlider.Value),
		VideoQuality: int(vsd.qualitySlider.Value),
		VideoBitrate: fmt.Sprintf("%.0fK", vsd.bitrateSlider.Value),
	}

	logrus.Infof("🎥 Запуск видео: %dx%d @ %d fps, качество %d%%, битрейт %s",
		request.VideoWidth, request.VideoHeight, request.VideoFPS, request.VideoQuality, request.VideoBitrate)

	// Скрываем диалог сразу
	vsd.Hide()

	// Вызываем callback в отдельной горутине, чтобы не блокировать UI-поток.
	// Без этого handleVideoStartWithParamsGStreamer (с retries и sleep) блокирует
	// рендер-цикл Fyne, и диалог визуально не скрывается.
	if vsd.onApply != nil {
		go vsd.onApply(request)
	}
}

// handleCancel обрабатывает нажатие кнопки отмены
func (vsd *VideoStartDialog) handleCancel() {
	logrus.Info("❌ Отмена запуска видео")
	vsd.Hide()
}

// SetDefaults устанавливает значения по умолчанию
func (vsd *VideoStartDialog) SetDefaults(width, height, fps, quality int, bitrate string) {
	// Находим подходящее разрешение
	found := false
	for _, resolution := range vsd.resolutions {
		if resolution.Width == width && resolution.Height == height {
			vsd.resolutionSelect.SetSelected(resolution.Name)
			found = true
			break
		}
	}

	// Если не нашли подходящее разрешение, устанавливаем SVGA по умолчанию
	if !found {
		vsd.resolutionSelect.SetSelected("800×600 (SVGA)")
	}

	vsd.fpsSlider.SetValue(float64(fps))
	vsd.qualitySlider.SetValue(float64(quality))

	// Парсим битрейт (например, "2M" -> 2000)
	if bitrate != "" {
		if bitrate[len(bitrate)-1] == 'M' {
			if val, err := strconv.ParseFloat(bitrate[:len(bitrate)-1], 64); err == nil {
				vsd.bitrateSlider.SetValue(val * 1000)
			}
		} else if bitrate[len(bitrate)-1] == 'K' {
			if val, err := strconv.ParseFloat(bitrate[:len(bitrate)-1], 64); err == nil {
				vsd.bitrateSlider.SetValue(val)
			}
		}
	}
}
