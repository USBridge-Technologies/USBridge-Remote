//go:build js && wasm

// Command wasm boots the real USBridge Fyne GUI (internal/gui) under
// GOOS=js GOARCH=wasm, rendering it on an HTML <canvas> in a browser via
// Fyne's js/wasm driver — the browser build of the client, alongside the
// existing linux/windows/darwin/android/ios ones.
//
// It also still exposes the stage-1/4 JS-callable test surface
// (window.usbridgeConnect / window.usbridgeSendInput, from
// internal/webrtcweb) used to verify the WebRTC signaling+DataChannel path
// against a real browser independently of the GUI — see the implementation
// plan's staged rollout. Real video-track wiring into internal/gui's
// VideoClient (stage 6) is the next step; until then these two pieces
// coexist rather than being merged into one path.
package main

import (
	"syscall/js"

	"usbridge-client/internal/gui"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/webrtcweb"
)

// activeClient is the last client Connect() succeeded on, so the test
// harness page can drive input sends from a separate button without
// plumbing the *webrtcweb.WebRTCClient through JS values.
var activeClient *webrtcweb.WebRTCClient

func main() {
	js.Global().Set("usbridgeConnect", js.FuncOf(connect))
	js.Global().Set("usbridgeSendInput", js.FuncOf(sendInput))

	i18n.Init("en")
	config := models.DefaultConfig()
	gui.SetAppVersion("web")
	view.SetAppVersion("web")
	mainWindow := gui.NewMainWindow(config)
	gui.InitIMEBridge()
	mainWindow.Show()
}

// sendInput(jsonPayload) pushes a raw JSON input message (matching
// agent/internal/app/webrtc_input.go's webRTCInputMessage shape) over the
// already-open "input" DataChannel — used by the stage-4 test harness to
// prove real mouse/keyboard events reach the agent's input.Controller.
func sendInput(this js.Value, args []js.Value) interface{} {
	if activeClient == nil {
		return "no active connection"
	}
	if err := activeClient.Send([]byte(args[0].String())); err != nil {
		return err.Error()
	}
	return nil
}

// connect(baseURL, masterKey, sessionID, onLog) is called from JS. onLog is
// invoked with human-readable status/RTT strings for the test harness to
// display, so the whole flow is observable both from the page and from
// devtools console without needing a Go-side test runner.
func connect(this js.Value, args []js.Value) interface{} {
	baseURL := args[0].String()
	masterKey := args[1].String()
	sessionID := args[2].String()
	onLog := args[3]

	log := func(msg string) {
		onLog.Invoke(msg)
	}

	client := webrtcweb.NewWebRTCClient(baseURL, masterKey)
	activeClient = client
	client.OnStateChange(func(state string) {
		log("connection state: " + state)
	})
	client.OnMessage(func(data []byte) {
		log("reply: " + string(data))
	})

	go func() {
		log("connecting to " + baseURL + " ...")
		if err := client.Connect(sessionID); err != nil {
			log("connect error: " + err.Error())
			return
		}
		log("offer/answer exchange complete, waiting for datachannel...")
	}()

	return nil
}
