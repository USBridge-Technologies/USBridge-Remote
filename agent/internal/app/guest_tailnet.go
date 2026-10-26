package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	"usbridge_agent/internal/guestauth"
	"usbridge_agent/internal/tailscale"
)

// The guest network: a second tsnet node of this machine on the USBridge-run
// Headscale, separate from the owner's own Tailscale (a.ts). Guests' apps join
// the same Headscale with a key from the backend; only nodes named after a live
// share are let through (guestauth.NodeName), everything else is closed before
// a byte is relayed. The node exists only while a share is live.

type guestNode struct {
	svc   *tailscale.Service
	proxy *tailscale.StreamProxy
	http  *http.Server
	ip    string
}

type guestNodeState struct {
	mu       sync.Mutex
	node     *guestNode
	reported string // the address last reported to the backend for the live shares
	nextTry  time.Time
}

// syncGuestNode keeps the guest node matched to the live shares: up (and its
// address reported to the backend) while a share on the USBridge guest network
// is live, down otherwise. Called from the watchdog, so it also brings the node
// back after the agent restarts.
func (a *App) syncGuestNode(ctx context.Context) {
	if a.guests == nil || a.cfg.GuestOwnTailscale {
		return
	}
	if !a.hasLiveGuestShare() {
		a.stopGuestNode()
		return
	}
	st := &a.guestNodeState
	st.mu.Lock()
	wait := time.Now().Before(st.nextTry)
	st.mu.Unlock()
	if wait {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ip, err := a.ensureGuestNode(cctx)
	st.mu.Lock()
	defer st.mu.Unlock()
	if err != nil {
		log.Printf("[guest-net] guest node: %v", err)
		st.nextTry = time.Now().Add(30 * time.Second)
		return
	}
	if st.reported == ip {
		return
	}
	ok := true
	now := time.Now()
	for _, sh := range a.guests.Store.List() {
		if !sh.Live(now) {
			continue
		}
		if err := a.guests.Cloud.SetShareHost(cctx, sh.ID, ip); err != nil {
			log.Printf("[guest-net] report address for share %s: %v", sh.ID, err)
			ok = false
		}
	}
	if ok {
		st.reported = ip
	} else {
		st.nextTry = time.Now().Add(30 * time.Second)
	}
}

// ensureGuestNode brings the guest node up if needed and returns its address.
func (a *App) ensureGuestNode(ctx context.Context) (string, error) {
	st := &a.guestNodeState
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.node != nil {
		return st.node.ip, nil
	}
	if a.guests == nil || a.guests.Cloud == nil {
		return "", errors.New("no cloud")
	}
	key, err := a.guests.Cloud.TailnetKey(ctx)
	if err != nil {
		return "", fmt.Errorf("tailnet key: %w", err)
	}
	svc := tailscale.NewGuestNode(a.cfg.StateDir, key.ControlURL, key.AuthKey, key.Hostname)
	n := &guestNode{svc: svc}
	// Starting the node registers it with the key.
	if _, err := svc.Server(); err != nil {
		_ = svc.Close()
		return "", fmt.Errorf("join guest network: %w", err)
	}
	ip, err := waitNodeIP(ctx, svc, 45*time.Second)
	if err != nil {
		_ = svc.Close()
		return "", err
	}
	n.ip = ip

	admit := func(remote net.Addr) bool { return a.admitGuestPeer(svc, remote) }
	basePort := a.cfg.SunshinePort - 1 // as restartStreamProxy: NvHTTP base = admin port - 1
	n.proxy = svc.StartStreamProxy(basePort, nil, nil)
	n.proxy.SetAdmit(admit)

	srv, err := svc.Server()
	if err != nil {
		n.proxy.Stop()
		_ = svc.Close()
		return "", err
	}
	ln, err := srv.Listen("tcp", fmt.Sprintf(":%d", a.cfg.HTTPPort))
	if err != nil {
		n.proxy.Stop()
		_ = svc.Close()
		return "", fmt.Errorf("guest network listen: %w", err)
	}
	n.http = &http.Server{Handler: a.handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := n.http.Serve(&admitListener{Listener: ln, allow: admit}); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[guest-net] http: %v", err)
		}
	}()
	log.Printf("[guest-net] guest node %s up at %s", key.Hostname, ip)
	st.node = n
	return ip, nil
}

// stopGuestNode takes the guest node down (no live share left).
func (a *App) stopGuestNode() {
	st := &a.guestNodeState
	st.mu.Lock()
	n := st.node
	st.node = nil
	st.mu.Unlock()
	if n == nil {
		return
	}
	if n.http != nil {
		_ = n.http.Close()
	}
	if n.proxy != nil {
		n.proxy.Stop()
	}
	_ = n.svc.Close()
	log.Printf("[guest-net] guest node down")
	st.mu.Lock()
	st.reported = ""
	st.mu.Unlock()
}

// admitGuestPeer reports whether the node behind remote belongs to a live share.
func (a *App) admitGuestPeer(svc *tailscale.Service, remote net.Addr) bool {
	if a.guests == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	name, tags, err := svc.WhoIsPeer(ctx, remote.String())
	if err != nil || !slices.Contains(tags, "tag:guest") {
		return false
	}
	now := time.Now()
	for _, sh := range a.guests.Store.List() {
		if sh.Live(now) && guestauth.NodeName(sh.ID) == name {
			return true
		}
	}
	return false
}

func waitNodeIP(ctx context.Context, svc *tailscale.Service, d time.Duration) (string, error) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		st, err := svc.Status(sctx)
		cancel()
		if err == nil && st != nil && st.Running && st.Self.IP4 != "" {
			return st.Self.IP4, nil
		}
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "", errors.New("the guest network did not come up in time")
}

// admitListener closes connections from peers that are not let in.
type admitListener struct {
	net.Listener
	allow func(net.Addr) bool
}

func (l *admitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.allow(c.RemoteAddr()) {
			return c, nil
		}
		c.Close()
	}
}
