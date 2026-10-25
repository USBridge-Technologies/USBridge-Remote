//go:build js && wasm

package main

import (
	"context"
	"strings"
	"syscall/js"
	"time"

	"usbridge-client/internal/guestclient"
	"usbridge-client/internal/webrtcweb"
)

// Guest entry points for web/join.html -- the page a person lands on from an
// invitation e-mail. It signs them in (the same account login the apps use),
// then usbridgeJoin connects them to the shared computer with their role
// instead of a master key. No dashboard: a guest gets the screen and, if the
// role allows it, the keyboard and mouse.

var guestClient *webrtcweb.WebRTCClient

// usbridgeJoin(shareID, accountToken, directBaseURL, callbacks) -> Promise
// callbacks: {onInfo({deviceName, role, requireConsent}), onState(string), onLog(string)}
// directBaseURL may be "" (reach the computer through the cloud relay only).
// The promise resolves when the video is up and rejects with a message.
func usbridgeJoin(this js.Value, args []js.Value) any {
	shareID := args[0].String()
	token := args[1].String()
	baseURL := strings.TrimRight(args[2].String(), "/")
	cb := args[3]
	call := func(name string, v any) {
		if f := cb.Get(name); f.Type() == js.TypeFunction {
			f.Invoke(v)
		}
	}

	handler := js.FuncOf(func(this js.Value, pargs []js.Value) any {
		resolve, reject := pargs[0], pargs[1]
		go func() {
			fail := func(msg string) { reject.Invoke(js.Global().Get("Error").New(msg)) }
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			sess, err := guestclient.Join(ctx, "", func() string { return token }, shareID)
			if err != nil {
				fail(err.Error())
				return
			}
			call("onInfo", map[string]any{"deviceName": sess.DeviceName, "role": sess.Role, "requireConsent": sess.RequireConsent})

			client := webrtcweb.NewWebRTCClient(baseURL, "", sess.HwID)
			client.SetGuest(sess)
			client.OnStateChange(func(state string) { call("onState", state) })
			guestClient = client

			// An offer is also the guest's "may I come in": while the person at
			// the computer hasn't answered, the agent says consent_pending.
			deadline := time.Now().Add(100 * time.Second)
			for {
				err = client.Connect("")
				if err == nil {
					break
				}
				msg := err.Error()
				if strings.Contains(msg, "consent_pending") && time.Now().Before(deadline) {
					call("onState", "waiting-for-host")
					time.Sleep(3 * time.Second)
					continue
				}
				if strings.Contains(msg, "consent_denied") {
					fail("The person at the computer declined the connection.")
					return
				}
				if strings.Contains(msg, "share_ended") {
					fail(guestclient.ErrShareEnded.Error())
					return
				}
				fail(msg)
				return
			}
			resolve.Invoke(client.VideoElement())
		}()
		return nil
	})
	return js.Global().Get("Promise").New(handler)
}

func guestSend(pkt []byte) any {
	if guestClient == nil {
		return false
	}
	return guestClient.SendBinary(pkt) == nil
}

func usbridgeGuestMouseMove(this js.Value, a []js.Value) any {
	return guestSend(webrtcweb.EncodeMouseMoveAbsolute(int16(a[0].Int()), int16(a[1].Int()), int16(a[2].Int()), int16(a[3].Int())))
}
func usbridgeGuestMouseButton(this js.Value, a []js.Value) any {
	return guestSend(webrtcweb.EncodeMouseButton(uint8(a[0].Int()), a[1].Bool()))
}
func usbridgeGuestScroll(this js.Value, a []js.Value) any {
	return guestSend(webrtcweb.EncodeScroll(int16(a[0].Int())))
}
func usbridgeGuestKey(this js.Value, a []js.Value) any {
	return guestSend(webrtcweb.EncodeKey(uint8(a[0].Int()), uint8(a[1].Int()), a[2].Bool()))
}
func usbridgeGuestText(this js.Value, a []js.Value) any {
	return guestSend(webrtcweb.EncodeUtf8Text(a[0].String()))
}
func usbridgeGuestLeave(this js.Value, a []js.Value) any {
	if guestClient != nil {
		guestClient.Close()
		guestClient = nil
	}
	return nil
}

func registerGuestEntryPoints() {
	g := js.Global()
	g.Set("usbridgeJoin", js.FuncOf(usbridgeJoin))
	g.Set("usbridgeGuestMouseMove", js.FuncOf(usbridgeGuestMouseMove))
	g.Set("usbridgeGuestMouseButton", js.FuncOf(usbridgeGuestMouseButton))
	g.Set("usbridgeGuestScroll", js.FuncOf(usbridgeGuestScroll))
	g.Set("usbridgeGuestKey", js.FuncOf(usbridgeGuestKey))
	g.Set("usbridgeGuestText", js.FuncOf(usbridgeGuestText))
	g.Set("usbridgeGuestLeave", js.FuncOf(usbridgeGuestLeave))
}
