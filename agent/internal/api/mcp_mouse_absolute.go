package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// move_to/click_at/double_click_at: the Agent's counterpart to the hardware
// KVM's own mouse.action actions of the same name (see
// usbridge_service/web/handlers.go's "move_to", "click_at", "double_click_at"
// case) -- added so that ui.parse's documented recipe ("compute the box
// center and pass it plus image_width/image_height to mouse.action's
// move_to/click_at") works identically regardless of which kind of device
// answered ui.parse: the hardware KVM's own NPU, or this client's local
// ui.parse offload (client/internal/api/local_ui_intercept.go) proxied
// through to a software Agent. Before this, the Agent only exposed
// dx/dy-relative move/click/scroll/action plus a broken "absolute" action
// (declared in the tool schema but never handled by applyMouse -- see that
// switch's new "absolute" case) -- an MCP client that had just run ui.parse
// against a software Agent target had detected boxes with nowhere reliable
// to click them.
//
// x/y/screen_width/screen_height use the exact same convention as the
// hardware KVM: x/y are pixel coordinates in the SAME capture ui.parse (or
// screen.get_image) returned, screen_width/screen_height are that capture's
// own dimensions (ui.parse's result.image_width/image_height). pixelToAbsoluteXY
// converts that into the 0..32767 normalized axis Input().AbsoluteEvent
// already expects (the same axis controller_common.go's scaleAbsoluteCoordinate
// maps back to actual screen pixels via the OS's own display bounds at
// injection time) -- so as long as the screenshot fed to ui.parse was taken
// at the host's actual display resolution (true for both screen.get_image
// and the live-frame path local_ui_intercept.go prefers), a box's pixel
// center lands within a pixel or two of the intended element regardless of
// what resolution ui.parse's caller happened to think in.
type mouseAbsoluteArgs struct {
	Action         string `json:"action"`
	X              *int   `json:"x,omitempty"`
	Y              *int   `json:"y,omitempty"`
	ScreenWidth    *int   `json:"screen_width,omitempty"`
	ScreenHeight   *int   `json:"screen_height,omitempty"`
	Button         *uint8 `json:"button,omitempty"`
	CaptureAfterMs *int   `json:"capture_after_ms,omitempty"`
	// Confirm=false skips click_at/double_click_at's before/after screenshot
	// diff (default true). The diff costs two full captures (~0.8s on a
	// typical desktop) -- pure overhead for a caller that verifies the
	// outcome itself (e.g. the Client's proxy, which diffs the frames it is
	// already decoding) or doesn't care.
	Confirm *bool `json:"confirm,omitempty"`
}

// pixelToAbsoluteXY mirrors usbridge_service/web/handlers.go's function of
// the same name exactly (same clamp, same scale formula) -- deliberately
// duplicated rather than shared (the agent and device repos don't share a
// Go module) so a caller gets byte-identical rounding whichever backend
// answers.
func pixelToAbsoluteXY(xPx, yPx, screenW, screenH int) (x uint16, y uint16) {
	const absMax = 32767
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	scale := func(v, dim int) uint16 {
		if dim <= 1 {
			return 0
		}
		return uint16(clamp(v*absMax/(dim-1), 0, absMax))
	}
	return scale(xPx, screenW), scale(yPx, screenH)
}

// mouseButtonMaskForMCP mirrors usbridge_service's function of the same
// name: MCP's 1/2/3 button numbering to AbsoluteEvent's bitmask.
func mouseButtonMaskForMCP(button uint8) (uint8, error) {
	switch button {
	case 1:
		return 0x01, nil
	case 2:
		return 0x02, nil
	case 3:
		return 0x04, nil
	default:
		return 0, fmt.Errorf("invalid button number: %d (use 1, 2 or 3)", button)
	}
}

// currentScreenPNG grabs a screenshot for diffing (never returned to the
// caller directly, see mcpMouseActionAbsolute) -- best-effort, an error
// here just means the eventual response omits screen_changed_pct rather
// than failing the click itself, same as the hardware KVM's own
// sampleScreenChangeSince.
func (s *Server) currentScreenPNG() ([]byte, error) {
	snap, err := s.app.Screen().Snapshot()
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(snap.ImageBase64)
}

// mcpMouseActionAbsolute handles move_to/click_at/double_click_at, called
// from mcpMouseAction once it sees one of those three action names.
func (s *Server) mcpMouseActionAbsolute(args mouseAbsoluteArgs) ([]map[string]any, error) {
	if args.X == nil || args.Y == nil || args.ScreenWidth == nil || args.ScreenHeight == nil {
		return nil, fmt.Errorf("x, y, screen_width and screen_height required")
	}
	if *args.ScreenWidth <= 0 || *args.ScreenHeight <= 0 {
		return nil, fmt.Errorf("screen_width and screen_height must be > 0")
	}
	absX, absY := pixelToAbsoluteXY(*args.X, *args.Y, *args.ScreenWidth, *args.ScreenHeight)

	if args.Action == "move_to" {
		if err := s.app.Input().AbsoluteEvent(0, absX, absY, 0); err != nil {
			return nil, err
		}
		if args.CaptureAfterMs != nil && *args.CaptureAfterMs > 0 {
			delay := time.Duration(*args.CaptureAfterMs) * time.Millisecond
			if delay > 5*time.Second {
				delay = 5 * time.Second // don't let a bad value hang the call
			}
			time.Sleep(delay)
			imgContent, err := s.mcpScreenGetImage()
			if err != nil {
				return []map[string]any{{"type": "text", "text": fmt.Sprintf(`{"status":"ok","capture_error":%q}`, err.Error())}}, nil
			}
			return append([]map[string]any{{"type": "text", "text": `{"status":"ok"}`}}, imgContent...), nil
		}
		return []map[string]any{{"type": "text", "text": "ok"}}, nil
	}

	button := uint8(1)
	if args.Button != nil {
		button = *args.Button
	}
	mask, err := mouseButtonMaskForMCP(button)
	if err != nil {
		return nil, err
	}
	clicks := 1
	if args.Action == "double_click_at" {
		clicks = 2
	}

	// Best-effort before capture so the response can tell the caller
	// whether the click visibly did anything -- a successful AbsoluteEvent
	// call only proves the OS accepted the injected event, not that
	// anything on screen reacted to it (wrong coordinates, an unfocused
	// window, a disabled control all "succeed" identically).
	wantDiff := args.Confirm == nil || *args.Confirm
	var beforePNG []byte
	beforeErr := fmt.Errorf("diff skipped")
	if wantDiff {
		beforePNG, beforeErr = s.currentScreenPNG()
	}

	for i := 0; i < clicks; i++ {
		if err := s.app.Input().AbsoluteEvent(mask, absX, absY, 0); err != nil {
			return nil, err
		}
		// Release with a second event at the same position -- AbsoluteEvent
		// has no built-in press/release pairing like MouseClick does.
		time.Sleep(50 * time.Millisecond)
		if err := s.app.Input().AbsoluteEvent(0, absX, absY, 0); err != nil {
			return nil, err
		}
		if clicks == 2 && i == 0 {
			// Gap between the two clicks of a double-click, short enough
			// for the host to register one double-click rather than two
			// independent clicks.
			time.Sleep(80 * time.Millisecond)
		}
	}

	result := map[string]any{"status": "ok"}
	if beforeErr == nil {
		if afterPNG, afterErr := s.currentScreenPNG(); afterErr == nil {
			if pct, diffErr := screenChangePercent(beforePNG, afterPNG); diffErr == nil {
				result["screen_changed_pct"] = pct
				result["screen_visibly_changed"] = pct > 0.05
			}
		}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return []map[string]any{{"type": "text", "text": string(payload)}}, nil
}
