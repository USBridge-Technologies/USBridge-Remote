//go:build android || windows || ios

package service

// moonlightTSNetProxy enables Moonlight streaming over userspace tsnet (DERP-only
// internet paths) by proxying all C moonlight-common-c connections through Go's
// tsnet stack.
//
// Problem: moonlight-common-c uses kernel BSD sockets that can't route to Tailscale
// 100.x IPs when tsnet is in userspace mode (no kernel tun interface on Android or
// when system Tailscale is not installed on Windows).
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
	"strconv"
	"strings"
	"sync"
	"time"

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

	var clientAddr *net.UDPAddr
	var clientMu sync.Mutex

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
				clientMu.Lock()
				clientAddr = addr
				clientMu.Unlock()
				c2sCount++
				if c2sCount <= 3 || c2sCount%200 == 0 {
					logrus.Infof("🌕 [Moonlight/Proxy] c→s serverPort=%d pkt#%d %d bytes from %s", serverPort, c2sCount, n, addr)
				}
				tsConn.Write(buf[:n]) //nolint:errcheck
			}
		}
	}()

	// server → client (video/audio RTP frames)
	go func() {
		buf := make([]byte, 65536)
		s2cCount := 0
		s2cDropped := 0
		s2cErrs := 0
		for {
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
						// The proxy (or the whole session) is shutting down —
						// this fd is really gone, no point retrying.
						return
					}
					// Anything else (observed: ECONNREFUSED, from an ICMP
					// port-unreachable when the host's own audio/video RTP
					// socket for this session hasn't finished binding yet —
					// a startup race, not a permanent failure) used to be
					// treated as fatal and returned here, permanently killing
					// this port's relay for the rest of the session even
					// though the host's socket becomes reachable moments
					// later. That silently broke audio (and would have
					// intermittently broken video too) specifically when
					// streaming through this tsnet relay — the direct-LAN
					// path never engages this code at all, which is why it
					// only showed up over Tailscale/DERP. Log and keep
					// retrying instead.
					s2cErrs++
					if s2cErrs <= 5 || s2cErrs%200 == 0 {
						logrus.Warnf("🌕 [Moonlight/Proxy] s→c serverPort=%d tsConn.Read error (retrying) #%d: %v", serverPort, s2cErrs, readErr)
					}
					continue
				}
			}
			if n > 0 {
				clientMu.Lock()
				ca := clientAddr
				clientMu.Unlock()
				if ca != nil {
					s2cCount++
					if s2cCount <= 3 || s2cCount%200 == 0 {
						logrus.Infof("🌕 [Moonlight/Proxy] s→c serverPort=%d pkt#%d %d bytes → %s", serverPort, s2cCount, n, ca)
					}
					ln.WriteToUDP(buf[:n], ca) //nolint:errcheck
				} else {
					s2cDropped++
					if s2cDropped <= 3 || s2cDropped%200 == 0 {
						logrus.Warnf("🌕 [Moonlight/Proxy] s→c serverPort=%d DROPPED #%d (%d bytes) — no client addr known yet (client never sent a packet on this proxy)", serverPort, s2cDropped, n)
					}
				}
			}
		}
	}()

	return localPort, nil
}

// ── ENet control UDP proxy ────────────────────────────────────────────────────

// runControlProxy proxies ENet UDP control traffic between the C library (at
// 127.0.0.1:47999) and the Sunshine server via tsnet.
func (p *moonlightTSNetProxy) runControlProxy() {
	srv, err := p.ts.serverInstance()
	if err != nil {
		logrus.Errorf("🌕 [Moonlight/Proxy] control proxy: tsnet unavailable: %v", err)
		return
	}

	serverAddr := net.JoinHostPort(p.serverHost, "47999")

	var (
		tsConn   net.Conn
		enetAddr *net.UDPAddr
		once     sync.Once
		dialErr  error
	)

	buf := make([]byte, 65536)
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

		// Lazily create tsnet UDP connection on first ENet packet.
		once.Do(func() {
			enetAddr = addr
			tsConn, dialErr = srv.Dial(p.ctx, "udp", serverAddr)
			if dialErr != nil {
				logrus.Errorf("🌕 [Moonlight/Proxy] control: tsnet dial %s: %v", serverAddr, dialErr)
				return
			}
			logrus.Infof("🌕 [Moonlight/Proxy] ENet control via tsnet to %s, enet_client=%v", serverAddr, addr)

			localConn := p.ctrlListener
			go func() {
				rbuf := make([]byte, 65536)
				for {
					tsConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
					rn, rerr := tsConn.Read(rbuf)
					if rerr != nil {
						select {
						case <-p.ctx.Done():
							return
						default:
							if netErr, ok := rerr.(net.Error); ok && netErr.Timeout() {
								continue
							}
							return
						}
					}
					if enetAddr != nil && rn > 0 {
						localConn.WriteToUDP(rbuf[:rn], enetAddr)
					}
				}
			}()
		})

		if dialErr != nil || tsConn == nil {
			continue
		}
		tsConn.Write(buf[:n]) //nolint:errcheck
	}
}
