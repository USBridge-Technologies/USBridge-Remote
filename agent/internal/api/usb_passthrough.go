package api

import (
	"encoding/json"
	"net/http"
	"usbridge_agent/internal/usbpass"
)

func (s *Server) SetUSBPassthrough(svc *usbpass.Service) {
	s.usb = svc
}

func (s *Server) usbPassthroughStatus(w http.ResponseWriter, r *http.Request) {
	if s.usb == nil {
		s.ok(w, "usb_passthrough", usbpass.Status{Available: false, Platform: "disabled"})
		return
	}
	s.ok(w, "usb_passthrough", s.usb.Status())
}

func (s *Server) usbPassthroughInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.fail(w, http.StatusMethodNotAllowed, "method_not_allowed", nil)
		return
	}
	if s.usb == nil {
		s.fail(w, http.StatusNotImplemented, "usb_passthrough_unavailable", nil)
		return
	}
	if err := s.usb.InstallDrivers(); err != nil {
		s.fail(w, http.StatusInternalServerError, "driver_install_failed", err)
		return
	}
	s.ok(w, "drivers_installed", s.usb.Status())
}

func (s *Server) usbPassthroughSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.fail(w, http.StatusMethodNotAllowed, "method_not_allowed", nil)
		return
	}
	if s.usb == nil {
		s.fail(w, http.StatusNotImplemented, "usb_passthrough_unavailable", nil)
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	st := s.usb.Status()
	s.ok(w, "usb_passthrough_session", map[string]any{
		"action":      body.Action,
		"listen_port": st.ListenPort,
		"broker":      st,
	})
}
