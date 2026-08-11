package app

import (
	"encoding/json"
	"log"
)

// webRTCInputMessage is the wire format for the browser web client's
// "input" DataChannel (stage 4 of the web-client rollout plan) — a
// deliberately close mirror of api.KeyboardRequest/api.MouseRequest's JSON
// shape (see agent/internal/api/types.go), just with a "kind" discriminator
// added so both travel over the one DataChannel instead of two separate
// REST endpoints. Reusing the same field names/semantics means the
// browser-side encoder and the desktop client's existing /api/keyboard,
// /api/mouse callers agree on exactly what each field means without a
// second protocol to learn.
type webRTCInputMessage struct {
	Kind string `json:"kind"` // "keyboard" | "mouse"

	// keyboard fields
	Action    string  `json:"action"`
	KeyCode   *uint8  `json:"key_code,omitempty"`
	Modifiers *uint8  `json:"modifiers,omitempty"`
	Text      *string `json:"text,omitempty"`

	// mouse fields
	DX          *int8  `json:"dx,omitempty"`
	DY          *int8  `json:"dy,omitempty"`
	Button      *uint8 `json:"button,omitempty"`
	Scroll      *int8  `json:"scroll,omitempty"`
	X           *int   `json:"x,omitempty"`
	Y           *int   `json:"y,omitempty"`
	ButtonState *uint8 `json:"button_state,omitempty"`
}

func u8(p *uint8) uint8 {
	if p == nil {
		return 0
	}
	return *p
}

func i8(p *int8) int8 {
	if p == nil {
		return 0
	}
	return *p
}

func i(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// handleWebRTCInput is wired as webrtcbridge.Bridge.OnInputMessage (see
// New() in app.go) — every message the browser sends on the "input"
// DataChannel lands here and gets dispatched to the exact same
// input.Controller the desktop client's /api/keyboard and /api/mouse REST
// handlers already drive, so there is no separate input-injection path to
// keep correct for the web client.
func (a *App) handleWebRTCInput(sessionID string, data []byte) {
	var msg webRTCInputMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[webrtc-input] session=%s invalid message: %v", sessionID, err)
		return
	}

	var err error
	switch msg.Kind {
	case "keyboard":
		switch msg.Action {
		case "key":
			err = a.input.Key(u8(msg.KeyCode))
		case "combo":
			err = a.input.Combo(u8(msg.Modifiers), u8(msg.KeyCode))
		case "text":
			if msg.Text != nil {
				err = a.input.Text(*msg.Text)
			}
		default:
			log.Printf("[webrtc-input] session=%s unsupported keyboard action %q", sessionID, msg.Action)
			return
		}
	case "mouse":
		switch msg.Action {
		case "move":
			err = a.input.MouseMove(i8(msg.DX), i8(msg.DY))
		case "click":
			err = a.input.MouseClick(u8(msg.Button))
		case "scroll":
			err = a.input.MouseScroll(i8(msg.Scroll))
		case "action":
			err = a.input.MouseAction(u8(msg.Button), i8(msg.DX), i8(msg.DY), i8(msg.Scroll))
		case "touch", "touch_position", "absolute_event":
			err = a.input.AbsoluteEvent(u8(msg.ButtonState), uint16(i(msg.X)), uint16(i(msg.Y)), i8(msg.Scroll))
		default:
			log.Printf("[webrtc-input] session=%s unsupported mouse action %q", sessionID, msg.Action)
			return
		}
	default:
		log.Printf("[webrtc-input] session=%s unknown kind %q", sessionID, msg.Kind)
		return
	}

	if err != nil {
		log.Printf("[webrtc-input] session=%s %s/%s failed: %v", sessionID, msg.Kind, msg.Action, err)
	}
}
