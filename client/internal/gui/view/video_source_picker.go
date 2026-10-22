package view

import (
	"fmt"
	"strconv"
	"strings"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// videoSourcePresets are the monitor shapes offered by name in the video
// source picker, one per shape: only the shape matters to the agent (where
// the capture card puts the picture), so any resolution of it stands for
// all. `examples` names a few; anything else is typed in.
var videoSourcePresets = []struct {
	w, h     int
	examples string
}{
	{1920, 1080, "1920 × 1080, 2560 × 1440, 3840 × 2160"},
	{1920, 1200, "1440 × 900, 1680 × 1050, 2560 × 1600"},
	{1650, 1050, "Mac scaled 1650 × 1050"},
	{3024, 1964, "MacBook Pro 14″ panel: 3024 × 1964, 1512 × 982"},
	{3456, 2234, "MacBook Pro 16″ panel: 3456 × 2234, 1728 × 1117"},
	{2560, 1664, "MacBook Air 13″ panel: 2560 × 1664, 1470 × 956"},
	{2880, 1920, "Surface, 2160 × 1440"},
	{1024, 768, "1024 × 768, 1600 × 1200"},
	{1280, 1024, "1280 × 1024"},
	{3440, 1440, "3440 × 1440, 2560 × 1080"},
	{5120, 1440, "5120 × 1440"},
	{1080, 1920, "1080 × 1920, 1200 × 1920"},
}

// videoSourcePicker is the video parameters dialog's "Video source" field:
// which part of a capture card's frame is the target's screen
// (models.VideoSourceSetting). Shown only when the agent takes the setting.
type videoSourcePicker struct {
	selectBox   *HeaderDropdown
	widthEntry  *StyledEntry
	heightEntry *StyledEntry
	rectEntry   *StyledEntry
	customRow   *fyne.Container
	// field: caption + picker, a column of the dialog's capture row.
	field *fyne.Container
	// block: what the picker adds under that row (the typed-in values).
	block        *fyne.Container
	presetLabels map[string][2]int
	// onShown: the agent does (true) or doesn't take the setting.
	onShown func(bool)
}

func newVideoSourcePicker(hintWidth float32) *videoSourcePicker {
	p := &videoSourcePicker{presetLabels: make(map[string][2]int)}
	p.widthEntry = NewStyledEntry()
	p.widthEntry.SetPlaceHolder(i18n.Current.VideoSourceWidth)
	p.heightEntry = NewStyledEntry()
	p.heightEntry.SetPlaceHolder(i18n.Current.VideoSourceHeight)
	p.rectEntry = NewStyledEntry()
	p.rectEntry.SetPlaceHolder(i18n.Current.VideoSourceRect)
	// The hint explains the typed-in values only; the other choices say
	// enough by name.
	p.customRow = container.NewVBox(
		container.NewGridWithColumns(2, p.widthEntry, p.heightEntry),
		p.rectEntry,
		newVideoDialogDescription(i18n.Current.VideoSourceHint, hintWidth),
	)
	p.customRow.Hide()

	options := []string{i18n.Current.VideoSourceAuto, i18n.Current.VideoSourceWholeFrame}
	details := map[string]string{
		i18n.Current.VideoSourceAuto:       i18n.Current.VideoSourceAutoDetail,
		i18n.Current.VideoSourceWholeFrame: i18n.Current.VideoSourceWholeFrameDetail,
	}
	for _, r := range videoSourcePresets {
		label := aspectLabel(r.w, r.h)
		if _, dup := p.presetLabels[label]; dup {
			label = fmt.Sprintf("%s (%d × %d)", label, r.w, r.h)
		}
		options = append(options, label)
		details[label] = r.examples
		p.presetLabels[label] = [2]int{r.w, r.h}
	}
	options = append(options, i18n.Current.VideoSourceCustom)
	p.selectBox = newVideoDialogPicker(func(string) { p.refreshCustomRow() })
	p.selectBox.SetOptions(options)
	p.selectBox.SetDetails(details)
	p.selectBox.SetSelected(i18n.Current.VideoSourceAuto)

	p.field = container.NewVBox(newVideoDialogFieldLabel(i18n.Current.VideoSource), p.selectBox)
	p.field.Hide()
	p.block = container.NewVBox(p.customRow)
	p.block.Hide()
	return p
}

func (p *videoSourcePicker) show(shown bool) {
	if shown {
		p.field.Show()
		p.block.Show()
	} else {
		p.field.Hide()
		p.block.Hide()
	}
	if p.onShown != nil {
		p.onShown(shown)
	}
}

func (p *videoSourcePicker) refreshCustomRow() {
	if p.selectBox.Selected == i18n.Current.VideoSourceCustom {
		p.customRow.Show()
	} else {
		p.customRow.Hide()
	}
	p.block.Refresh()
}

// configure shows the picker when the agent takes the setting, on the
// agent's current value.
func (p *videoSourcePicker) configure(info *models.VideoSourceInfo) {
	if info == nil {
		p.show(false)
		return
	}
	if info.AutoSupported {
		p.selectBox.MutedOptions = nil
	} else {
		p.selectBox.MutedOptions = []string{i18n.Current.VideoSourceAuto}
	}
	p.widthEntry.SetText("")
	p.heightEntry.SetText("")
	p.rectEntry.SetText("")
	selected := i18n.Current.VideoSourceWholeFrame
	switch info.Mode {
	case models.VideoSourceAuto:
		if info.AutoSupported {
			selected = i18n.Current.VideoSourceAuto
		}
	case models.VideoSourceManual:
		selected = i18n.Current.VideoSourceCustom
		if len(info.Rect) == 0 {
			for label, r := range p.presetLabels {
				// Same shape: one preset stands for every resolution of it.
				if r[0]*info.Height == r[1]*info.Width {
					selected = label
				}
			}
		}
		if info.Width > 0 && info.Height > 0 {
			p.widthEntry.SetText(strconv.Itoa(info.Width))
			p.heightEntry.SetText(strconv.Itoa(info.Height))
		}
		if len(info.Rect) == 4 {
			parts := make([]string, 4)
			for i, f := range info.Rect {
				parts[i] = strconv.FormatFloat(f*100, 'f', -1, 64)
			}
			p.rectEntry.SetText(strings.Join(parts, ", "))
		}
	}
	p.selectBox.SetSelected(selected)
	p.refreshCustomRow()
	p.show(true)
}

// setting is the picker's choice, or an error for a typed-in value that
// doesn't parse; nil while the picker is hidden (the agent doesn't take it).
func (p *videoSourcePicker) setting() (*models.VideoSourceSetting, error) {
	if !p.block.Visible() {
		return nil, nil
	}
	switch sel := p.selectBox.Selected; sel {
	case i18n.Current.VideoSourceWholeFrame:
		return &models.VideoSourceSetting{Mode: models.VideoSourceCapture}, nil
	case i18n.Current.VideoSourceAuto:
		return &models.VideoSourceSetting{Mode: models.VideoSourceAuto}, nil
	case i18n.Current.VideoSourceCustom:
		s := models.VideoSourceSetting{Mode: models.VideoSourceManual}
		w, werr := strconv.Atoi(strings.TrimSpace(p.widthEntry.Text))
		h, herr := strconv.Atoi(strings.TrimSpace(p.heightEntry.Text))
		if werr == nil && herr == nil && w > 0 && h > 0 {
			s.Width, s.Height = w, h
		}
		if text := strings.TrimSpace(p.rectEntry.Text); text != "" {
			rect, err := parsePercentRect(text)
			if err != nil {
				return nil, err
			}
			s.Rect = rect
		}
		if s.Width == 0 && len(s.Rect) == 0 {
			return nil, fmt.Errorf("source resolution %q x %q", p.widthEntry.Text, p.heightEntry.Text)
		}
		return &s, nil
	default:
		r, ok := p.presetLabels[sel]
		if !ok {
			return nil, nil
		}
		return &models.VideoSourceSetting{Mode: models.VideoSourceManual, Width: r[0], Height: r[1]}, nil
	}
}

// parsePercentRect reads "x, y, w, h" in percent of the frame as fractions.
func parsePercentRect(text string) ([]float64, error) {
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '%' })
	if len(fields) != 4 {
		return nil, fmt.Errorf("exact area %q: need x, y, w, h", text)
	}
	rect := make([]float64, 4)
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil || v < 0 || v > 100 {
			return nil, fmt.Errorf("exact area %q: %q is not a percentage", text, f)
		}
		rect[i] = v / 100
	}
	if rect[2] <= 0 || rect[3] <= 0 || rect[0]+rect[2] > 1.0001 || rect[1]+rect[3] > 1.0001 {
		return nil, fmt.Errorf("exact area %q is outside the frame", text)
	}
	return rect, nil
}

// aspectLabel is w:h reduced ("16:10"), or a decimal ratio ("2.39:1") when
// the reduced form isn't a familiar one.
func aspectLabel(w, h int) string {
	a, b := w, h
	for b != 0 {
		a, b = b, a%b
	}
	rw, rh := w/a, h/a
	if rw == 8 && rh == 5 {
		rw, rh = 16, 10
	}
	if rw <= 32 && rh <= 32 {
		return fmt.Sprintf("%d:%d", rw, rh)
	}
	return fmt.Sprintf("%.2f:1", float64(w)/float64(h))
}
