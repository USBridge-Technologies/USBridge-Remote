package view

// scripts_tab.go -- Scripts tab: Devices-style two-column split (narrow MCP
// card, wide automation table) with Connections/Snapshots section headers
// and the shared app footer (busy spinner + version).

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

const (
	scriptsColumnGap          float32 = 16
	scriptsColumnRatio        float32 = 1.8
	scriptsContentInset       float32 = 18
	scriptsHeaderDividerNudge float32 = 5
	scriptsVisibleRows                = 8
	scriptsTableScrollGutter  float32 = 10
)

var scriptsMCPURLColor = color.NRGBA{R: 0xe6, G: 0xf9, B: 0xb9, A: 0xff}

const (
	scriptsMCPSubtitle        = "Local signed MCP endpoint."
	scriptsAutomationSubtitle = "Starlark jobs on the device."
)

var (
	scriptListColumnLabels = []string{"NAME", "SOURCE", "STATE", "ACTIONS"}
	scriptListColumnWidths = []float32{0, 72, 90, 136}
)

var (
	scriptsPlayIconSVG      = fyne.NewStaticResource("scripts-play.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#4c6803"><path d="M16.6582 9.28638C18.098 10.1862 18.8178 10.6361 19.0647 11.2122C19.2803 11.7152 19.2803 12.2847 19.0647 12.7878C18.8178 13.3638 18.098 13.8137 16.6582 14.7136L9.896 18.94C8.29805 19.9387 7.49907 20.4381 6.83973 20.385C6.26501 20.3388 5.73818 20.0469 5.3944 19.584C5 19.053 5 18.1108 5 16.2264V7.77357C5 5.88919 5 4.94701 5.3944 4.41598C5.73818 3.9531 6.26501 3.66111 6.83973 3.6149C7.49907 3.5619 8.29805 4.06126 9.896 5.05998L16.6582 9.28638Z"/></svg>`))
	scriptsMCPPlayIconSVG   = fyne.NewStaticResource("scripts-mcp-play.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#111111"><path d="M16.6582 9.28638C18.098 10.1862 18.8178 10.6361 19.0647 11.2122C19.2803 11.7152 19.2803 12.2847 19.0647 12.7878C18.8178 13.3638 18.098 13.8137 16.6582 14.7136L9.896 18.94C8.29805 19.9387 7.49907 20.4381 6.83973 20.385C6.26501 20.3388 5.73818 20.0469 5.3944 19.584C5 19.053 5 18.1108 5 16.2264V7.77357C5 5.88919 5 4.94701 5.3944 4.41598C5.73818 3.9531 6.26501 3.66111 6.83973 3.6149C7.49907 3.5619 8.29805 4.06126 9.896 5.05998L16.6582 9.28638Z"/></svg>`))
	scriptsStopIconSVG      = fyne.NewStaticResource("scripts-stop.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#F5F5F5"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>`))
	scriptsStopHoverIconSVG = fyne.NewStaticResource("scripts-stop-hover.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#fda4af"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>`))
	scriptsLogIconSVG       = fyne.NewStaticResource("scripts-log.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#F5F5F5"><path d="M4 6h16v2H4V6zm0 5h16v2H4v-2zm0 5h10v2H4v-2z"/></svg>`))
	scriptsEditIconSVG      = fyne.NewStaticResource("scripts-edit.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c5c8b5"><path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zm2.92 2.33H5v-.92l9.06-9.06.92.92L5.92 19.58zM20.71 7.04a1.003 1.003 0 0 0 0-1.42L18.37 3.29a1.003 1.003 0 0 0-1.42 0l-1.13 1.13 3.75 3.75 1.14-1.13z"/></svg>`))
	scriptsDeleteIconSVG    = fyne.NewStaticResource("scripts-delete.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c5c8b5"><path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/></svg>`))
	scriptsCopyIconSVG      = fyne.NewStaticResource("scripts-copy.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c5c8b5"><path d="M16 1H4c-1.1 0-2 .9-2 2v14h2V3h12V1zm3 4H8c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h11c1.1 0 2-.9 2-2V7c0-1.1-.9-2-2-2zm0 16H8V7h11v14z"/></svg>`))
)

// ScriptsMCPData is the left-column MCP proxy card.
type ScriptsMCPData struct {
	URL       string
	Running   bool
	Enabled   bool
	LocalUI   bool
	OnToggle  func()
	OnCopy    func()
	OnLocalUI func(bool)
}

// ScriptTableRow is one automation-script row.
type ScriptTableRow struct {
	Name     string
	Source   string
	Running  bool
	Error    string
	OnRun    func()
	OnStop   func()
	OnLog    func()
	OnEdit   func()
	OnDelete func()
	// BindStatus, when set, receives the live Running/Error painter so the
	// 2s poller can update STATE/Run without rebuilding the table.
	BindStatus func(func(running bool, errStr string))
}

// ScriptsSectionData is the Scripts tab body (headers + columns). Footer
// is owned by the widget, same as Snapshots.
type ScriptsSectionData struct {
	MCP           ScriptsMCPData
	ScriptCount   int
	NewEnabled    bool
	OnNewEMMC     func()
	OnNewSD       func()
	Rows          []ScriptTableRow
	LockedMessage string
	// Banner, when set, replaces the scripts table -- the same USBridge
	// Firmware strip Connections uses, shown only in this automation half.
	Banner fyne.CanvasObject
}

// NewScriptsSection builds sticky dual headers over a scrolling two-column
// body: MCP grid-style card on the left, scripts table on the right.
func NewScriptsSection(data ScriptsSectionData) fyne.CanvasObject {
	cols := &DeviceDashboardColumnsLayout{Gap: scriptsColumnGap, Ratio: scriptsColumnRatio}
	headerRow := container.New(&scriptsSplitColumnsLayout{Gap: scriptsColumnGap, Ratio: scriptsColumnRatio},
		newScriptsMCPHeader(),
		newScriptsHeaderDivider(),
		newScriptsAutomationHeader(data),
	)
	bodyRow := container.New(cols, NewScriptsMCPCard(data.MCP), newScriptsTableBody(data))

	headerPad := NewInsetExact(headerRow, scriptsContentInset, scriptsContentInset, 8, 0)
	bodyPad := NewInsetExact(bodyRow, scriptsContentInset, scriptsContentInset+10, 8, 12)
	scroll := container.NewVScroll(container.New(&snapshotsBodyTopLayout{}, bodyPad))
	return container.NewBorder(headerPad, nil, nil, nil, scroll)
}

func newScriptsHeaderDivider() fyne.CanvasObject {
	sep := canvas.NewRectangle(design.ColorConnectionsSectionUnderline)
	sep.SetMinSize(fyne.NewSize(1, 22))
	return sep
}

// scriptsSplitColumnsLayout is DeviceDashboardColumnsLayout with a short
// vertical rule sitting in the gap between the two headers.
type scriptsSplitColumnsLayout struct {
	Gap   float32
	Ratio float32
}

func (l *scriptsSplitColumnsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}
	left, sep, right := objects[0], objects[1], objects[2]
	ratio := l.Ratio
	if ratio <= 0 {
		ratio = 1
	}
	avail := maxFloat32(0, size.Width-l.Gap)
	leftW := avail / (ratio + 1)
	rightW := avail - leftW

	left.Move(fyne.NewPos(0, 0))
	left.Resize(fyne.NewSize(leftW, left.MinSize().Height))
	right.Move(fyne.NewPos(leftW+l.Gap, 0))
	right.Resize(fyne.NewSize(rightW, right.MinSize().Height))

	sepW := float32(1)
	sepH := sep.MinSize().Height
	if sepH > size.Height {
		sepH = size.Height
	}
	sep.Move(fyne.NewPos(leftW+(l.Gap-sepW)/2-scriptsHeaderDividerNudge, (size.Height-sepH)/2))
	sep.Resize(fyne.NewSize(sepW, sepH))
}

func (l *scriptsSplitColumnsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 3 {
		return fyne.NewSize(0, 0)
	}
	leftMin := objects[0].MinSize()
	rightMin := objects[2].MinSize()
	return fyne.NewSize(leftMin.Width+l.Gap+rightMin.Width, maxFloat32(leftMin.Height, rightMin.Height))
}

func newScriptsColumnHeader(title, subtitle string, badge, action fyne.CanvasObject) fyne.CanvasObject {
	titleText := NewBrandText(title, 18, design.ColorConnectionsSectionTitle, true)
	titleItems := []fyne.CanvasObject{container.NewCenter(titleText)}
	if badge != nil {
		gap := canvas.NewRectangle(color.Transparent)
		gap.SetMinSize(fyne.NewSize(10, 1))
		titleItems = append(titleItems, gap, container.NewCenter(badge))
	}
	titleRow := container.NewHBox(titleItems...)

	sub := canvas.NewText(subtitle, design.ColorConnectionsSectionSubtitle)
	sub.TextSize = 10
	left := container.NewVBox(titleRow, sub)

	var row fyne.CanvasObject
	if action != nil {
		row = container.NewHBox(container.NewCenter(left), layout.NewSpacer(), container.NewCenter(action))
	} else {
		row = container.NewHBox(container.NewCenter(left), layout.NewSpacer())
	}

	accentLine := canvas.NewRectangle(design.ColorConnectionsSectionUnderline)
	accentLine.SetMinSize(fyne.NewSize(1, 0.5))
	underlineRightGap := canvas.NewRectangle(color.Transparent)
	underlineRightGap.SetMinSize(fyne.NewSize(connectionsHeaderUnderlineRightPullback, 1))
	underline := container.NewBorder(nil, nil, nil, underlineRightGap, accentLine)

	return container.NewBorder(NewInset(row, 0, 0, 4, 8), underline, nil, nil)
}

func newScriptsMCPHeader() fyne.CanvasObject {
	return newScriptsColumnHeader("MCP", scriptsMCPSubtitle, nil, nil)
}

func newScriptsAutomationHeader(data ScriptsSectionData) fyne.CanvasObject {
	badge := newConnectionSortBadge(
		fmt.Sprintf("%d Scripts", data.ScriptCount),
		design.ColorConnectionBadgeText,
		false,
		nil,
	)
	return newScriptsColumnHeader("Automation Scripts", scriptsAutomationSubtitle, badge, newScriptsNewButtons(data))
}

func newScriptsNewButtons(data ScriptsSectionData) fyne.CanvasObject {
	emmc := newScriptsCreateButton("New eMMC", data.OnNewEMMC, data.NewEnabled)
	sd := newScriptsCreateButton("New SD Card", data.OnNewSD, data.NewEnabled)
	row := container.New(&DeviceRowControlsLayout{Gap: 4}, emmc, sd)

	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 8
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = 8
	border.StrokeColor = design.ColorTailscaleChipBorder
	border.StrokeWidth = 1
	if !data.NewEnabled {
		bg.FillColor = design.ColorSurfaceLight
		border.StrokeColor = color.Transparent
	}
	return container.NewStack(bg, border, NewInsetExact(row, 3, 3, 3, 3))
}

func newScriptsCreateButton(label string, onTap func(), enabled bool) *iconChromeButton {
	plusSVG := `<svg viewBox="0 0 24 24" fill="#4c6803"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>`
	plusIcon := fyne.NewStaticResource("scripts-new-"+label+".svg", []byte(plusSVG))
	plusIdle := fyne.NewStaticResource("scripts-new-idle-"+label+".svg", []byte(
		`<svg viewBox="0 0 24 24" fill="#8f9381"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>`))
	spec := iconChromeButtonSpec{
		NormalFill:         design.ColorConnectionAddFill,
		HoverFill:          design.ColorConnectionAddFillHover,
		DisabledFill:       connectionActionBlockedFill,
		Stroke:             color.Transparent,
		LabelColor:         color.NRGBA{R: 0x4c, G: 0x68, B: 0x03, A: 0xff},
		LabelSize:          9,
		LabelBold:          true,
		NormalIcon:         plusIcon,
		HoverIcon:          plusIcon,
		IconSize:           fyne.NewSize(12, 12),
		ButtonSize:         fyne.NewSize(0, 24),
		OnTapped:           onTap,
		MuteDisabledVisual: false,
		CornerRadius:       6,
	}
	if !enabled {
		spec.NormalFill = design.ColorSurfaceLight
		spec.HoverFill = design.ColorSurfaceLight
		spec.DisabledFill = design.ColorSurfaceLight
		spec.LabelColor = design.ColorConnectionsSectionMutedText
		spec.NormalIcon = plusIdle
		spec.HoverIcon = plusIdle
		spec.DisabledIcon = plusIdle
		spec.OnTapped = nil
	}
	btn := newIconChromeButton(spec)
	btn.SetText(label)
	btn.SetDisabled(!enabled)
	return btn
}

// NewScriptsMCPCard is a Connections-grid card: status + badge, endpoint
// stats box, local-models toggle, Start/Stop.
func NewScriptsMCPCard(data ScriptsMCPData) fyne.CanvasObject {
	running := data.Running
	statusDot := canvas.NewCircle(design.ColorBorder)
	if running {
		statusDot.FillColor = design.ColorConnectionBadgeText
	}
	dotWrap := container.NewCenter(container.NewGridWrap(fyne.NewSize(8, 8), statusDot))
	dotSlot := container.NewGridWrap(fyne.NewSize(16, 16), dotWrap)

	nameText := NewBrandText("MCP Proxy", 12, design.ColorTextLight, true)
	topLeft := container.New(&DeviceRowControlsLayout{Gap: 8}, dotSlot, nameText)
	topRow := container.NewBorder(nil, nil, topLeft, newScriptsMCPStateBadge(running))

	chips := NewInset(newConnectionCardChipsRow("Local endpoint", "", design.ColorConnectionBadgeText), 0, 0, 4, 8)

	url := strings.TrimSpace(data.URL)
	if url == "" {
		url = "none"
	}
	urlColor := color.Color(scriptsMCPURLColor)
	if url == "none" {
		urlColor = design.ColorTextMuted
	}
	statsBox := newScriptsMCPStatsBox(url, urlColor, data.OnCopy)

	dividerColor := color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}
	dividerLine := canvas.NewRectangle(dividerColor)
	dividerLine.SetMinSize(fyne.NewSize(1, 1))
	divider := NewInset(dividerLine, 0, 0, 4, 4)

	localLabel := canvas.NewText("Local models", design.ColorConnectionsSectionSubtitle)
	localLabel.TextSize = 10
	localToggle := NewDeviceToggle(data.LocalUI, func(on bool) {
		if data.OnLocalUI != nil {
			data.OnLocalUI(on)
		}
	})
	localToggle.ActiveFill = design.ColorConnectionBadgeText
	localRow := container.New(&DeviceRowControlsLayout{Gap: 8}, localToggle, container.NewCenter(localLabel))

	startBtn := newScriptsMCPStartButton(data)
	bottom := container.NewBorder(nil, nil, container.NewCenter(localRow), startBtn)

	inner := container.New(&tightStatsVBoxLayout{Gap: 0},
		topRow,
		chips,
		statsBox,
		divider,
		bottom,
	)
	content := NewInset(inner, 14, 14, 12, 12)

	cardBg := canvas.NewRectangle(design.ColorGray900)
	cardBg.CornerRadius = design.RadiusLG
	cardBg.StrokeColor = design.ColorTailscaleChipBorder
	cardBg.StrokeWidth = 1

	var hoverTimer *time.Timer
	setCardHovered := func(hovered bool) {
		if hovered {
			if hoverTimer != nil {
				hoverTimer.Stop()
				hoverTimer = nil
			}
			cardBg.StrokeColor = design.ColorConnectionBadgeText
			cardBg.Refresh()
			return
		}
		if hoverTimer != nil {
			hoverTimer.Stop()
		}
		hoverTimer = time.AfterFunc(50*time.Millisecond, func() {
			cardBg.StrokeColor = design.ColorTailscaleChipBorder
			cardBg.Refresh()
		})
	}
	localToggle.OnHover = setCardHovered
	startBtn.spec.OnHover = setCardHovered
	overlay := newConnectionCardOverlay(nil, setCardHovered)

	return container.NewStack(overlay, cardBg, content)
}

func newScriptsMCPStateBadge(running bool) fyne.CanvasObject {
	text := "Stopped"
	accent := color.Color(design.ColorConnectionsSectionSubtitle)
	if running {
		text = "Running"
		accent = design.ColorConnectionBadgeText
	}
	dot := canvas.NewCircle(accent)
	label := canvas.NewText(text, accent)
	label.TextSize = 9
	label.TextStyle.Monospace = true
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 3
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	chip := container.New(&typeBadgeLayout{}, bg, dot, label)
	return container.NewCenter(chip)
}

func newScriptsMCPStatsBox(url string, urlColor color.Color, onCopy func()) fyne.CanvasObject {
	labelText := canvas.NewText("ENDPOINT", color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff})
	labelText.TextSize = 10
	labelText.TextStyle.Monospace = true

	valueText := canvas.NewText(url, urlColor)
	valueText.TextSize = 9
	valueText.TextStyle.Monospace = true

	copyBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		Stroke:       color.Transparent,
		NormalIcon:   scriptsCopyIconSVG,
		HoverIcon:    scriptsCopyIconSVG,
		IconSize:     fyne.NewSize(12, 12),
		ButtonSize:   fyne.NewSize(22, 22),
		OnTapped:     onCopy,
		CornerRadius: 6,
	})

	valueRow := container.NewBorder(nil, nil, nil, copyBtn, valueText)
	rows := container.New(&tightStatsVBoxLayout{Gap: 4}, labelText, valueRow)

	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, NewInset(rows, 12, 12, 8, 8))
}

func newScriptsMCPStartButton(data ScriptsMCPData) *iconChromeButton {
	if data.Running {
		btn := newIconChromeButton(iconChromeButtonSpec{
			NormalFill:         color.Transparent,
			HoverFill:          deviceDashboardDisconnectHoverFill,
			DisabledFill:       deviceDashboardDisabledFill,
			Stroke:             design.ColorTailscaleChipBorder,
			HoverStroke:        deviceDashboardDisconnectHoverStroke,
			StrokeWidth:        1,
			CornerRadius:       6,
			NormalIcon:         scriptsStopIconSVG,
			HoverIcon:          scriptsStopHoverIconSVG,
			IconSize:           fyne.NewSize(9, 9),
			ButtonSize:         fyne.NewSize(0, 22),
			OnTapped:           data.OnToggle,
			LabelColor:         design.ColorTextLight,
			HoverLabelColor:    color.NRGBA{R: 0xfd, G: 0xa4, B: 0xaf, A: 0xff},
			LabelSize:          9,
			LabelBold:          true,
			MuteDisabledVisual: true,
		})
		btn.SetText("Stop")
		btn.SetDisabled(!data.Enabled && !data.Running)
		return btn
	}

	connectColor := design.ColorConnectionBadgeText
	connectHover := color.NRGBA{R: 0x61, G: 0xf0, B: 0xd3, A: 0xff}
	btn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:         connectColor,
		HoverFill:          connectHover,
		DisabledFill:       connectionActionBlockedFill,
		MuteDisabledVisual: true,
		Stroke:             color.Transparent,
		LabelColor:         design.ColorGray950,
		LabelBold:          true,
		LabelSize:          9,
		CornerRadius:       6,
		NormalIcon:         scriptsMCPPlayIconSVG,
		HoverIcon:          scriptsMCPPlayIconSVG,
		IconSize:           fyne.NewSize(9, 9),
		ButtonSize:         fyne.NewSize(0, 22),
		OnTapped:           data.OnToggle,
	})
	btn.SetText("Start")
	btn.SetDisabled(!data.Enabled)
	return btn
}

func newScriptsTableBody(data ScriptsSectionData) fyne.CanvasObject {
	if data.Banner != nil {
		return data.Banner
	}
	if strings.TrimSpace(data.LockedMessage) != "" {
		return newScriptsLockedCard(data.LockedMessage)
	}
	return NewScriptsListTable(data.Rows)
}

func newScriptsLockedCard(msg string) fyne.CanvasObject {
	label := canvas.NewText(msg, design.ColorConnectionsSectionSubtitle)
	label.TextSize = 11
	label.Alignment = fyne.TextAlignCenter
	inner := NewInset(container.NewCenter(label), 16, 16, 24, 24)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusLG
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, inner)
}

// NewScriptsListTable is the connections/snapshots list card for scripts.
// Past scriptsVisibleRows the data rows scroll internally, same idea as
// Devices' Storage card (dashboardStorageVisibleRows).
func NewScriptsListTable(rows []ScriptTableRow) fyne.CanvasObject {
	labels, widths := scriptListColumnLabels, scriptListColumnWidths
	dividerColor := color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}
	newDivider := func() fyne.CanvasObject {
		sep := canvas.NewRectangle(dividerColor)
		sep.SetMinSize(fyne.NewSize(1, 1))
		return NewInset(sep, 0, 0, 2, 2)
	}

	header := newConnectionListHeaderRow(labels, widths)
	var dataItems []fyne.CanvasObject
	if len(rows) == 0 {
		dataItems = []fyne.CanvasObject{newDivider(), newScriptsEmptyRow(widths)}
	} else {
		for _, row := range rows {
			dataItems = append(dataItems, newDivider(), newScriptListRow(row, widths))
		}
	}
	dataCol := container.New(&tightStatsVBoxLayout{Gap: 0}, dataItems...)
	// Same 10px right inset Devices uses on the Storage row list so the
	// overlay scrollbar thumb clears the ACTIONS buttons without leaving a
	// reserved empty lane.
	dataScroll := container.NewVScroll(NewInsetExact(dataCol, 0, scriptsTableScrollGutter, 0, 0))
	height := scriptsRowsCapHeight(dataItems, len(rows))
	if height <= 0 {
		height = dataCol.MinSize().Height
	}
	dataScroll.SetMinSize(fyne.NewSize(0, height))

	headerObj := NewInsetExact(header, 0, scriptsTableScrollGutter, 0, 0)
	body := container.New(&tightStatsVBoxLayout{Gap: 0}, headerObj, dataScroll)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusLG
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, NewInset(body, 16, 16, 12, 12))
}

func scriptsRowsCapHeight(items []fyne.CanvasObject, dataRowCount int) float32 {
	if dataRowCount <= scriptsVisibleRows {
		return 0
	}
	capCount := scriptsVisibleRows * 2
	if capCount > len(items) {
		capCount = len(items)
	}
	return (&tightStatsVBoxLayout{Gap: 0}).MinSize(items[:capCount]).Height
}

func newScriptsEmptyRow(widths []float32) fyne.CanvasObject {
	label := canvas.NewText("No scripts yet", design.ColorConnectionsSectionSubtitle)
	label.TextSize = 11
	empty := canvas.NewRectangle(color.Transparent)
	return container.New(&connectionsTableRowLayout{Widths: widths, Gap: connectionListColumnGap},
		label, empty, empty, empty)
}

func newScriptListRow(row ScriptTableRow, widths []float32) fyne.CanvasObject {
	nameCell := newScriptsNameCell(row.Name, scriptNameColor(row.Running, row.Error))
	sourceCell := container.NewCenter(newScriptsSourceChip(row.Source))
	stateChip := newScriptsLiveStateChip(row.Running, row.Error)
	actions := newScriptsActionsCell(row)

	if row.BindStatus != nil {
		row.BindStatus(func(running bool, errStr string) {
			nameCell.SetColor(scriptNameColor(running, errStr))
			stateChip.Set(running, errStr)
			actions.SetActive(running, errStr)
		})
	}

	actionsCell := container.NewBorder(nil, nil, nil, actions)
	return container.New(&connectionsTableRowLayout{Widths: widths, Gap: connectionListColumnGap},
		nameCell, sourceCell, container.NewCenter(stateChip), actionsCell)
}

func scriptNameColor(running bool, errStr string) color.Color {
	if running {
		return DeviceDashboardAccentLime
	}
	if strings.TrimSpace(errStr) != "" {
		return color.NRGBA{R: 0xff, G: 0x5a, B: 0x52, A: 0xff}
	}
	return design.ColorTextLight
}

func newScriptsSourceChip(source string) fyne.CanvasObject {
	text := strings.TrimSpace(source)
	if text == "" {
		text = "eMMC"
	}
	accent := color.Color(design.ColorConnectionsSectionSubtitle)
	if strings.EqualFold(text, "SD") {
		accent = design.ColorConnectionBadgeText
	}
	label := canvas.NewText(text, accent)
	label.TextSize = 9
	label.TextStyle.Monospace = true
	dot := canvas.NewCircle(accent)
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 3
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	return container.NewCenter(container.New(&typeBadgeLayout{}, bg, dot, label))
}

type scriptsLiveStateChip struct {
	widget.BaseWidget
	running bool
	errStr  string
	dot     *canvas.Circle
	label   *canvas.Text
}

func newScriptsLiveStateChip(running bool, errStr string) *scriptsLiveStateChip {
	c := &scriptsLiveStateChip{running: running, errStr: errStr}
	c.ExtendBaseWidget(c)
	return c
}

func (c *scriptsLiveStateChip) Set(running bool, errStr string) {
	c.running = running
	c.errStr = errStr
	c.refreshVisuals()
}

func (c *scriptsLiveStateChip) refreshVisuals() {
	if c.dot == nil || c.label == nil {
		return
	}
	text, accent := scriptsStateAppearance(c.running, c.errStr)
	c.dot.FillColor = accent
	c.label.Text = text
	c.label.Color = accent
	c.dot.Refresh()
	c.label.Refresh()
}

func (c *scriptsLiveStateChip) CreateRenderer() fyne.WidgetRenderer {
	text, accent := scriptsStateAppearance(c.running, c.errStr)
	c.dot = canvas.NewCircle(accent)
	c.label = canvas.NewText(text, accent)
	c.label.TextSize = 9
	c.label.TextStyle.Monospace = true
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 3
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	return widget.NewSimpleRenderer(container.New(&typeBadgeLayout{}, bg, c.dot, c.label))
}

func scriptsStateAppearance(running bool, errStr string) (string, color.Color) {
	text := "Idle"
	accent := color.Color(design.ColorConnectionsSectionSubtitle)
	switch {
	case running:
		text = "Running"
		accent = design.ColorConnectionAddFill
	case strings.TrimSpace(errStr) != "":
		text = "Error"
		accent = color.NRGBA{R: 0xff, G: 0x5a, B: 0x52, A: 0xff}
	}
	return text, accent
}

type scriptsActionsCell struct {
	widget.BaseWidget

	row       ScriptTableRow
	runBtn    *iconChromeButton
	stopBtn   *iconChromeButton
	editBtn   *iconChromeButton
	deleteBtn *iconChromeButton
	box       *fyne.Container
}

func newScriptsActionsCell(row ScriptTableRow) *scriptsActionsCell {
	c := &scriptsActionsCell{row: row}
	c.ExtendBaseWidget(c)
	return c
}

func (c *scriptsActionsCell) SetActive(running bool, errStr string) {
	c.row.Running = running
	c.row.Error = errStr
	if c.runBtn == nil || c.stopBtn == nil {
		return
	}
	c.applyActionVisibility()
	if c.box != nil {
		c.box.Refresh()
	}
}

func (c *scriptsActionsCell) applyActionVisibility() {
	running := c.row.Running
	showStop := running || strings.TrimSpace(c.row.Error) != ""
	if showStop {
		c.runBtn.Hide()
		c.stopBtn.Show()
	} else {
		c.stopBtn.Hide()
		c.runBtn.Show()
	}
	if c.editBtn != nil && c.deleteBtn != nil {
		if running {
			c.editBtn.Hide()
			c.deleteBtn.Hide()
		} else {
			c.editBtn.Show()
			c.deleteBtn.Show()
		}
	}
}

func (c *scriptsActionsCell) CreateRenderer() fyne.WidgetRenderer {
	logBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		Stroke:       design.ColorTailscaleChipBorder,
		StrokeWidth:  1,
		CornerRadius: 6,
		NormalIcon:   scriptsLogIconSVG,
		HoverIcon:    scriptsLogIconSVG,
		IconSize:     fyne.NewSize(11, 11),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     c.row.OnLog,
	})
	c.editBtn = newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		Stroke:       design.ColorTailscaleChipBorder,
		StrokeWidth:  1,
		CornerRadius: 6,
		NormalIcon:   scriptsEditIconSVG,
		HoverIcon:    scriptsEditIconSVG,
		IconSize:     fyne.NewSize(11, 11),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     c.row.OnEdit,
	})
	c.deleteBtn = newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		Stroke:       design.ColorTailscaleChipBorder,
		StrokeWidth:  1,
		CornerRadius: 6,
		NormalIcon:   scriptsDeleteIconSVG,
		HoverIcon:    scriptsDeleteIconSVG,
		IconSize:     fyne.NewSize(11, 11),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     c.row.OnDelete,
	})

	connectColor := color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0xff}
	connectHover := color.NRGBA{R: 0xd4, G: 0xf7, B: 0x8a, A: 0xff}
	c.runBtn = newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   connectColor,
		HoverFill:    connectHover,
		Stroke:       color.Transparent,
		CornerRadius: 6,
		NormalIcon:   scriptsPlayIconSVG,
		HoverIcon:    scriptsPlayIconSVG,
		IconSize:     fyne.NewSize(10, 10),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     c.row.OnRun,
	})
	c.stopBtn = newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    deviceDashboardDisconnectHoverFill,
		Stroke:       design.ColorTailscaleChipBorder,
		HoverStroke:  deviceDashboardDisconnectHoverStroke,
		StrokeWidth:  1,
		CornerRadius: 6,
		NormalIcon:   scriptsStopIconSVG,
		HoverIcon:    scriptsStopHoverIconSVG,
		IconSize:     fyne.NewSize(9, 9),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     c.row.OnStop,
	})
	c.applyActionVisibility()

	c.box = container.New(&DeviceRowControlsLayout{Gap: 6}, c.deleteBtn, c.editBtn, logBtn, c.runBtn, c.stopBtn)
	return widget.NewSimpleRenderer(c.box)
}

const (
	scriptNameTextSize float32 = 9
	scriptNameLineH    float32 = 11
	scriptNameLineGap  float32 = 1
	// Small floor so a long name cannot inflate the NAME flex column's
	// MinSize and shove SOURCE/STATE/ACTIONS off the card. Wrapping uses
	// the width Layout actually assigned.
	scriptNameMinWidth float32 = 48
)

type scriptsNameCell struct {
	widget.BaseWidget
	raw   string
	col   color.Color
	line1 *canvas.Text
	line2 *canvas.Text
}

func newScriptsNameCell(name string, col color.Color) *scriptsNameCell {
	c := &scriptsNameCell{raw: name, col: col}
	c.ExtendBaseWidget(c)
	return c
}

func (c *scriptsNameCell) SetColor(col color.Color) {
	c.col = col
	if c.line1 != nil {
		c.line1.Color = col
		c.line1.Refresh()
	}
	if c.line2 != nil {
		c.line2.Color = col
		c.line2.Refresh()
	}
}

func (c *scriptsNameCell) CreateRenderer() fyne.WidgetRenderer {
	c.line1 = NewBrandText("", scriptNameTextSize, c.col, true)
	c.line2 = NewBrandText("", scriptNameTextSize, c.col, true)
	c.line2.Hide()
	return &scriptsNameCellRenderer{cell: c, objects: []fyne.CanvasObject{c.line1, c.line2}}
}

type scriptsNameCellRenderer struct {
	cell    *scriptsNameCell
	objects []fyne.CanvasObject
}

func (r *scriptsNameCellRenderer) Destroy() {}

func (r *scriptsNameCellRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *scriptsNameCellRenderer) Refresh() {
	if r.cell.line1 != nil {
		r.cell.line1.Color = r.cell.col
		r.cell.line1.Refresh()
	}
	if r.cell.line2 != nil {
		r.cell.line2.Color = r.cell.col
		r.cell.line2.Refresh()
	}
	canvas.Refresh(r.cell)
}

func (r *scriptsNameCellRenderer) MinSize() fyne.Size {
	return fyne.NewSize(scriptNameMinWidth, scriptNameLineH*2+scriptNameLineGap)
}

func (r *scriptsNameCellRenderer) Layout(size fyne.Size) {
	l1, l2 := wrapScriptName(r.cell.raw, size.Width)
	r.cell.line1.Text = l1
	r.cell.line1.Color = r.cell.col
	total := scriptNameLineH
	if l2 != "" {
		total = scriptNameLineH*2 + scriptNameLineGap
	}
	y := (size.Height - total) / 2
	if y < 0 {
		y = 0
	}
	r.cell.line1.Move(fyne.NewPos(0, y))
	r.cell.line1.Resize(fyne.NewSize(size.Width, scriptNameLineH))
	r.cell.line1.Refresh()
	if l2 == "" {
		r.cell.line2.Hide()
		return
	}
	r.cell.line2.Show()
	r.cell.line2.Text = l2
	r.cell.line2.Color = r.cell.col
	r.cell.line2.Move(fyne.NewPos(0, y+scriptNameLineH+scriptNameLineGap))
	r.cell.line2.Resize(fyne.NewSize(size.Width, scriptNameLineH))
	r.cell.line2.Refresh()
}

func wrapScriptName(name string, width float32) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	if width < scriptNameMinWidth {
		width = scriptNameMinWidth
	}
	style := fyne.TextStyle{Bold: true}
	if fyne.MeasureText(name, scriptNameTextSize, style).Width <= width {
		return name, ""
	}
	runes := []rune(name)
	best := -1
	for i, r := range runes {
		if r != '_' && r != '-' && r != '.' && r != ' ' {
			continue
		}
		if fyne.MeasureText(string(runes[:i+1]), scriptNameTextSize, style).Width <= width {
			best = i + 1
		}
	}
	if best > 0 && best < len(runes) {
		return strings.TrimSpace(string(runes[:best])), strings.TrimSpace(string(runes[best:]))
	}
	lo, hi := 1, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fyne.MeasureText(string(runes[:mid]), scriptNameTextSize, style).Width <= width {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo < 1 {
		lo = 1
	}
	if lo >= len(runes) {
		return name, ""
	}
	return string(runes[:lo]), string(runes[lo:])
}
