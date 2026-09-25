package view

// scripts_tab_web_bridge.go -- the wasm/browser build's MCP card
// (ScriptsMCPData.WebBridge == true, see that type's doc comment).
//
// Desktop's card (NewScriptsMCPCard's main branch, scripts_tab.go) is a
// bare endpoint URL + Start/Stop, because MCPProxy can run a real local
// HTTP listener there -- any MCP client's "url" server entry just points
// straight at it. A browser tab structurally cannot accept an inbound
// connection at all (no raw TCP/HTTP listen in a browser sandbox), so
// there is no URL to hand out here. Instead: the pasted config's `node -e`
// fetches a small local relay script straight from GitHub at launch
// (client/web/mcp-bridge/bridge.mjs, see that file's own doc comment for
// the full topology -- and MCPBridgeConfigJSON for why fetch+eval instead
// of a downloaded file) that Claude Desktop spawns over stdio -- the one
// transport every MCP client already supports without needing "url"/SSE
// support -- and that itself opens a local WebSocket server this browser
// tab dials OUT to (the one direction a browser sandbox permits).
// BridgeConnected reflects whether that outbound connection is currently
// up; OnToggleBridge dials/closes it.

import (
	"image/color"
	"strings"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func newScriptsMCPCardWebBridge(data ScriptsMCPData) fyne.CanvasObject {
	statusDot := canvas.NewCircle(design.ColorBorder)
	if data.BridgeConnected {
		statusDot.FillColor = design.ColorConnectionBadgeText
	}
	dotWrap := container.NewCenter(container.NewGridWrap(fyne.NewSize(8, 8), statusDot))
	dotSlot := container.NewGridWrap(fyne.NewSize(16, 16), dotWrap)
	nameText := NewBrandText("MCP Bridge", 12, design.ColorTextLight, true)
	topLeft := container.New(&DeviceRowControlsLayout{Gap: 8}, dotSlot, nameText)
	topRow := container.NewBorder(nil, nil, topLeft, newScriptsMCPStateBadge(data.BridgeConnected))

	explainer := widget.NewLabel("A browser tab can't accept an incoming connection, so Claude Desktop " +
		"instead runs a small script (fetched automatically, nothing to download) that this page " +
		"connects out to. Paste the config below into Claude Desktop, then connect.")
	explainer.Wrapping = fyne.TextWrapWord

	configLabel := canvas.NewText("CLAUDE DESKTOP CONFIG", color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff})
	configLabel.TextSize = 10
	configLabel.TextStyle.Monospace = true

	copyBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		Stroke:       color.Transparent,
		NormalIcon:   scriptsCopyIconSVG,
		HoverIcon:    scriptsCopyIconSVG,
		IconSize:     fyne.NewSize(12, 12),
		ButtonSize:   fyne.NewSize(22, 22),
		OnTapped:     data.OnCopyBridgeConfig,
		CornerRadius: 6,
	})
	configLabelRow := container.NewBorder(nil, nil, nil, copyBtn, configLabel)

	var configRows []fyne.CanvasObject
	for _, line := range strings.Split(data.BridgeConfigJSON, "\n") {
		configRows = append(configRows, newConfigLineText(line, scriptsMCPURLColor, 9))
	}
	configBlock := container.New(&tightStatsVBoxLayout{Gap: 1}, configRows...)

	pathHint := widget.NewLabel("Requires Node.js on the machine running Claude Desktop.")
	pathHint.Wrapping = fyne.TextWrapWord

	configInner := container.New(&tightStatsVBoxLayout{Gap: 4}, configLabelRow, configBlock)
	configBg := canvas.NewRectangle(design.ColorGray950)
	configBg.CornerRadius = 6
	configBg.StrokeColor = design.ColorTailscaleChipBorder
	configBg.StrokeWidth = 1
	configBox := container.NewStack(configBg, NewInset(configInner, 12, 12, 8, 8))

	// Independent of BridgeConnected: this toggle is what actually lazily
	// loads the onnxruntime-web icon_detect session (api.LazyInitLocalUIParse,
	// see applyLocalUIParseSetting) for the MCP bridge's own ui.parse
	// interception (MCPBrowserBridge.handle's tryLocalUIParse call, mirroring
	// desktop's MCPProxy.handle) -- same toggle/meaning as desktop's card,
	// just dropped by oversight when this platform-specific card was first
	// split out. Left off: ui.parse just keeps forwarding to the paired
	// device/agent, the same "optional accelerator, never a hard dependency"
	// fallback every platform already has.
	localLabel := canvas.NewText("USE LOCAL MODELS", color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff})
	localLabel.TextSize = 10
	localLabel.TextStyle.Monospace = true
	localToggle := NewDeviceToggle(data.LocalUI, func(on bool) {
		if data.OnLocalUI != nil {
			data.OnLocalUI(on)
		}
	})
	localToggle.ActiveFill = design.ColorConnectionBadgeText
	localRow := container.New(&DeviceRowControlsLayout{Gap: 8}, localToggle, localLabel)

	toggleLabel := "Connect"
	if data.BridgeConnected {
		toggleLabel = "Disconnect"
	}
	toggleBtn := widget.NewButton(toggleLabel, data.OnToggleBridge)
	toggleBtn.Importance = widget.HighImportance

	inner := container.New(&tightStatsVBoxLayout{Gap: 10},
		topRow,
		explainer,
		configBox,
		pathHint,
		localRow,
		toggleBtn,
	)
	content := NewInset(inner, 14, 14, 12, 12)

	cardBg := canvas.NewRectangle(design.ColorGray900)
	cardBg.CornerRadius = design.RadiusLG
	cardBg.StrokeColor = design.ColorTailscaleChipBorder
	cardBg.StrokeWidth = 1

	return container.NewStack(cardBg, content)
}

// configLineText renders one line of the copy-able config JSON, wrapping at
// the character level -- not just whitespace, since the fetch+eval line in
// MCPBridgeConfigJSON's output has none -- to whatever width it's actually
// given instead of overflowing configBox the way a bare canvas.Text does.
// canvas.Text has no Wrapping support at all and always reports its full
// unwrapped width as MinSize, which is exactly what pushed past the card's
// real (ratio-based, see DeviceDashboardColumnsLayout) width instead of
// wrapping into it. Modeled on deviceDashboardWrapText
// (device_dashboard_view.go), which hits the same tightStatsVBoxLayout/
// canvas.Text MinSize problem for card names -- the one real difference is
// this has no line cap/ellipsis: unlike a name label, truncating config
// text would just hide bytes the user still needs to read (OnCopyBridgeConfig
// copies data.BridgeConfigJSON directly, not this widget's text, so the
// clipboard is never affected either way -- but the whole point of showing
// the config at all is letting the user actually read it here first).
type configLineText struct {
	widget.BaseWidget
	text     string
	color    color.Color
	textSize float32
}

func newConfigLineText(text string, col color.Color, size float32) *configLineText {
	t := &configLineText{text: text, color: col, textSize: size}
	t.ExtendBaseWidget(t)
	return t
}

// MinSize reports a width of 1, not the wrapped text's actual rendered
// width -- deliberately, so this widget can never be the thing that forces
// the card wider (that's tightStatsVBoxLayout's w = max(child.MinSize().
// Width) rule, the same mechanism a bare canvas.Text abuses to overflow).
// Real width always comes top-down from the parent Resize call below.
func (t *configLineText) MinSize() fyne.Size {
	h := fyne.MeasureText("Ag", t.textSize, fyne.TextStyle{Monospace: true}).Height
	if h < 1 {
		h = t.textSize + 2
	}
	n := len(wrapConfigLine(t.text, t.textSize, t.Size().Width))
	if n < 1 {
		n = 1
	}
	return fyne.NewSize(1, float32(n)*h)
}

func (t *configLineText) Resize(size fyne.Size) {
	prev := t.Size()
	t.BaseWidget.Resize(size)
	if prev.Width != size.Width {
		t.Refresh()
	}
}

func (t *configLineText) CreateRenderer() fyne.WidgetRenderer {
	return &configLineTextRenderer{t: t}
}

type configLineTextRenderer struct {
	t     *configLineText
	lines []*canvas.Text
}

func (r *configLineTextRenderer) Layout(size fyne.Size) { r.apply(size.Width) }

func (r *configLineTextRenderer) apply(width float32) {
	parts := wrapConfigLine(r.t.text, r.t.textSize, width)
	h := fyne.MeasureText("Ag", r.t.textSize, fyne.TextStyle{Monospace: true}).Height
	for len(r.lines) < len(parts) {
		ln := canvas.NewText("", r.t.color)
		ln.TextStyle.Monospace = true
		r.lines = append(r.lines, ln)
	}
	y := float32(0)
	for i, ln := range r.lines {
		if i < len(parts) {
			ln.Text = parts[i]
			ln.Color = r.t.color
			ln.TextSize = r.t.textSize
			ln.Show()
			ln.Move(fyne.NewPos(0, y))
			ln.Resize(ln.MinSize())
			ln.Refresh()
			y += h
		} else {
			ln.Hide()
		}
	}
}

func (r *configLineTextRenderer) MinSize() fyne.Size           { return r.t.MinSize() }
func (r *configLineTextRenderer) Refresh()                     { r.apply(r.t.Size().Width); canvas.Refresh(r.t) }
func (r *configLineTextRenderer) BackgroundColor() color.Color { return color.Transparent }
func (r *configLineTextRenderer) Destroy()                     {}
func (r *configLineTextRenderer) Objects() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, len(r.lines))
	for i, ln := range r.lines {
		objs[i] = ln
	}
	return objs
}

// wrapConfigLine greedily fits as many runes as will fit in width onto each
// line, character by character rather than word by word -- a plain
// strings.Fields word-wrap (the pattern used elsewhere in this codebase,
// e.g. videoDialogWrapText) can't break a single space-free token like the
// fetch/eval JS string at all. No max line count: the caller needs every
// byte visible, not a truncated preview.
func wrapConfigLine(text string, textSize, width float32) []string {
	if width <= 8 {
		return []string{text}
	}
	if text == "" {
		return []string{""}
	}
	style := fyne.TextStyle{Monospace: true}
	fit := func(s string) bool {
		return fyne.MeasureText(s, textSize, style).Width <= width
	}
	runes := []rune(text)
	var lines []string
	start := 0
	for start < len(runes) {
		end := start + 1
		for end <= len(runes) && fit(string(runes[start:end])) {
			end++
		}
		end--
		if end <= start {
			end = start + 1
		}
		lines = append(lines, string(runes[start:end]))
		start = end
	}
	if len(lines) == 0 {
		lines = []string{text}
	}
	return lines
}
