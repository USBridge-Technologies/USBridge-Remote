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
		t := canvas.NewText(line, scriptsMCPURLColor)
		t.TextSize = 9
		t.TextStyle.Monospace = true
		configRows = append(configRows, t)
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
