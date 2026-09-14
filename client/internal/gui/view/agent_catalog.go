package view

// agent_catalog.go -- Connections footer's Agent dialog: four host-service
// editions on the left, feature copy on the right. Chrome (title, X,
// Download/GitHub) lives in the controller; this file is the two-column
// body. Feature strings are placeholders until product copy is filled in.

import (
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

const (
	AgentCatalogWebsiteURL = "https://www.usbridge.io/software-agent"
	AgentCatalogGitHubURL  = "https://github.com/USBridge-Technologies/USBridge-Remote"

	AgentCatalogTitle      = "Software Agent"
	AgentCatalogSubtitle   = "Install the host service on the machine you want to control."
	AgentCatalogFooterHint = "The Agent is installed on the target machine, not this client."
)

type agentEditionKind int

const (
	agentEditionList agentEditionKind = iota
	agentEditionProPlus
)

type agentEdition struct {
	Title    string
	Tag      string
	Pro      bool
	Kind     agentEditionKind
	Features []string
}

var agentCatalogEditions = []agentEdition{
	{
		Title: "Sunshine",
		Tag:   "Open Source",
		Kind:  agentEditionList,
		Features: []string{
			"Ultra-low latency streaming",
			"Shared clipboard",
			"Multi-monitor support",
		},
	},
	{
		Title: "USBridge Streamer",
		Tag:   "Free",
		Kind:  agentEditionList,
		Features: []string{
			"Browser web client",
			"Windows pre-login access",
			"Fast connect",
		},
	},
	{
		Title: "USBridge Streamer",
		Tag:   "Pro",
		Pro:   true,
		Kind:  agentEditionProPlus,
		Features: []string{
			"4:4:4 color fidelity",
			"USB device emulation",
		},
	},
	{
		Title: "USBridge Streamer",
		Tag:   "Enterprise",
		Pro:   true,
		Kind:  agentEditionProPlus,
		Features: []string{
			"Session recording and audit logs",
			"Built for company-wide rollout",
		},
	},
}

// AgentCatalogBody is the two-column catalog: a short edition list and
// the selected edition's feature pane.
type AgentCatalogBody struct {
	widget.BaseWidget

	selected int
	rows     []*agentEditionRow
	right    *fyne.Container
}

func NewAgentCatalogBody() *AgentCatalogBody {
	b := &AgentCatalogBody{}
	b.ExtendBaseWidget(b)
	return b
}

func (b *AgentCatalogBody) CreateRenderer() fyne.WidgetRenderer {
	b.rows = make([]*agentEditionRow, 0, len(agentCatalogEditions))
	leftItems := make([]fyne.CanvasObject, 0, len(agentCatalogEditions)*2-1)
	gridItems := make([]fyne.CanvasObject, 0, len(agentCatalogEditions))
	for i, ed := range agentCatalogEditions {
		idx := i
		row := newAgentEditionRow(ed.Title, ed.Tag, ed.Pro, func() { b.selectEdition(idx) })
		b.rows = append(b.rows, row)
		gridItems = append(gridItems, row)
		if i > 0 {
			leftItems = append(leftItems, newAgentListSeparator())
		}
		leftItems = append(leftItems, row)
	}
	b.right = container.NewMax(newAgentFeaturePane(agentCatalogEditions[0]))
	b.selectEdition(0)

	if IsMobile() {
		grid := container.New(&agentCatalogMobileGridLayout{Gap: 6}, gridItems...)
		rule := canvas.NewRectangle(design.ColorConnectionsSectionUnderline)
		rule.SetMinSize(fyne.NewSize(1, 1))
		body := container.New(&tightStatsVBoxLayout{Gap: 8},
			grid,
			rule,
			NewMobileFillWidth(b.right),
		)
		return widget.NewSimpleRenderer(NewMobileFillWidth(body))
	}

	left := container.New(&tightStatsVBoxLayout{Gap: 0}, leftItems...)
	sep := canvas.NewRectangle(design.ColorConnectionsSectionUnderline)
	sep.SetMinSize(fyne.NewSize(1, 1))
	rightPad := NewInsetExact(b.right, 16, 0, 0, 0)
	cols := container.New(&agentCatalogSplitLayout{Gap: 14, Ratio: 2.1}, left, sep, rightPad)
	return widget.NewSimpleRenderer(cols)
}

// agentCatalogMobileGridLayout is a 2-column edition picker for the phone
// catalog -- list on top, feature copy underneath (see CreateRenderer).
type agentCatalogMobileGridLayout struct {
	Gap float32
}

func (l *agentCatalogMobileGridLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var cellH float32
	n := 0
	for _, obj := range objects {
		if obj == nil || !obj.Visible() {
			continue
		}
		n++
		if h := obj.MinSize().Height; h > cellH {
			cellH = h
		}
	}
	if n == 0 {
		return fyne.NewSize(1, 0)
	}
	rows := (n + 1) / 2
	return fyne.NewSize(1, float32(rows)*cellH+float32(maxInt(rows-1, 0))*l.Gap)
}

func (l *agentCatalogMobileGridLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	visible := make([]fyne.CanvasObject, 0, len(objects))
	for _, obj := range objects {
		if obj != nil && obj.Visible() {
			visible = append(visible, obj)
		}
	}
	if len(visible) == 0 {
		return
	}
	cols := 2
	slotW := (size.Width - l.Gap) / float32(cols)
	if slotW < 0 {
		slotW = 0
	}
	var cellH float32
	for _, obj := range visible {
		if h := obj.MinSize().Height; h > cellH {
			cellH = h
		}
	}
	for i, obj := range visible {
		col := i % cols
		row := i / cols
		obj.Move(fyne.NewPos(float32(col)*(slotW+l.Gap), float32(row)*(cellH+l.Gap)))
		obj.Resize(fyne.NewSize(slotW, cellH))
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func newAgentListSeparator() fyne.CanvasObject {
	sep := canvas.NewRectangle(design.ColorConnectionsSectionUnderline)
	sep.SetMinSize(fyne.NewSize(0, 1))
	return sep
}

// agentCatalogSplitLayout is a narrow list | rule | wide features row.
type agentCatalogSplitLayout struct {
	Gap   float32
	Ratio float32
}

func (l *agentCatalogSplitLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}
	left, sep, right := objects[0], objects[1], objects[2]
	ratio := l.Ratio
	if ratio <= 0 {
		ratio = 1
	}
	avail := size.Width - l.Gap
	if avail < 0 {
		avail = 0
	}
	leftW := avail / (ratio + 1)
	rightW := avail - leftW
	sepW := float32(1)

	left.Move(fyne.NewPos(0, 0))
	left.Resize(fyne.NewSize(leftW, left.MinSize().Height))
	sep.Move(fyne.NewPos(leftW+(l.Gap-sepW)/2, 0))
	sep.Resize(fyne.NewSize(sepW, size.Height))
	right.Move(fyne.NewPos(leftW+l.Gap, 0))
	right.Resize(fyne.NewSize(rightW, maxFloat32(right.MinSize().Height, size.Height)))
}

func (l *agentCatalogSplitLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 3 {
		return fyne.NewSize(0, 0)
	}
	leftMin := objects[0].MinSize()
	rightMin := objects[2].MinSize()
	return fyne.NewSize(leftMin.Width+l.Gap+rightMin.Width, maxFloat32(leftMin.Height, rightMin.Height))
}

func (b *AgentCatalogBody) selectEdition(idx int) {
	if idx < 0 || idx >= len(agentCatalogEditions) {
		return
	}
	b.selected = idx
	for i, row := range b.rows {
		row.SetSelected(i == idx)
	}
	if b.right != nil {
		b.right.Objects = []fyne.CanvasObject{newAgentFeaturePane(agentCatalogEditions[idx])}
		b.right.Refresh()
	}
}

var (
	_ fyne.Tappable     = (*agentEditionRow)(nil)
	_ desktop.Hoverable = (*agentEditionRow)(nil)
)

type agentEditionRow struct {
	widget.BaseWidget

	title    string
	tag      string
	pro      bool
	selected bool
	hovered  bool
	onTap    func()

	name *canvas.Text
	qual *canvas.Text
	bg   *canvas.Rectangle
}

func (r *agentEditionRow) mobileButtonLabel() string {
	if r.title == "Sunshine" {
		return r.title
	}
	if r.tag != "" {
		return r.tag
	}
	return r.title
}

func newAgentEditionRow(title, tag string, pro bool, onTap func()) *agentEditionRow {
	r := &agentEditionRow{title: title, tag: tag, pro: pro, onTap: onTap}
	r.ExtendBaseWidget(r)
	return r
}

func (r *agentEditionRow) SetSelected(on bool) {
	r.selected = on
	r.refreshVisuals()
}

func (r *agentEditionRow) Tapped(*fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}

func (r *agentEditionRow) TappedSecondary(*fyne.PointEvent) {}

func (r *agentEditionRow) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (r *agentEditionRow) MouseIn(*desktop.MouseEvent) {
	r.hovered = true
	r.refreshVisuals()
}

func (r *agentEditionRow) MouseMoved(*desktop.MouseEvent) {}

func (r *agentEditionRow) MouseOut() {
	r.hovered = false
	r.refreshVisuals()
}

func (r *agentEditionRow) refreshVisuals() {
	if r.name == nil {
		return
	}
	if r.bg != nil {
		switch {
		case r.selected:
			r.bg.FillColor = design.ColorSurfaceLight
			r.bg.StrokeColor = design.ColorConnectionBadgeBorder
		case r.hovered:
			r.bg.FillColor = design.ColorBorder
			r.bg.StrokeColor = design.ColorHeaderAccentLine
		default:
			r.bg.FillColor = design.ColorGray900
			r.bg.StrokeColor = design.ColorHeaderAccentLine
		}
		r.bg.StrokeWidth = 1
		r.bg.Refresh()
		switch {
		case r.pro:
			r.name.Color = design.ColorPro
		case r.selected:
			r.name.Color = design.ColorConnectionBadgeText
		case r.hovered:
			r.name.Color = design.ColorTextLight
		default:
			r.name.Color = design.ColorConnectionsSectionTitle
		}
		r.name.Refresh()
		return
	}
	if r.qual == nil {
		return
	}
	switch {
	case r.selected:
		r.name.Color = design.ColorConnectionBadgeText
	case r.hovered:
		r.name.Color = design.ColorTextLight
	default:
		r.name.Color = design.ColorConnectionsSectionTitle
	}
	if r.pro {
		r.qual.Color = design.ColorPro
	} else if r.selected {
		r.qual.Color = design.ColorConnectionBadgeText
	} else {
		r.qual.Color = design.ColorConnectionsSectionMutedText
	}
	r.name.Refresh()
	r.qual.Refresh()
}

func (r *agentEditionRow) CreateRenderer() fyne.WidgetRenderer {
	r.name = canvas.NewText(r.title, design.ColorConnectionsSectionTitle)
	r.name.TextSize = 10
	r.qual = canvas.NewText(" ("+r.tag+")", design.ColorConnectionsSectionMutedText)
	r.qual.TextSize = 10
	if IsMobile() {
		r.bg = canvas.NewRectangle(design.ColorGray900)
		r.bg.CornerRadius = 6
		r.bg.StrokeWidth = 1
		r.name.Text = r.mobileButtonLabel()
		r.name.TextSize = 10
		r.name.TextStyle.Bold = true
		r.name.Alignment = fyne.TextAlignCenter
		r.qual = nil
		heightLock := canvas.NewRectangle(color.Transparent)
		heightLock.SetMinSize(fyne.NewSize(1, 24))
		r.refreshVisuals()
		return widget.NewSimpleRenderer(container.NewMax(r.bg, heightLock, container.NewCenter(r.name)))
	}
	line := container.New(&DeviceRowControlsLayout{Gap: 0}, r.name, r.qual)
	var inner fyne.CanvasObject = line
	if r.pro {
		star := canvas.NewImageFromResource(assets.StarProIcon)
		star.FillMode = canvas.ImageFillContain
		star.SetMinSize(fyne.NewSize(10, 10))
		inner = container.New(&DeviceRowControlsLayout{Gap: 5}, container.NewCenter(star), line)
	}
	r.refreshVisuals()
	return widget.NewSimpleRenderer(NewInsetExact(inner, 2, 4, 4, 4))
}

func (ed agentEdition) headerBadge() (label string, accent color.Color, ok bool) {
	switch ed.Tag {
	case "Open Source", "Free":
		return ed.Tag, design.ColorConnectionBadgeText, true
	case "Pro", "Enterprise":
		return ed.Tag, design.ColorPro, true
	default:
		return "", nil, false
	}
}

func newAgentFeaturePane(ed agentEdition) fyne.CanvasObject {
	badgeLabel, badgeAccent, hasBadge := ed.headerBadge()
	titleText := ed.Title
	if ed.Tag != "" && !hasBadge {
		titleText = ed.Title + " (" + ed.Tag + ")"
	}
	title := NewBrandText(titleText, 13, design.ColorConnectionsSectionTitle, true)
	var left fyne.CanvasObject = title
	if hasBadge {
		badge := newConnectionSortBadge(badgeLabel, badgeAccent, true, nil)
		left = container.New(&DeviceRowControlsLayout{Gap: 8}, title, container.NewCenter(badge))
	}
	var titleRow fyne.CanvasObject = left
	showBasic := ed.Tag == "Free" || ed.Kind == agentEditionProPlus
	if showBasic {
		chip := newAgentBasicChip()
		if IsMobile() {
			titleRow = container.New(&DeviceRowControlsLayout{Gap: 6}, left, container.NewCenter(chip))
		} else {
			titleRow = container.NewBorder(nil, nil, left, chip)
		}
	}

	items := make([]fyne.CanvasObject, 0, 8)
	items = append(items, titleRow)
	if ed.Kind == agentEditionProPlus {
		for _, feat := range ed.Features {
			items = append(items, newAgentPlusFeature(feat))
		}
	} else {
		for _, feat := range ed.Features {
			items = append(items, newAgentPlainFeature(feat))
		}
	}
	pane := container.NewVBox(items...)
	if IsMobile() {
		return NewMobileFillWidth(pane)
	}
	return pane
}

func newAgentBasicChip() fyne.CanvasObject {
	label := canvas.NewText("+ Basic functionality", design.ColorConnectionsSectionMutedText)
	label.TextSize = 8
	label.TextStyle.Bold = true
	bg := canvas.NewRectangle(design.ColorSurfaceLight)
	bg.CornerRadius = 20
	bg.StrokeColor = design.ColorConnectionBadgeBorder
	bg.StrokeWidth = 1
	pad := float32(8)
	vpad := float32(2)
	if IsMobile() {
		pad, vpad = 6, 1
	}
	inner := NewInsetExact(container.NewCenter(label), pad, pad, vpad, vpad)
	return container.New(&agentBasicChipLayout{}, bg, inner)
}

// agentBasicChipLayout sizes the chip to its label -- Stack would stretch
// the pill across the whole title row.
type agentBasicChipLayout struct{}

func (l *agentBasicChipLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	return objects[1].MinSize()
}

func (l *agentBasicChipLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	min := objects[1].MinSize()
	y := (size.Height - min.Height) / 2
	if y < 0 {
		y = 0
	}
	for _, o := range objects {
		o.Move(fyne.NewPos(0, y))
		o.Resize(min)
	}
}

func newAgentPlainFeature(text string) fyne.CanvasObject {
	dot := canvas.NewRectangle(design.ColorConnectionBadgeText)
	dot.CornerRadius = 5
	dot.SetMinSize(fyne.NewSize(5, 5))
	label := canvas.NewText(text, design.ColorConnectionsSectionTitle)
	label.TextSize = 11
	return NewInsetExact(container.New(&DeviceRowControlsLayout{Gap: 8}, container.NewCenter(dot), label), 0, 0, 3, 3)
}

func newAgentPlusFeature(text string) fyne.CanvasObject {
	plus := canvas.NewText("+", design.ColorPro)
	plus.TextSize = 12
	plus.TextStyle.Bold = true
	label := canvas.NewText(text, design.ColorConnectionsSectionTitle)
	label.TextSize = 11
	return NewInsetExact(container.New(&DeviceRowControlsLayout{Gap: 8}, plus, label), 0, 0, 3, 3)
}
