//go:build !(js && wasm)

package service

// moonlightTSNetProxy enables Moonlight streaming over userspace tsnet (DERP-only
// internet paths) by proxying all C moonlight-common-c connections through Go's
// tsnet stack. Tailscale runs in userspace (tsnet) mode on every platform, so
// this proxy is used universally.
//
// Problem: moonlight-common-c uses kernel BSD sockets that can't route to
// Tailscale 100.x IPs — only Go's tsnet stack (an in-process userspace
// WireGuard/netstack, no kernel TUN) can reach them.
//
// Architecture:
//
//	TCP:  C lib → 127.0.0.1:rtspPort → [proxy] → tsnet → server:48010  (RTSP)
//	      SETUP responses are rewritten: server_port=X → server_port=LOCAL_PORT
//
//	UDP:  C lib → 127.0.0.1:LOCAL_PORT → [proxy] → tsnet → server:X    (video/audio ping)
//	      server → tsnet → [proxy] → 127.0.0.1:clientDynPort            (video/audio data)
//	      (Sunshine sends video back to the source of the UDP ping, which is our tsnet IP)
//
//	UDP:  C lib → 127.0.0.1:47999 → [proxy] → tsnet → server:47999     (ENet control)

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/sirupsen/logrus"
)

// startMoonlightProxy starts the tsnet proxy for internet (DERP-only) Moonlight
// streaming. Returns a stop func and the local TCP RTSP proxy port, or an error.
func startMoonlightProxy(ts *TailscaleService, serverHost string, rtspPort int) (stop func(), localRTSPPort int, err error) {
	p := &moonlightTSNetProxy{
		ts:              ts,
		serverHost:      serverHost,
		rtspPort:        rtspPort,
		seenServerPorts: make(map[int]bool),
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())

	port, err := p.start()
	if err != nil {
		p.cancel()
		return func() {}, 0, err
	}
	return p.stop, port, nil
}

type moonlightTSNetProxy struct {
	ts         *TailscaleService
	serverHost string
	rtspPort   int

	ctx    context.Context
	cancel context.CancelFunc

	rtspListener net.Listener
	ctrlListener *net.UDPConn

	mu              sync.Mutex
	udpConns        []net.Conn       // tsnet UDP connections (for cleanup)
	localListeners  []net.PacketConn // local UDP listeners (for cleanup)
	seenServerPorts map[int]bool     // server ports already proxied
}

func (p *moonlightTSNetProxy) start() (rtspProxyPort int, err error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("RTSP TCP proxy listen: %w", err)
	}
	p.rtspListener = ln
	rtspProxyPort = ln.Addr().(*net.TCPAddr).Port
	logrus.Infof("🌕 [Moonlight/Proxy] RTSP TCP proxy on 127.0.0.1:%d → %s:%d (via tsnet)",
		rtspProxyPort, p.serverHost, p.rtspPort)
	go p.runRTSPProxy()

	ctrlConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 47999})
	if err != nil {
		ln.Close()
		return 0, fmt.Errorf("ENet control UDP proxy bind :47999: %w", err)
	}
	p.ctrlListener = ctrlConn
	logrus.Infof("🌕 [Moonlight/Proxy] ENet control UDP proxy on 127.0.0.1:47999 → %s:47999 (via tsnet)",
		p.serverHost)
	go p.runControlProxy()

	return rtspProxyPort, nil
}

func (p *moonlightTSNetProxy) stop() {
	p.cancel()
	if p.rtspListener != nil {
		p.rtspListener.Close()
	}
	if p.ctrlListener != nil {
		p.ctrlListener.Close()
	}
	p.mu.Lock()
	udpConns := p.udpConns
	listeners := p.localListeners
	p.udpConns = nil
	p.localListeners = nil
	p.mu.Unlock()
	for _, c := range udpConns {
		c.Close()
	}
	for _, l := range listeners {
		l.Close()
	}
	logrus.Info("🌕 [Moonlight/Proxy] stopped")
}

// ── RTSP TCP proxy ────────────────────────────────────────────────────────────

func (p *moonlightTSNetProxy) runRTSPProxy() {
	for {
		conn, err := p.rtspListener.Accept()
		if err != nil {
			select {
			case <-p.ctx.Done():
			default:
				logrus.Warnf("🌕 [Moonlight/Proxy] RTSP accept: %v", err)
			}
			return
		}
		go p.handleRTSP(conn)
	}
}

func (p *moonlightTSNetProxy) handleRTSP(client net.Conn) {
	defer client.Close()

	srv, err := p.ts.serverInstance()
	if err != nil {
		logrus.Errorf("🌕 [Moonlight/Proxy] RTSP: tsnet not ready: %v", err)
		return
	}

	target := net.JoinHostPort(p.serverHost, strconv.Itoa(p.rtspPort))
	server, err := srv.Dial(p.ctx, "tcp", target)
	if err != nil {
		logrus.Errorf("🌕 [Moonlight/Proxy] RTSP: dial %s via tsnet: %v", target, err)
		return
	}
	defer server.Close()
	logrus.Infof("🌕 [Moonlight/Proxy] RTSP tunnel via tsnet to %s", target)

	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		copyConn(server, client)
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		p.proxyAndRewriteResponse(client, server)
	}()
	<-done
}

func copyConn(dst, src net.Conn) {
	buf := make([]byte, 32768)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			dst.Write(buf[:n]) //nolint:errcheck
		}
		if err != nil {
			return
		}
	}
}

// proxyAndRewriteResponse copies server→client RTSP data, rewriting server_port=X
// to point to a local UDP proxy that tunnels via tsnet.
func (p *moonlightTSNetProxy) proxyAndRewriteResponse(dst, src net.Conn) {
	buf := make([]byte, 32768)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			data := buf[:n]
			s := string(data)
			if strings.Contains(s, "server_port=") {
				data = p.rewriteServerPorts(data)
			}
			dst.Write(data) //nolint:errcheck
		}
		if err != nil {
			return
		}
	}
}

// rewriteServerPorts replaces each "server_port=X-Y" in the RTSP SETUP response
// with a local UDP proxy port that tunnels to the real server port via tsnet.
func (p *moonlightTSNetProxy) rewriteServerPorts(data []byte) []byte {
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
		portStr := strings.SplitN(portPair, "-", 2)[0]
		serverPort, err := strconv.Atoi(strings.TrimSpace(portStr))
		if err != nil || serverPort <= 1024 {
			offset = abs + len("server_port=") + end
			continue
		}

		p.mu.Lock()
		seen := p.seenServerPorts[serverPort]
		if !seen {
			p.seenServerPorts[serverPort] = true
		}
		p.mu.Unlock()

		if !seen {
			localPort, proxyErr := p.createServerUDPProxy(serverPort)
			if proxyErr != nil {
				logrus.Errorf("🌕 [Moonlight/Proxy] UDP proxy for server_port=%d: %v", serverPort, proxyErr)
				offset = abs + len("server_port=") + end
				continue
			}
			newPair := fmt.Sprintf("%d-%d", localPort, localPort+1)
			logrus.Infof("🌕 [Moonlight/Proxy] SETUP rewrite: server_port=%s → %s (stream port %d via tsnet)", portPair, newPair, serverPort)
			s = s[:abs] + "server_port=" + newPair + s[abs+len("server_port=")+end:]
			offset = abs + len("server_port=") + len(newPair)
		} else {
			offset = abs + len("server_port=") + end
		}
	}
	return []byte(s)
}

// createServerUDPProxy creates a bidirectional UDP proxy:
//
//	C library → 127.0.0.1:localPort → tsnet → server:serverPort
//	server    → tsnet → 127.0.0.1:localPort → C library (last seen addr)
func (p *moonlightTSNetProxy) createServerUDPProxy(serverPort int) (localPort int, err error) {
	ln, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return 0, fmt.Errorf("local UDP listen: %w", err)
	}
	localPort = ln.LocalAddr().(*net.UDPAddr).Port

	srv, err := p.ts.serverInstance()
	if err != nil {
		ln.Close()
		return 0, fmt.Errorf("tsnet: %w", err)
	}

	serverAddr := net.JoinHostPort(p.serverHost, strconv.Itoa(serverPort))
	tsConn, err := srv.Dial(p.ctx, "udp", serverAddr)
	if err != nil {
		ln.Close()
		return 0, fmt.Errorf("tsnet dial udp %s: %w", serverAddr, err)
	}

	p.mu.Lock()
	p.udpConns = append(p.udpConns, tsConn)
	p.localListeners = append(p.localListeners, ln)
	p.mu.Unlock()

	logrus.Infof("🌕 [Moonlight/Proxy] UDP proxy: 127.0.0.1:%d ↔ tsnet ↔ %s", localPort, serverAddr)

	// clientAddr is written on every inbound client packet and read on every
	// outbound one (the s→c write goroutine below) -- both video-direction
	// hot paths processing hundreds of packets per keyframe burst.
	// atomic.Pointer avoids a lock/unlock pair per packet on that path while
	// keeping the same one-writer/many-readers safety a mutex gave it.
	var clientAddr atomic.Pointer[net.UDPAddr]

	// client → server (pings + control data)
	go func() {
		buf := make([]byte, 65536)
		c2sCount := 0
		for {
			ln.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, addr, readErr := ln.ReadFromUDP(buf)
			if readErr != nil {
				select {
				case <-p.ctx.Done():
					return
				default:
					if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
						continue
					}
					return
				}
			}
			if n > 0 {
				clientAddr.Store(addr)
				c2sCount++
				if c2sCount <= 3 || c2sCount%200 == 0 {
					logrus.Infof("🌕 [Moonlight/Proxy] c→s serverPort=%d pkt#%d %d bytes from %s", serverPort, c2sCount, n, addr)
				}
				// tsConn.Write hands the packet to tsnet's userspace WireGuard
				// netstack — this is the one hop in this proxy that could
				// plausibly block (internal queue/congestion control), as
				// opposed to the OS-level UDP read/write calls around it which
				// are effectively instant. Timing it directly settles whether
				// multi-second Moonlight control-latency reports are a real
				// network problem or queueing introduced right here, instead
				// of guessing from gaps between throttled packet-count logs.
				wStart := time.Now()
				tsConn.Write(buf[:n]) //nolint:errcheck
				if wElapsed := time.Since(wStart); wElapsed > 50*time.Millisecond {
					logrus.Warnf("🌕 [Moonlight/Proxy] c→s serverPort=%d SLOW tsConn.Write: %v (pkt#%d)", serverPort, wElapsed, c2sCount)
				}
			}
		}
	}()

	// Try to set native socket buffers if possible (OS loopback socket)
	ln.SetReadBuffer(4194304)
	ln.SetWriteBuffer(4194304)
	// Try to set native socket buffers on tsConn (Tailscale netstack)
	if sb, ok := tsConn.(interface{ SetReadBuffer(int) error }); ok {
		sb.SetReadBuffer(4194304)
	}
	if sb, ok := tsConn.(interface{ SetWriteBuffer(int) error }); ok {
		sb.SetWriteBuffer(4194304)
	}

	// server → client (video/audio RTP frames)
	// We use a buffered channel to avoid backpressure on the tsnet read loop
	// when the IDR burst of 200 packets arrives. This effectively acts as a 4MB userspace UDP buffer.
	s2cQueue := make(chan []byte, 4096)

	// Write goroutine
	go func() {
		s2cCount := 0
		// Pace packets to avoid overflowing Android's 127.0.0.1 UDP socket buffer
		// (Android enforces a small loopback SO_RCVBUF regardless of the
		// SetReadBuffer call above, so a fast IDR burst can get dropped at the
		// kernel socket layer there). Every other platform's loopback socket
		// honors the 4MB buffer we set, so pacing there only hurts: at 60fps a
		// single frame can legitimately need 100+ packets (confirmed live —
		// "Unrecoverable frame N: ... received < 111 needed" on a 1280x720@60
		// stream), and this limiter's default 5000pps/burst-20 cap takes ~18ms
		// to drain that alone -- longer than the ~16.7ms frame interval, so the
		// backlog it creates never drains and keeps missing moonlight-common-c's
		// FEC recovery window, which is what was actually forcing the repeated
		// "no video frame ... forcing reconnect" cycle on desktop.
		var limiter *rate.Limiter
		if runtime.GOOS == "android" {
			limiter = rate.NewLimiter(rate.Limit(5000), 20)
		}
		for {
			select {
			case <-p.ctx.Done():
				return
			case packet, ok := <-s2cQueue:
				if !ok {
					return
				}
				if limiter != nil {
					limiter.Wait(p.ctx)
				}
				ca := clientAddr.Load()
				if ca != nil {
					s2cCount++
					if s2cCount <= 3 || s2cCount%200 == 0 {
						logrus.Infof("🌕 [Moonlight/Proxy] s→c serverPort=%d pkt#%d %d bytes → %s", serverPort, s2cCount, len(packet), ca)
					}
					wStart := time.Now()
					ln.WriteToUDP(packet, ca) //nolint:errcheck
					if wElapsed := time.Since(wStart); wElapsed > 50*time.Millisecond {
						logrus.Warnf("🌕 [Moonlight/Proxy] s→c serverPort=%d SLOW WriteToUDP: %v (pkt#%d)", serverPort, wElapsed, s2cCount)
					}
				}
			}
		}
	}()

	// Read goroutine
	go func() {
		s2cErrs := 0
		defer close(s2cQueue)

		// Use a large preallocated slab to avoid per-packet allocations and GC pauses
		slabSize := 1024 * 1024 // 1MB slab
		slab := make([]byte, slabSize)
		slabOffset := 0

		for {
			if slabOffset+2048 > slabSize {
				slab = make([]byte, slabSize)
				slabOffset = 0
			}
			buf := slab[slabOffset : slabOffset+2048]

			tsConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, readErr := tsConn.Read(buf)
			if readErr != nil {
				select {
				case <-p.ctx.Done():
					return
				default:
					if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
						continue
					}
					if errors.Is(readErr, net.ErrClosed) {
						return
					}
					s2cErrs++
					if s2cErrs <= 5 || s2cErrs%200 == 0 {
						logrus.Warnf("🌕 [Moonlight/Proxy] s→c serverPort=%d tsConn.Read error (retrying) #%d: %v", serverPort, s2cErrs, readErr)
					}
					continue
				}
			}
			if n > 0 {
				packet := buf[:n]
				slabOffset += n

				select {
				case <-p.ctx.Done():
					return
				case s2cQueue <- packet:
					// Enqueued successfully
				default:
					logrus.Warnf("🌕 [Moonlight/Proxy] s→c serverPort=%d DROPPED packet (s2cQueue is full!)", serverPort)
				}
			}
		}
	}()

	return localPort, nil
}

// ── ENet control UDP proxy ────────────────────────────────────────────────────

// runControlProxy proxies ENet UDP control traffic between the C library (at
// 127.0.0.1:47999) and the Sunshine server via tsnet.
//
// Redials the tsnet side whenever it goes bad instead of giving up after the
// first dial, the way this used to work (a single sync.Once dial for the
// whole proxy's lifetime, with its reader goroutine just returning silently
// -- no log line, nothing -- on any non-timeout error). Confirmed live over
// a DERP-relayed tsnet path: once that single tsConn went quiet, every ENet
// control packet the C library still thought it was sending (including its
// own keepalive pings) silently never left this process, so the *server*
// side's ENet peer -- not this one -- was what eventually declared the
// disconnect, ~10s later (ControlStream.c's enet_peer_timeout). From the
// user's side that looked like "video plays a moment then stops": the whole
// session (video+audio) tears down right along with the control channel,
// moonlight-common-c reconnects fresh, and the exact same thing happens
// again on the new (equally un-redialed) tsConn a few seconds later -- a
// tight, indefinite retry loop, never actually recovering on its own.
func (p *moonlightTSNetProxy) runControlProxy() {
	srv, err := p.ts.serverInstance()
	if err != nil {
		logrus.Errorf("🌕 [Moonlight/Proxy] control proxy: tsnet unavailable: %v", err)
		return
	}

	serverAddr := net.JoinHostPort(p.serverHost, "47999")

	var (
		connMu   sync.Mutex
		tsConn   net.Conn
		enetAddr *net.UDPAddr
		dialing  bool
	)

	// redial (re)establishes tsConn and starts its own read-pump goroutine,
	// relaying server -> local. Safe to call repeatedly (e.g. once per
	// reader-goroutine failure) -- `dialing` collapses concurrent callers
	// (a failing read and a failing write can both notice around the same
	// moment) into a single in-flight redial attempt.
	var redial func()
	redial = func() {
		connMu.Lock()
		if dialing {
			connMu.Unlock()
			return
		}
		dialing = true
		if tsConn != nil {
			tsConn.Close()
			tsConn = nil
		}
		connMu.Unlock()

		conn, dialErr := srv.Dial(p.ctx, "udp", serverAddr)

		connMu.Lock()
		dialing = false
		if dialErr != nil {
			connMu.Unlock()
			select {
			case <-p.ctx.Done():
			default:
				logrus.Errorf("🌕 [Moonlight/Proxy] control: tsnet dial %s: %v", serverAddr, dialErr)
			}
			return
		}
		tsConn = conn
		connMu.Unlock()
		logrus.Infof("🌕 [Moonlight/Proxy] ENet control via tsnet to %s", serverAddr)

		localConn := p.ctrlListener
		go func() {
			rbuf := make([]byte, 65536)
			for {
				conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				rn, rerr := conn.Read(rbuf)
				if rerr != nil {
					select {
					case <-p.ctx.Done():
						return
					default:
						if netErr, ok := rerr.(net.Error); ok && netErr.Timeout() {
							continue
						}
						logrus.Warnf("🌕 [Moonlight/Proxy] control: tsnet connection lost (%v), redialing", rerr)
						redial()
						return
					}
				}
				connMu.Lock()
				dst := enetAddr
				connMu.Unlock()
				if dst != nil && rn > 0 {
					localConn.WriteToUDP(rbuf[:rn], dst) //nolint:errcheck
				}
			}
		}()
	}

	buf := make([]byte, 65536)
	first := true
	for {
		p.ctrlListener.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, addr, err := p.ctrlListener.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-p.ctx.Done():
				return
			default:
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				logrus.Errorf("🌕 [Moonlight/Proxy] control read: %v", err)
				return
			}
		}

		connMu.Lock()
		enetAddr = addr
		connMu.Unlock()
		if first {
			first = false
			redial()
		}

		connMu.Lock()
		conn := tsConn
		connMu.Unlock()
		if conn == nil {
			continue
		}
		if _, werr := conn.Write(buf[:n]); werr != nil {
			select {
			case <-p.ctx.Done():
			default:
				logrus.Warnf("🌕 [Moonlight/Proxy] control write: %v, redialing", werr)
				redial()
			}
		}
	}
}
