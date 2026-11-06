package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"usbridge_agent/internal/input"
)

// keyboardLayout is GET/POST /api/keyboard/layout: read or switch the
// host's active keyboard layout (KDE Wayland only, see
// input/layout_linux.go). POST {"lang":"ru"} or {"lang":"en"}. Returns
// 501 where the host can't do it, so the client can fall back.
func (s *Server) keyboardLayout(w http.ResponseWriter, r *http.Request) {
	var (
		layout string
		err    error
	)
	if r.Method == http.MethodPost {
		var req struct {
			Lang string `json:"lang"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.fail(w, http.StatusBadRequest, "invalid_json", err)
			return
		}
		layout, err = input.SetKeyboardLayout(req.Lang)
	} else {
		layout, err = input.CurrentKeyboardLayout()
	}
	switch {
	case errors.Is(err, input.ErrLayoutUnsupported):
		s.fail(w, http.StatusNotImplemented, "keyboard_layout_unsupported", err)
	case errors.Is(err, input.ErrLayoutNotConfigured):
		s.fail(w, http.StatusNotFound, "keyboard_layout_not_configured", err)
	case err != nil:
		s.fail(w, http.StatusInternalServerError, "keyboard_layout_failed", err)
	default:
		s.ok(w, "keyboard_layout", map[string]string{"layout": layout})
	}
}

// keyboardTextMode is GET/POST /api/keyboard/text_mode: how the streamers'
// text reaches this host, typed on the USB broker's keyboard. POST
// {"mode":"keys"} types it as key presses (switching the layout a
// character needs), {"mode":"clipboard"} pastes it through the clipboard.
func (s *Server) keyboardTextMode(w http.ResponseWriter, r *http.Request) {
	if s.usb == nil {
		s.fail(w, http.StatusNotImplemented, "text_mode_unavailable", errors.New("no USB broker"))
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.fail(w, http.StatusBadRequest, "invalid_json", err)
			return
		}
		if err := s.usb.SetTextMode(req.Mode); err != nil {
			s.fail(w, http.StatusServiceUnavailable, "text_mode_failed", err)
			return
		}
	}
	s.ok(w, "text_mode", map[string]string{"mode": s.usb.TextMode()})
}
