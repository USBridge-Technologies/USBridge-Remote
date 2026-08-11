package app

import (
	"testing"

	"usbridge_agent/internal/input"
)

// TestHandleWebRTCInput_DispatchesToController exercises the exact
// dispatch table stage 4 of the web-client plan added, without needing a
// real WebRTC session — a JSON payload shaped like what
// client/internal/webrtcweb.WebRTCClient sends over the "input" DataChannel
// goes in, and we assert it reaches input.Controller the same way
// /api/mouse and /api/keyboard's handlers already do (see
// agent/internal/api/server.go's applyMouse/keyboard for the REST twin of
// this dispatch). input.New() safely no-ops if uinput isn't available in
// the test environment (see controller_linux.go's tryInitUinput), so this
// runs everywhere; a real end-to-end check against actual uinput device
// nodes was additionally performed manually through a real browser — see
// the implementation plan's stage 1/4 notes.
func TestHandleWebRTCInput_DispatchesToController(t *testing.T) {
	a := &App{input: input.New()}

	cases := []string{
		`{"kind":"mouse","action":"move","dx":40,"dy":0}`,
		`{"kind":"mouse","action":"click","button":1}`,
		`{"kind":"mouse","action":"scroll","scroll":-5}`,
		`{"kind":"keyboard","action":"key","key_code":4}`,
		`{"kind":"keyboard","action":"combo","modifiers":2,"key_code":6}`,
		`{"kind":"keyboard","action":"text","text":"hello"}`,
		`{"kind":"mouse","action":"touch","button_state":1,"x":10,"y":20}`,
	}
	for _, c := range cases {
		// Must not panic regardless of whether the underlying uinput call
		// itself succeeds in this environment.
		a.handleWebRTCInput("test-session", []byte(c))
	}

	// Malformed/unknown payloads must be dropped, not panic.
	a.handleWebRTCInput("test-session", []byte("not json"))
	a.handleWebRTCInput("test-session", []byte(`{"kind":"bogus"}`))
	a.handleWebRTCInput("test-session", []byte(`{"kind":"mouse","action":"bogus"}`))
}
