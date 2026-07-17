package tailscale

// StreamProxy is the receiving-side counterpart to the client's
// moonlight_tsnet_proxy.go. Sunshine speaks real kernel BSD sockets; the
// agent's tsnet is a userspace-only netstack with no kernel interface for its
// 100.x IP, so a remote client's connection to that IP never reaches
// Sunshine's real sockets unless something inside the tsnet netstack
// explicitly Listens for it and relays the bytes to Sunshine on 127.0.0.1.
//
// This listens on tsnet for Sunshine's fixed ports (HTTP/HTTPS/RTSP/ENet
// control) and relays 1:1 to the same port on 127.0.0.1 — no port rewriting
// is needed here (unlike the client's proxy) because the port number Sunshine
// picked is already correct; the client just needs a listener to exist at
// that same number inside the agent's tsnet netstack. Video/audio RTP ports
// are discovered the same way the client discovers them: by snooping the
// RTSP SETUP response for "server_port=X" and opening a relay for each one
// seen.
//
// Sunshine is a prebuilt third-party binary (see fetch_sunshine.sh) with no
// support for listening on a Unix domain socket, and RTSP/ENet/RTP are wire
// protocols the Moonlight client speaks directly — so the loopback hop here
// has to stay TCP/UDP to 127.0.0.1. That hop's overhead is negligible next to
// the actual WireGuard/DERP path to the client; the fix that matters is that
// this relay exists at all.
import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"tailscale.com/tsnet"
)

// StartStreamProxy starts relaying Sunshine's streaming ports through tsnet.
// basePort is Sunshine's NVHTTP base port (sunshine.conf's "port", default
// 47989); HTTPS/control/RTSP ports are derived from it using Sunshine's fixed
// offsets. Returns immediately; listeners come up in the background once
// tsnet has a valid tailnet IP.
func (s *Service) StartStreamProxy(basePort int) *StreamProxy {
	p := &StreamProxy{svc: s, basePort: basePort, seenUDP: make(map[int]bool)}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	go p.run()
	return p
}

type StreamProxy struct {
	svc      *Service
	basePort int

	ctx    context.Context
	cancel context.CancelFunc

	srv    *tsnet.Server
	selfIP string

	mu        sync.Mutex
	listeners []io.Closer
	seenUDP   map[int]bool
}

// Stop tears down every listener and relay goroutine this proxy started.
func (p *StreamProxy) Stop() {
	p.cancel()
	p.mu.Lock()
	ls := p.listeners
	p.listeners = nil
	p.mu.Unlock()
	for _, l := range ls {
		l.Close()
	}
}

func (p *StreamProxy) run() {
	srv, err := p.svc.Server()
	if err != nil {
		logrus.Errorf("🛰️ [StreamProxy] tsnet server unavailable: %v", err)
		return
	}
	p.srv = srv

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}
		ip4, _ := srv.TailscaleIPs()
		if ip4.IsValid() {
			p.selfIP = ip4.String()
			break
		}
		time.Sleep(time.Second)
	}

	httpPort := p.basePort
	httpsPort := p.basePort - 5
	controlPort := p.basePort + 10
	rtspPort := p.basePort + 21

	logrus.Infof("🛰️ [StreamProxy] relaying Sunshine ports via tsnet at %s (http=%d https=%d control=%d rtsp=%d)",
		p.selfIP, httpPort, httpsPort, controlPort, rtspPort)

	p.startTCPRelay(httpPort, false)
	p.startTCPRelay(httpsPort, false)
	p.startTCPRelay(rtspPort, true)
	p.startUDPRelay(controlPort)
}

func (p *StreamProxy) addListener(c io.Closer) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.ctx.Done():
		return false
	default:
	}
	p.listeners = append(p.listeners, c)
	return true
}

// ── TCP relay (HTTP, HTTPS, RTSP) ──────────────────────────────────────────

func (p *StreamProxy) startTCPRelay(port int, snoopRTSP bool) {
	ln, err := p.srv.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		logrus.Errorf("🛰️ [StreamProxy] tsnet listen tcp :%d: %v", port, err)
		return
	}
	if !p.addListener(ln) {
		ln.Close()
		return
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-p.ctx.Done():
				default:
					logrus.Warnf("🛰️ [StreamProxy] accept :%d: %v", port, err)
				}
				return
			}
			go p.handleTCP(conn, port, snoopRTSP)
		}
	}()
}

func (p *StreamProxy) handleTCP(remote net.Conn, port int, snoopRTSP bool) {
	defer remote.Close()

	local, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		logrus.Warnf("🛰️ [StreamProxy] dial local :%d: %v", port, err)
		return
	}
	defer local.Close()

	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		io.Copy(local, remote) //nolint:errcheck
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		if snoopRTSP {
			p.copyAndSnoop(remote, local)
		} else {
			io.Copy(remote, local) //nolint:errcheck
		}
	}()
	<-done
}

// copyAndSnoop copies Sunshine's RTSP response back to the remote client,
// watching for "server_port=X" so a UDP relay can be opened for each RTP
// port Sunshine picked — mirroring the client's rewriteServerPorts, but
// without rewriting: the port number is already correct, we just need a
// listener at that number inside our own tsnet netstack.
func (p *StreamProxy) copyAndSnoop(dst, src net.Conn) {
	buf := make([]byte, 32768)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			data := buf[:n]
			if bytes.Contains(data, []byte("server_port=")) {
				p.scanServerPorts(data)
			}
			dst.Write(data) //nolint:errcheck
		}
		if err != nil {
			return
		}
	}
}

func (p *StreamProxy) scanServerPorts(data []byte) {
	s := string(data)
	offset := 0
	for {
		idx := strings.Index(s[offset:], "server_port=")
		if idx < 0 {
			break
		}
		abs := offset + idx
		rest := s[abs+len("server_port="):]
		end := strings.IndexAny(rest, "\r\n ;")
		if end < 0 {
			break
		}
		portPair := rest[:end]
		offset = abs + len("server_port=") + end

		portStr := strings.SplitN(portPair, "-", 2)[0]
		port, err := strconv.Atoi(strings.TrimSpace(portStr))
		if err != nil || port <= 1024 {
			continue
		}
		p.ensureUDPRelay(port)
	}
}

// ── UDP relay (ENet control + per-session RTP ports) ───────────────────────

func (p *StreamProxy) ensureUDPRelay(port int) {
	p.mu.Lock()
	if p.seenUDP[port] {
		p.mu.Unlock()
		return
	}
	p.seenUDP[port] = true
	p.mu.Unlock()
	logrus.Infof("🛰️ [StreamProxy] SETUP server_port=%d → opening tsnet UDP relay", port)
	p.startUDPRelay(port)
}

func (p *StreamProxy) startUDPRelay(port int) {
	addr := net.JoinHostPort(p.selfIP, strconv.Itoa(port))
	pc, err := p.srv.ListenPacket("udp4", addr)
	if err != nil {
		logrus.Errorf("🛰️ [StreamProxy] tsnet listen udp %s: %v", addr, err)
		return
	}
	if !p.addListener(pc) {
		pc.Close()
		return
	}

	local, err := net.Dial("udp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		logrus.Errorf("🛰️ [StreamProxy] dial local udp :%d: %v", port, err)
		return
	}
	if !p.addListener(local) {
		local.Close()
		return
	}

	var (
		remoteAddr net.Addr
		remoteMu   sync.Mutex
	)

	// remote (tsnet) → local (Sunshine)
	go func() {
		buf := make([]byte, 65536)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				select {
				case <-p.ctx.Done():
				default:
					if !errors.Is(err, net.ErrClosed) {
						logrus.Warnf("🛰️ [StreamProxy] udp :%d read: %v", port, err)
					}
				}
				return
			}
			if n > 0 {
				remoteMu.Lock()
				remoteAddr = addr
				remoteMu.Unlock()
				local.Write(buf[:n]) //nolint:errcheck
			}
		}
	}()

	// local (Sunshine) → remote (tsnet)
	go func() {
		buf := make([]byte, 65536)
		for {
			n, err := local.Read(buf)
			if err != nil {
				select {
				case <-p.ctx.Done():
					return
				default:
				}
				if errors.Is(err, net.ErrClosed) {
					return
				}
				// ECONNREFUSED from an ICMP port-unreachable while Sunshine's
				// own RTP socket for this session is still binding is a
				// startup race, not a permanent failure — keep retrying
				// (same fix as the client's tsnet proxy).
				continue
			}
			if n > 0 {
				remoteMu.Lock()
				ra := remoteAddr
				remoteMu.Unlock()
				if ra != nil {
					pc.WriteTo(buf[:n], ra) //nolint:errcheck
				}
			}
		}
	}()
}
