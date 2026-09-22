package browserusb

import (
	"crypto/rand"
	"fmt"
	"net"

	"usbridge-client/pkg/usbpasscore"
)

// browserExportHost is reported to the browser as the attach payload's
// ExportHost. It must resolve to this machine's loopback (127.0.0.0/8 is
// entirely loopback on every platform this agent runs on) while not being
// one of the exact strings rust-shine's is_loopback_host matches
// ("127.0.0.1"/"::1"/"localhost") -- see this package's doc comment (in
// session.go) for why that match must be avoided here.
const browserExportHost = "127.0.0.2"

// tunneledExport is the pair of listeners every browser-sourced device
// needs: a plain, loopback-only Server (StartExport) answering the real
// USB/IP traffic, and an AEAD TunnelListener in front of it that's what the
// broker's relay actually dials -- see session.go's doc comment for why a
// production agent (--tsnet-bridge configured) needs this AEAD layer even
// though the "export" never leaves the machine.
type tunneledExport struct {
	server *usbpasscore.Server
	tunnel *usbpasscore.TunnelListener

	exportHost string
	exportPort string
	nonce      []byte
}

// startTunneledExport starts dev's plain export plus its AEAD tunnel front,
// arming the tunnel with a key both this process and rust-shine's broker
// (given the returned nonce, inside the browser's own Attach frame) derive
// independently from secret (the agent's HMAC master key) -- never sent on
// the wire itself.
func startTunneledExport(busID string, dev *usbpasscore.ExportedDevice, secret []byte) (*tunneledExport, error) {
	internalAddr, err := freeLoopbackAddr("127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("browserusb: allocate loopback port: %w", err)
	}

	server, err := usbpasscore.StartExport(internalAddr, []*usbpasscore.ExportedDevice{dev})
	if err != nil {
		return nil, fmt.Errorf("browserusb: start export on %s: %w", internalAddr, err)
	}

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		server.Stop()
		return nil, fmt.Errorf("browserusb: tunnel nonce: %w", err)
	}
	sessionKey := usbpasscore.DeriveSessionKey(secret)
	tunnelKey := usbpasscore.DeriveTunnelKey(sessionKey, busID, nonce)
	usbpasscore.RegisterTunnelKey(busID, tunnelKey)

	tunnel, err := usbpasscore.StartTunnelListener("0.0.0.0:0", internalAddr)
	if err != nil {
		server.Stop()
		return nil, fmt.Errorf("browserusb: start tunnel listener: %w", err)
	}
	_, tunnelPort, err := net.SplitHostPort(tunnel.Addr())
	if err != nil {
		tunnel.Stop()
		server.Stop()
		return nil, fmt.Errorf("browserusb: tunnel listener addr: %w", err)
	}

	return &tunneledExport{
		server:     server,
		tunnel:     tunnel,
		exportHost: browserExportHost,
		exportPort: tunnelPort,
		nonce:      nonce,
	}, nil
}

func (t *tunneledExport) Close() {
	t.tunnel.Stop()
	t.server.Stop()
}

// freeLoopbackAddr asks the OS for an ephemeral port on addr by briefly
// binding to it and reading back what it chose, then releasing it -- the
// caller does its own net.Listen right after, so there's an unavoidable
// (tiny) TOCTOU window between the two binds, same as any other "reserve a
// port" pattern.
func freeLoopbackAddr(addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	got := ln.Addr().String()
	_ = ln.Close()
	return got, nil
}
