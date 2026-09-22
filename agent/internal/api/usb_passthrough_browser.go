package api

import (
	"crypto/hmac"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"usbridge-client/pkg/usbpasscore"
	"usbridge_agent/internal/browserusb"

	"github.com/gorilla/websocket"
)

// Browser-sourced USB/IP passthrough: a browser tab has no OS-level USB
// stack and can never accept an inbound TCP connection (no listen()/accept()
// in any web platform API), so unlike the native client's usbaes_attach.go
// -- which owns a real local device and exports it over the network for the
// agent's usbip-win2/vhci-hcd to dial into -- a browser-sourced device
// (Gamepad API state today; WebHID-sourced devices later) is exported from
// *inside this agent process itself* (see agent/internal/browserusb), behind
// an AEAD tunnel listener the same way a native Attach() protects its own
// exporter -- a production agent normally has a --tsnet-bridge configured,
// which makes rust-shine's broker route any loopback-looking ExportHost
// through its remote-peer relay instead of skipping encryption, so this
// path cannot get away with an unencrypted loopback export the way an
// agent with no bridge at all could (see browserusb's own doc comment for
// how that was confirmed live).
//
// Two things the browser still needs that only this agent process can give
// it, since a browser tab can't net.Dial the broker's AES control-plane
// port directly the way the native client's usbaes_attach.go does:
//
//  1. A loopback port to put in its own Attach payload's ExportService --
//     usbPassthroughBrowserSession allocates that (via browserusb) and hands
//     it back over an ordinary authenticated HTTP POST, which (unlike a
//     WebSocket) the browser CAN sign with the usual X-Auth-* headers.
//  2. A way to actually reach the broker's AES port at all --
//     usbPassthroughBrowserAttach is a byte-for-byte relay: it dials
//     127.0.0.1:<broker's urb port> itself (agent Go code can net.Dial
//     loopback fine) and pumps the WebSocket <-> that TCP connection with no
//     protocol awareness. The browser's wasm build then speaks the exact
//     same Hello/Attach AES-GCM framing usbaes_attach.go already implements
//     natively, just over this relayed transport instead of a raw net.Dial.

// browserSessions tracks active browser-sourced gamepad exports by bus id.
// Package-level (not a Server field) to match usbaes_attach.go's own
// package-level single-flight pattern on the client side -- there is one
// agent process per machine, same as one client process per machine.
var (
	browserSessionsMu sync.Mutex
	browserSessions   = map[string]*browserusb.GamepadSession{}
)

// wsAuthWindow matches security.go's verifyHMAC timestamp tolerance.
const wsAuthWindow = 60 * time.Second

// verifyWSAuth checks a query-string HMAC for endpoints a browser reaches
// via the WebSocket constructor, which -- unlike fetch()/XHR -- cannot set
// custom request headers at all, so the X-Auth-Signature/X-Auth-Timestamp
// scheme verifyHMAC expects is not reachable from a browser WebSocket
// client. This carries the same signed material (method, path, timestamp,
// empty body) through ?ts=&sig= query params instead, verified against the
// identical CalculateHMAC used everywhere else in this package.
func (s *Server) verifyWSAuth(r *http.Request) bool {
	q := r.URL.Query()
	tsStr := q.Get("ts")
	sig := q.Get("sig")
	masterKey := s.sec.currentMasterKey()
	if tsStr == "" || sig == "" || len(masterKey) == 0 {
		return false
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return false
	}
	now := time.Now().Unix()
	if now-ts > int64(wsAuthWindow.Seconds()) || ts-now > int64(wsAuthWindow.Seconds()) {
		return false
	}
	expected := CalculateHMAC(http.MethodGet, r.URL.Path, tsStr, "", masterKey)
	return hmac.Equal([]byte(sig), []byte(expected))
}

// usbPassthroughBrowserSession allocates a loopback USB/IP export for a
// browser-sourced synthetic Xbox 360 controller and returns the bus id +
// port the browser's own Attach payload should use as
// (BusID, ExportHost="127.0.0.1", ExportService=port).
func (s *Server) usbPassthroughBrowserSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.fail(w, http.StatusMethodNotAllowed, "method_not_allowed", nil)
		return
	}
	if s.usb == nil {
		s.fail(w, http.StatusNotImplemented, "usb_passthrough_unavailable", nil)
		return
	}

	masterKey := s.sec.currentMasterKey()
	if len(masterKey) == 0 {
		s.fail(w, http.StatusInternalServerError, "browser_session_failed", fmt.Errorf("no master key"))
		return
	}

	busID := fmt.Sprintf("browser-%d", time.Now().UnixNano())
	session, err := browserusb.NewGamepadSession(busID, busID, masterKey)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "browser_session_failed", err)
		return
	}

	browserSessionsMu.Lock()
	browserSessions[busID] = session
	browserSessionsMu.Unlock()

	s.ok(w, "browser_usb_session", map[string]any{
		"bus_id":         busID,
		"export_host":    session.ExportHost(),
		"export_service": session.ExportPort(),
		"tunnel_nonce":   hex.EncodeToString(session.Nonce()),
		"broker_addr":    fmt.Sprintf("127.0.0.1:%d", s.usb.ListenPort()),
	})
}

// usbPassthroughBrowserAttach relays raw bytes between a browser WebSocket
// and the local usbridge-usb-broker's AES control-plane port -- see this
// file's doc comment. It never parses the USB passthrough protocol itself.
func (s *Server) usbPassthroughBrowserAttach(w http.ResponseWriter, r *http.Request) {
	if !s.verifyWSAuth(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if s.usb == nil {
		http.Error(w, "usb passthrough unavailable", http.StatusNotImplemented)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[api] usb_browser_attach upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	broker, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", s.usb.ListenPort()))
	if err != nil {
		log.Printf("[api] usb_browser_attach: dial broker: %v", err)
		return
	}
	defer broker.Close()

	done := make(chan struct{}, 2)

	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 64*1024)
		for {
			n, err := broker.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if _, werr := broker.Write(data); werr != nil {
				return
			}
		}
	}()
	<-done
}

// browserGamepadFrameLen is the wire size of one X360 state sample sent by
// the wasm client's Gamepad API poller: buttons:u16, leftTrigger:u8,
// rightTrigger:u8, leftX/leftY/rightX/rightY:i16 -- all little-endian. This
// is a wire format of this endpoint's own choosing (not USB/IP, not
// X360Backend.report()'s device-facing layout), kept deliberately simple
// since the only consumer is gamepad_capture_wasm.go.
const browserGamepadFrameLen = 12

// usbPassthroughBrowserGamepad receives a stream of gamepad-state frames for
// one bus id (see browserGamepadFrameLen) and applies each to that bus id's
// GamepadSession, which republishes it as the synthetic controller's next
// USB interrupt-IN report.
func (s *Server) usbPassthroughBrowserGamepad(w http.ResponseWriter, r *http.Request) {
	if !s.verifyWSAuth(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	busID := r.URL.Query().Get("bus_id")
	browserSessionsMu.Lock()
	session := browserSessions[busID]
	browserSessionsMu.Unlock()
	if session == nil {
		http.Error(w, "unknown bus_id", http.StatusNotFound)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[api] usb_browser_gamepad upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	defer func() {
		browserSessionsMu.Lock()
		if browserSessions[busID] == session {
			delete(browserSessions, busID)
		}
		browserSessionsMu.Unlock()
		session.Close()
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if len(data) != browserGamepadFrameLen {
			continue
		}
		session.SetState(decodeBrowserGamepadFrame(data))
	}
}

func decodeBrowserGamepadFrame(b []byte) usbpasscore.X360State {
	return usbpasscore.X360State{
		Buttons: binary.LittleEndian.Uint16(b[0:2]),
		LT:      b[2],
		RT:      b[3],
		LX:      int16(binary.LittleEndian.Uint16(b[4:6])),
		LY:      int16(binary.LittleEndian.Uint16(b[6:8])),
		RX:      int16(binary.LittleEndian.Uint16(b[8:10])),
		RY:      int16(binary.LittleEndian.Uint16(b[10:12])),
	}
}
