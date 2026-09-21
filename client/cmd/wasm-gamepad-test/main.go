//go:build js && wasm

// Command wasm-gamepad-test is a minimal standalone wasm binary for testing
// the browser-sourced USB/IP gamepad path (usbpass.AttachBrowserGamepad +
// platform.StartGamepadCapture) end to end against a real agent, without
// needing the full client/cmd/wasm build -- which currently fails to
// compile for GOOS=js for unrelated, pre-existing reasons (internal/gui ->
// internal/service pulls in cgo-only .c sources with no cgo available under
// wasm). This binary only imports usbpass/platform, so it's unaffected.
//
// Exposes exactly two JS globals:
//
//	usbridgeStartBrowserGamepad(agentBaseURL, secret, onLog)
//	usbridgeStopBrowserGamepad()
//
// agentBaseURL/secret are the same values the real client already uses to
// reach the agent's HTTP API (e.g. whatever masterKey usbridgeConnect takes
// in cmd/wasm/main.go) -- this introduces no new pairing/secret of its own.
package main

import (
	"syscall/js"

	"usbridge-client/internal/platform"
	"usbridge-client/internal/usbpass"
)

var activeStop func()

func main() {
	js.Global().Set("usbridgeStartBrowserGamepad", js.FuncOf(startBrowserGamepad))
	js.Global().Set("usbridgeStopBrowserGamepad", js.FuncOf(stopBrowserGamepad))
	select {} // keep the wasm instance alive; everything happens from JS callbacks
}

func startBrowserGamepad(this js.Value, args []js.Value) interface{} {
	agentBaseURL := args[0].String()
	secret := args[1].String()
	onLog := args[2]
	log := func(msg string) { onLog.Invoke(msg) }

	if activeStop != nil {
		log("stopping previous session")
		activeStop()
		activeStop = nil
	}

	log("requesting browser-session from agent...")
	send, stop, err := usbpass.AttachBrowserGamepad(usbpass.BrowserGamepadAttachOptions{
		AgentBaseURL: agentBaseURL,
		Secret:       []byte(secret),
	})
	if err != nil {
		log("attach error: " + err.Error())
		return nil
	}
	log("attached -- press a button on the gamepad, then check the agent for a new Xbox 360 controller")

	capture := platform.StartGamepadCapture(send)
	activeStop = func() {
		capture.Stop()
		stop()
	}
	return nil
}

func stopBrowserGamepad(this js.Value, args []js.Value) interface{} {
	if activeStop != nil {
		activeStop()
		activeStop = nil
	}
	return nil
}
