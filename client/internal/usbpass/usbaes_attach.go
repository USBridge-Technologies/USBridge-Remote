//go:build linux || windows || darwin

package usbpass

// Pure-Go reimplementation of `usbridge-usb-broker --role client` (rust-shine
// crates/usb-passthrough + bin/usb-broker/src/main.rs run_client/attach_to_agent).
// The client side of that exchange carries no licensing/entitlement logic in
// rust-shine either — require_licensed() is only called from run_agent().
// The pro/enterprise entitlement gate stays exactly where it was: on the closed
// agent binary running on the Windows side. This file only ever talks to
// that agent as a client over AES-GCM/TCP; it never touches usbip-win2 VHCI
// or any licensing code.
//
// Wire format is byte-for-byte compatible with the Rust peer (see
// usbaes_transport.go / usbaes_protocol.go) so the agent binary does not
// change at all.

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// usbAesProtoVersion matches usb_passthrough::protocol::VERSION in rust-shine.
const usbAesProtoVersion byte = 1

type aesAttachSession struct {
	conn   net.Conn
	stream *aeadStream
}

var (
	attachMu sync.Mutex
	attach   *aesAttachSession
)

// Attach dials the agent's AES control plane directly, performs Hello/Attach,
// and — once the agent has acknowledged — holds the session open in the
// background until StopAttach, a detach/reset from the agent, or the
// connection drops. It replaces the previous exec.Command("usbridge-usb-broker",
// "--role", "client", ...) spawn; the client no longer needs that binary
// staged at all.
func Attach(opts AttachOptions) error {
	if opts.ExportService == "" {
		opts.ExportService = "3240"
	}
	if opts.USBIPBusID == "" {
		opts.USBIPBusID = StableUSBIPBusID(opts.InstanceID)
	}
	vid, pid, err := ParseVIDPID(opts.VID, opts.PID)
	if err != nil {
		return fmt.Errorf("usb passthrough attach: %w", err)
	}

	// One active AES session at a time, matching the previous broker
	// process's behaviour (StopAttach was called at the top of Attach()).
	StopAttach()

	key := deriveSessionKey([]byte(opts.Secret))
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.Dial("tcp", opts.AgentAddr)
	if err != nil {
		return fmt.Errorf("connect agent %s: %w", opts.AgentAddr, err)
	}
	stream, err := newAeadStream(conn, key)
	if err != nil {
		conn.Close()
		return fmt.Errorf("aes session: %w", err)
	}

	if err := stream.sendFrame(encodeHelloFrame(usbAesRoleClient, usbAesProtoVersion)); err != nil {
		conn.Close()
		return fmt.Errorf("hello: %w", err)
	}
	ackRaw, err := stream.recvFrame()
	if err != nil {
		conn.Close()
		return fmt.Errorf("hello-ack: %w", err)
	}
	msgType, payload, err := decodeUsbpFrame(ackRaw)
	if err != nil {
		conn.Close()
		return fmt.Errorf("hello-ack: %w", err)
	}
	if msgType != msgHelloAck {
		conn.Close()
		return fmt.Errorf("expected hello-ack, got msg type %d", msgType)
	}
	ok, detail, err := decodeHelloAck(payload)
	if err != nil {
		conn.Close()
		return fmt.Errorf("hello-ack: %w", err)
	}
	if !ok {
		conn.Close()
		return fmt.Errorf("agent refused hello: %s", detail)
	}
	logrus.Infof("usbpass: agent hello ack: %s", detail)

	attachFrame := attachPayload{
		BusID:         opts.USBIPBusID,
		VID:           vid,
		PID:           pid,
		Speed:         3, // matches rust-shine's hardcoded Speed::Super placeholder
		DeviceDesc:    syntheticDeviceDesc(vid, pid),
		ConfigDesc:    syntheticMSCConfig(),
		ExportHost:    "", // empty → agent uses the AES peer IP (Direct/Tailscale)
		ExportService: opts.ExportService,
	}
	if err := stream.sendFrame(encodeAttachFrame(attachFrame)); err != nil {
		conn.Close()
		return fmt.Errorf("attach: %w", err)
	}
	logrus.Infof("usbpass: AES attach sent to %s bus=%s vid:pid=%04x:%04x (Go owns USB/IP :%s)",
		opts.AgentAddr, opts.USBIPBusID, vid, pid, opts.ExportService)

	s := &aesAttachSession{conn: conn, stream: stream}
	attachMu.Lock()
	attach = s
	attachMu.Unlock()

	go holdAttachSession(s)
	return nil
}

// holdAttachSession keeps the AES session open, mirroring attach_to_agent's
// keep_alive loop: read frames until the agent detaches/resets or the
// connection drops, then best-effort send a Detach and close.
func holdAttachSession(s *aesAttachSession) {
	for {
		raw, err := s.stream.recvFrame()
		if err != nil {
			logrus.Infof("usbpass: AES session ended: %v", err)
			break
		}
		msgType, _, err := decodeUsbpFrame(raw)
		if err != nil {
			logrus.Warnf("usbpass: AES session: bad frame: %v", err)
			continue
		}
		if msgType == msgDetach || msgType == msgReset {
			logrus.Infof("usbpass: agent requested detach")
			break
		}
	}
	_ = s.stream.sendFrame(encodeDetachFrame(1))
	_ = s.conn.Close()

	attachMu.Lock()
	if attach == s {
		attach = nil
	}
	attachMu.Unlock()
}

// StopAttach ends the active AES session, if any.
func StopAttach() {
	attachMu.Lock()
	s := attach
	attach = nil
	attachMu.Unlock()
	if s == nil {
		return
	}
	_ = s.stream.sendFrame(encodeDetachFrame(1))
	_ = s.conn.Close()
}
