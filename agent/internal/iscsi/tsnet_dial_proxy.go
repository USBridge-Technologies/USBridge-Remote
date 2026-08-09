package iscsi

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/sirupsen/logrus"
)

// Dialer is satisfied by *tsnet.Server (tailscale.com/tsnet) — kept as a
// narrow interface here so this package doesn't need to import tsnet or
// the agent's tailscale package directly.
type Dialer interface {
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
}

// TsnetDialProxy bridges a local TCP listener to a single remote address,
// dialing out through the agent's own embedded tsnet identity for every
// accepted connection.
//
// Why this exists: iscsiadm is a real system binary (not something this
// process can hand a Go net.Conn to) — it can only ever open a real OS
// socket. When the client is reached over Tailscale, the agent's *own*
// tailnet identity is a userspace tsnet stack with no kernel route at all
// (see client's TailscaleService.Listen doc comment for the mirror-image
// problem on that side) — so a raw iscsiadm connection to the client's
// tailnet IP instead goes out over whatever OS-level network the host
// actually has (which may be a system Tailscale install, a different
// tailnet, or nothing at all — many hosts only ever run this agent's
// embedded tsnet, no system-level Tailscale). Routing it through this
// proxy instead makes the control-plane (HTTP API) and data-plane (iSCSI)
// connections consistently use the exact same tailnet identity.
type TsnetDialProxy struct {
	ln         net.Listener
	dial       Dialer
	remoteAddr string

	mu     sync.Mutex
	cancel context.CancelFunc
	closed bool
}

// StartTsnetDialProxy starts listening on 127.0.0.1:0 (an OS-assigned free
// port) and returns the proxy along with that local address. Every
// connection accepted on it is bridged to remoteAddr via dial.Dial.
func StartTsnetDialProxy(dial Dialer, remoteAddr string) (*TsnetDialProxy, string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("tsnet dial proxy: listen: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &TsnetDialProxy{ln: ln, dial: dial, remoteAddr: remoteAddr, cancel: cancel}
	go p.run(ctx)
	return p, ln.Addr().String(), nil
}

func (p *TsnetDialProxy) run(ctx context.Context) {
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
			default:
				logrus.Warnf("⚠️ [ISCSI-TSPROXY] accept: %v", err)
			}
			return
		}
		go p.handle(ctx, conn)
	}
}

func (p *TsnetDialProxy) handle(ctx context.Context, local net.Conn) {
	defer local.Close()

	remote, err := p.dial.Dial(ctx, "tcp", p.remoteAddr)
	if err != nil {
		logrus.Warnf("⚠️ [ISCSI-TSPROXY] tsnet dial %s: %v", p.remoteAddr, err)
		return
	}
	defer remote.Close()

	done := make(chan struct{}, 2)
	go func() { io.Copy(remote, local); done <- struct{}{} }() //nolint:errcheck
	go func() { io.Copy(local, remote); done <- struct{}{} }() //nolint:errcheck

	select {
	case <-done:
	case <-ctx.Done():
	}
}

// Stop closes the local listener and cancels any in-flight relays.
func (p *TsnetDialProxy) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	p.cancel()
	p.ln.Close()
}
