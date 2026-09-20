package tailscale

// UsbTunnelBridge lets the closed rust-shine usb-broker's outbound USB/IP
// data-plane dial actually reach the client over Tailscale.
//
// tsnet runs userspace-only (no kernel TUN — see TailscaleService's own doc
// comment on the client side), so nothing outside this Go process can route
// to a peer's 100.x IP; the broker is a separate subprocess with no access
// to this process's *tsnet.Server at all. And the broker's own view of "who
// is the client" (the AES control-plane connection's peer address) is
// useless in Tailscale mode anyway, since that connection itself only
// reaches the broker via StreamProxy's tsnet→127.0.0.1 hairpin (see
// stream_proxy.go) — from the broker's side, every Tailscale client looks
// like 127.0.0.1.
//
// So instead: StreamProxy records the *real* tsnet peer address the moment
// it accepts that hairpinned connection (RememberPeer, called from
// handleTCP for the USB control-plane port only). The broker, needing to
// reach that same peer's USB/IP export port, dials this bridge on loopback
// instead of the real address, sends the target port as a bare line, and
// this bridge dials out through tsnet.Server.Dial to whichever peer it last
// remembered — then relays bytes 1:1. Those bytes are already AES-GCM-
// wrapped by the broker (see usb-passthrough's tunnel.rs) — this bridge
// never decrypts anything, it's a dumb pipe.
//
// Only used when the broker's own direct dial would hit a tsnet hairpin
// artifact (see resolve_dial_target in bin/usb-broker/src/main.rs) — a
// genuine Direct/LAN attach never touches this at all.

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// DefaultUsbBridgeAddr is where UsbTunnelBridge listens for the broker
// subprocess — loopback-only, fixed so it can be baked into the broker's
// --tsnet-bridge flag at spawn time (see agent/internal/usbpass/service.go).
const DefaultUsbBridgeAddr = "127.0.0.1:18092"

type UsbTunnelBridge struct {
	svc *Service

	mu   sync.Mutex
	ln   net.Listener
	peer net.Addr
}

func NewUsbTunnelBridge(svc *Service) *UsbTunnelBridge {
	return &UsbTunnelBridge{svc: svc}
}

// RememberPeer records the real tsnet remote address of the most recent
// connection accepted for the USB control-plane port. See StreamProxy's
// handleTCP, which calls this for exactly that port and no other.
func (b *UsbTunnelBridge) RememberPeer(remote net.Addr) {
	b.mu.Lock()
	b.peer = remote
	b.mu.Unlock()
}

// Start binds addr (loopback — the broker subprocess is the only intended
// caller) and returns the bound address. Safe to call once at agent
// startup and leave running for the process's whole lifetime; RememberPeer
// and Stop are the only other things that touch it afterward.
func (b *UsbTunnelBridge) Start(addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	b.mu.Lock()
	b.ln = ln
	b.mu.Unlock()
	go b.acceptLoop()
	logrus.Infof("🛰️ [UsbTunnelBridge] listening %s", ln.Addr())
	return ln.Addr().String(), nil
}

func (b *UsbTunnelBridge) Stop() {
	b.mu.Lock()
	ln := b.ln
	b.ln = nil
	b.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}

func (b *UsbTunnelBridge) acceptLoop() {
	for {
		b.mu.Lock()
		ln := b.ln
		b.mu.Unlock()
		if ln == nil {
			return
		}
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go b.handle(conn)
	}
}

// handle reads one newline-terminated ASCII port number, then relays the
// rest of the connection (opaque AEAD bytes) to that port on whichever
// tsnet peer RememberPeer last recorded.
func (b *UsbTunnelBridge) handle(local net.Conn) {
	defer local.Close()
	_ = local.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(local)
	line, err := reader.ReadString('\n')
	if err != nil {
		logrus.Debugf("🛰️ [UsbTunnelBridge] header read: %v", err)
		return
	}
	_ = local.SetReadDeadline(time.Time{})
	port := strings.TrimSpace(line)

	b.mu.Lock()
	peer := b.peer
	b.mu.Unlock()
	if peer == nil {
		logrus.Warnf("🛰️ [UsbTunnelBridge] no known tsnet peer yet for USB dial-out (port %s)", port)
		return
	}
	host, _, err := net.SplitHostPort(peer.String())
	if err != nil {
		host = peer.String()
	}
	target := net.JoinHostPort(host, port)

	srv, err := b.svc.Server()
	if err != nil {
		logrus.Errorf("🛰️ [UsbTunnelBridge] tsnet unavailable: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	remote, err := srv.Dial(ctx, "tcp", target)
	if err != nil {
		logrus.Errorf("🛰️ [UsbTunnelBridge] dial %s via tsnet: %v", target, err)
		return
	}
	defer remote.Close()
	logrus.Infof("🛰️ [UsbTunnelBridge] USB/IP tunnel via tsnet to %s", target)

	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		io.Copy(remote, reader) //nolint:errcheck
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		io.Copy(local, remote) //nolint:errcheck
	}()
	<-done
}
