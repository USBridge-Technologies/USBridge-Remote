package usbpass

import (
	"fmt"
	"log"
	"time"
)

// Text modes of the broker's keyboard (rust-shine bin/usb-broker/src/keyboard.rs):
// how the streamers' text (Moonlight UTF-8 packets) reaches this host.
const (
	// TextModeKeys types text as key presses, switching the host layout a
	// character needs (the default).
	TextModeKeys = "keys"
	// TextModeClipboard puts text on the host clipboard and pastes it:
	// independent of any layout, and it carries emoji and other scripts.
	TextModeClipboard = "clipboard"
)

// SetTextMode tells the broker how to bring text in and remembers it, so a
// restarted broker (it forgets on exit) gets it again from Start.
func (s *Service) SetTextMode(mode string) error {
	if mode != TextModeKeys && mode != TextModeClipboard {
		return fmt.Errorf("unknown text mode %q", mode)
	}
	s.mu.Lock()
	s.textMode = mode
	s.mu.Unlock()
	out, err := s.control("text_mode", map[string]any{"mode": mode})
	if err != nil {
		return err
	}
	if ok, _ := out["ok"].(bool); !ok {
		return fmt.Errorf("broker refused text mode %q: %v", mode, out["error"])
	}
	return nil
}

// TextMode is the mode last set (TextModeKeys until a client picks one).
func (s *Service) TextMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.textMode == "" {
		return TextModeKeys
	}
	return s.textMode
}

// reapplyTextMode waits for a freshly started broker to answer and gives it
// the remembered text mode. Keys is the broker's own default.
func (s *Service) reapplyTextMode() {
	mode := s.TextMode()
	if mode == TextModeKeys {
		return
	}
	for i := 0; i < 50; i++ {
		if _, err := s.control("ping", nil); err == nil {
			if err := s.SetTextMode(mode); err != nil {
				log.Printf("[usbpass] text mode %s after restart: %v", mode, err)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
