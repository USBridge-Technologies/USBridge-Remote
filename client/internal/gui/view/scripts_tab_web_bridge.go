package view

// scripts_tab_web_bridge.go -- the wasm/browser build's MCP card
// (ScriptsMCPData.WebBridge == true, see that type's doc comment).
//
// Desktop's card (NewScriptsMCPCard's main branch, scripts_tab.go) is a
// bare endpoint URL + Start/Stop, because MCPProxy can run a real local
// HTTP listener there -- any MCP client's "url" server entry just points
// straight at it. A browser tab structurally cannot accept an inbound
// connection at all (no raw TCP/HTTP listen in a browser sandbox), so
// there is no URL to hand out here. Instead: download a small local relay
// script (client/web/mcp-bridge/bridge.mjs, see that file's own doc
// comment for the full topology) that Claude Desktop spawns over stdio --
// the one transport every MCP client already supports without needing
// "url"/SSE support -- and that itself opens a local WebSocket server this
// browser tab dials OUT to (the one direction a browser sandbox permits).
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
		"instead runs a small local script that this page connects out to. Download it once, " +
		"paste the config below into Claude Desktop, then connect.")
	explainer.Wrapping = fyne.TextWrapWord

	downloadBtn := widget.NewButton("Download bridge.cjs", data.OnDownloadBridge)

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

	pathHint := widget.NewLabel("Replace the first \"args\" entry with wherever you saved bridge.cjs.")
	pathHint.Wrapping = fyne.TextWrapWord

	configInner := container.New(&tightStatsVBoxLayout{Gap: 4}, configLabelRow, configBlock)
	configBg := canvas.NewRectangle(design.ColorGray950)
	configBg.CornerRadius = 6
	configBg.StrokeColor = design.ColorTailscaleChipBorder
	configBg.StrokeWidth = 1
	configBox := container.NewStack(configBg, NewInset(configInner, 12, 12, 8, 8))

	toggleLabel := "Connect"
	if data.BridgeConnected {
		toggleLabel = "Disconnect"
	}
	toggleBtn := widget.NewButton(toggleLabel, data.OnToggleBridge)
	toggleBtn.Importance = widget.HighImportance

	inner := container.New(&tightStatsVBoxLayout{Gap: 10},
		topRow,
		explainer,
		downloadBtn,
		configBox,
		pathHint,
		toggleBtn,
	)
	content := NewInset(inner, 14, 14, 12, 12)

	cardBg := canvas.NewRectangle(design.ColorGray900)
	cardBg.CornerRadius = design.RadiusLG
	cardBg.StrokeColor = design.ColorTailscaleChipBorder
	cardBg.StrokeWidth = 1

	return container.NewStack(cardBg, content)
}
