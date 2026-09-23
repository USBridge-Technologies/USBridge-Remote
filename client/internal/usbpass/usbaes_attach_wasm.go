//go:build js && wasm

package usbpass

// Browser-sourced USB/IP attach: the wasm counterpart of usbaes_attach.go's
// Attach(), for a synthetic gamepad/pen tablet whose input comes from the
// browser instead of a real local device. Two things differ from the native
// flow:
//
//  1. There's no local device to describe or export -- the agent hosts the
//     loopback USB/IP Server itself (agent/internal/browserusb) and hands
//     back a bus id + loopback port via an ordinary authenticated HTTP POST
//     (postBrowserSession) instead of this code picking one.
//  2. The AES Hello/Attach handshake below is byte-for-byte the same
//     protocol usbaes_attach.go speaks (same attachPayload/encodeAttachFrame/
//     aeadStream, all pure Go -- see usbaes_protocol.go/usbaes_transport.go's
//     widened build tags), carried over a labeled WebRTC DataChannel on the
//     browser's already-established video/control RTCPeerConnection
//     (opts.OpenDataChannel, wired from
//     client/internal/webrtcweb.WebRTCClient.OpenDataChannel via
//     client/internal/gui/controller/disk_widget.go's SetPeerConnection)
//     instead of a separate WebSocket to the agent's own HTTP server. This
//     traffic is DTLS-encrypted end to end and never subject to the
//     browser's mixed-content blocking the way a plain ws:// connection
//     from an https page would be -- see
//     agent/internal/api/usb_passthrough_browser.go's bridge listener for
//     the other end of this channel (relayed into the Go agent by
//     rustshine, the process that actually terminates the PeerConnection).
//
// Unlike native Attach(), the TunnelNonce here is *not* generated locally:
// the agent already generated one (and armed its own TunnelListener with the
// key derived from it) by the time postBrowserSession returns, since the
// agent -- not this browser tab -- is the one hosting the tunnel listener
// that protects the loopback exporter. This code just has to echo the same
// nonce back in its Attach frame so the broker derives the identical key
// agent already did.

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// calculateHMACV2 duplicates client/internal/api.CalculateHMACV2's exact
// algorithm (HMAC-SHA256 of METHOD+PATH+TIMESTAMP+BODY, keyed by SHA-256 of
// the shared secret) rather than importing that package: internal/api pulls
// in internal/localui (ONNX-backed local-UI OCR), which has no wasm build
// at all -- importing the package for one small pure function would drag a
// build-breaking dependency into the wasm binary. This same primitive is
// already independently reimplemented on the agent side too
// (agent/internal/api/security.go's CalculateHMAC) -- a third small,
// self-contained copy here follows that existing pattern rather than
// fighting it. Only postBrowserSession's header signing needs this now --
// the attach/gamepad/pen data paths ride an authenticated DataChannel
// instead of a signed WebSocket URL (see attachBrowserDevice).
func calculateHMACV2(method, path, timestamp, body string, key []byte) string {
	h := sha256.Sum256(key)
	mac := hmac.New(sha256.New, h[:])
	mac.Write([]byte(method + path + timestamp + body))
	return hex.EncodeToString(mac.Sum(nil))
}

// BrowserGamepadAttachOptions is the wasm-side counterpart of AttachOptions.
type BrowserGamepadAttachOptions struct {
	// AgentBaseURL is the same base URL the rest of the web client's HTTP
	// API calls already use, e.g. "https://192.168.1.20:47990" -- only
	// needed here for the one-shot session-setup POST (postBrowserSession);
	// the attach/data channels themselves go over OpenDataChannel below.
	AgentBaseURL string
	Secret       []byte

	// OpenDataChannel creates a new labeled DataChannel on the client's
	// already-connected video/control WebRTC PeerConnection, blocking until
	// it's open. Wired from client/internal/webrtcweb.WebRTCClient via
	// DiskWidget.SetPeerConnection -- see disk_widget_gamepad_start_wasm.go
	// / disk_widget_pen_start_wasm.go. nil (no peer connection yet, e.g.
	// video/control never connected) is a hard precondition failure for
	// browser USB passthrough now that it rides the same PeerConnection --
	// attachBrowserDevice fails immediately with a clear error instead of
	// falling back to anything else.
	OpenDataChannel func(label string) (net.Conn, error)
}

type browserSessionData struct {
	BusID         string `json:"bus_id"`
	ExportHost    string `json:"export_host"`
	ExportService string `json:"export_service"`
	TunnelNonce   string `json:"tunnel_nonce"` // hex-encoded; see AttachBrowserGamepad
}

type browserSessionEnvelope struct {
	Success bool               `json:"success"`
	Error   string             `json:"error"`
	Data    browserSessionData `json:"data"`
}

func postBrowserSession(opts BrowserGamepadAttachOptions, path string, body []byte) (browserSessionData, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := calculateHMACV2(http.MethodPost, path, ts, string(body), opts.Secret)

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(http.MethodPost, opts.AgentBaseURL+path, bodyReader)
	if err != nil {
		return browserSessionData{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Auth-Signature", sig)
	req.Header.Set("X-Auth-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return browserSessionData{}, fmt.Errorf("browser session request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return browserSessionData{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return browserSessionData{}, fmt.Errorf("browser session: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	var env browserSessionEnvelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return browserSessionData{}, fmt.Errorf("browser session: decode response: %w", err)
	}
	if !env.Success {
		return browserSessionData{}, fmt.Errorf("browser session: %s", env.Error)
	}
	return env.Data, nil
}

// AttachBrowserGamepad asks the agent for a loopback USB/IP export of a
// synthetic Xbox 360 controller, performs the AES Hello/Attach handshake
// against it, and returns send (push one browserGamepadFrameLen-shaped
// state frame) and stop (detach and close both DataChannels).
func AttachBrowserGamepad(opts BrowserGamepadAttachOptions) (send func([]byte), stop func(), err error) {
	dataConn, stop, err := attachBrowserDevice(opts,
		"/api/usb/passthrough/browser-session", nil,
		x360VID, x360PID, x360DeviceDesc(), x360ConfigDesc(),
		"usbpass-gamepad")
	if err != nil {
		return nil, nil, err
	}
	send = func(frame []byte) { _, _ = dataConn.Write(frame) }
	return send, stop, nil
}

// browserPenSessionRequest mirrors agent/internal/api's
// browserPenSessionRequest exactly (JSON tags included) -- the two are not
// shared code since they live in different modules (see
// pkg/usbpasscore/export.go's own doc comment for why that split exists at
// all), but the wire shape must match byte for byte.
type browserPenSessionRequest struct {
	VendorID    uint16 `json:"vendor_id"`
	ProductID   uint16 `json:"product_id"`
	ProductName string `json:"product_name"`
}

// AttachBrowserPen asks the agent for a loopback USB/IP export of the Wacom
// tablet identified by vid/pid/productName (as WebHID's device.vendorId/
// productId/productName give them -- see pen_capture_wasm.go), performs the
// AES Hello/Attach handshake against it, and returns send (push one raw
// HID input report, verbatim from device.oninputreport) and stop (detach
// and close both DataChannels). The device/config descriptor bytes in the
// Attach frame come from the exact same model resolution
// usbpasscore.NewWacomExportedDevice does agent-side (wacom_model.go has no
// build tag, so this package already has it) -- building a throwaway
// backend here just for its ExportedDevice fields is wasted work, but
// keeps this file from needing a separate "just the descriptors" entry
// point into wacom_model.go.
func AttachBrowserPen(vid, pid uint16, productName string, opts BrowserGamepadAttachOptions) (send func([]byte), stop func(), err error) {
	dev, _, err := NewWacomExportedDevice("browser-pen-probe", vid, pid, productName)
	if err != nil {
		return nil, nil, err
	}
	body, err := json.Marshal(browserPenSessionRequest{VendorID: vid, ProductID: pid, ProductName: productName})
	if err != nil {
		return nil, nil, err
	}
	dataConn, stop, err := attachBrowserDevice(opts,
		"/api/usb/passthrough/browser-pen-session", body,
		dev.VID, dev.PID, dev.DeviceDesc, dev.ConfigDesc,
		"usbpass-pen")
	if err != nil {
		return nil, nil, err
	}
	send = func(report []byte) { _, _ = dataConn.Write(report) }
	return send, stop, nil
}

// attachBrowserDevice is the device-agnostic half of AttachBrowserGamepad/
// AttachBrowserPen: request a loopback export (sessionPath/sessionBody),
// speak the AES Hello/Attach handshake describing it as vid:pid with the
// given descriptors over a DataChannel labeled "usbpass-attach-<bus_id>",
// hold the attach session open in the background, then open a second
// DataChannel labeled "<dataChannelPrefix>-<bus_id>" the caller streams its
// own device-specific frames over. dataConn is that second connection; stop
// tears down both.
func attachBrowserDevice(
	opts BrowserGamepadAttachOptions,
	sessionPath string, sessionBody []byte,
	vid, pid uint16, deviceDesc, configDesc []byte,
	dataChannelPrefix string,
) (dataConn net.Conn, stop func(), err error) {
	if opts.OpenDataChannel == nil {
		return nil, nil, fmt.Errorf("browser attach: no WebRTC peer connection available (connect video/control first)")
	}

	session, err := postBrowserSession(opts, sessionPath, sessionBody)
	if err != nil {
		return nil, nil, err
	}

	conn, err := opts.OpenDataChannel("usbpass-attach-" + session.BusID)
	if err != nil {
		return nil, nil, fmt.Errorf("browser attach channel: %w", err)
	}

	key := deriveSessionKey(opts.Secret)
	stream, err := newAeadStream(conn, key)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("aes session: %w", err)
	}

	if err := stream.sendFrame(encodeHelloFrame(usbAesRoleClient, usbAesProtoVersion)); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("hello: %w", err)
	}
	ackRaw, err := stream.recvFrame()
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("hello-ack: %w", err)
	}
	msgType, payload, err := decodeUsbpFrame(ackRaw)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("hello-ack: %w", err)
	}
	if msgType != msgHelloAck {
		conn.Close()
		return nil, nil, fmt.Errorf("expected hello-ack, got msg type %d", msgType)
	}
	ok, detail, err := decodeHelloAck(payload)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("hello-ack: %w", err)
	}
	if !ok {
		conn.Close()
		return nil, nil, fmt.Errorf("agent refused hello: %s", detail)
	}

	tunnelNonce, err := hex.DecodeString(session.TunnelNonce)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("tunnel nonce: %w", err)
	}

	attachFrame := attachPayload{
		BusID:         session.BusID,
		VID:           vid,
		PID:           pid,
		Speed:         3, // matches usbaes_attach.go's own placeholder value
		DeviceDesc:    deviceDesc,
		ConfigDesc:    configDesc,
		ExportHost:    session.ExportHost,
		ExportService: session.ExportService,
		TunnelNonce:   tunnelNonce,
	}
	if err := stream.sendFrame(encodeAttachFrame(attachFrame)); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("attach: %w", err)
	}
	logrus.Infof("usbpass(wasm): browser attach sent bus=%s vid=%04x pid=%04x -> %s:%s",
		session.BusID, vid, pid, session.ExportHost, session.ExportService)

	// Mirrors usbaes_attach.go's holdAttachSession: loop until the agent
	// sends Detach/Reset or the connection drops. stop() below closes conn,
	// which unblocks a pending recvFrame() with an error the same way
	// closing a real net.Conn would.
	go func() {
		for {
			raw, err := stream.recvFrame()
			if err != nil {
				logrus.Infof("usbpass(wasm): browser attach session ended: %v", err)
				return
			}
			msgType, _, err := decodeUsbpFrame(raw)
			if err != nil {
				continue
			}
			if msgType == msgDetach || msgType == msgReset {
				return
			}
		}
	}()

	dataConn, err = opts.OpenDataChannel(dataChannelPrefix + "-" + session.BusID)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("browser data channel: %w", err)
	}

	stop = func() {
		_ = stream.sendFrame(encodeDetachFrame(1))
		_ = conn.Close()
		_ = dataConn.Close()
	}
	return dataConn, stop, nil
}
