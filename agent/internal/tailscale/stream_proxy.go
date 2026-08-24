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
	"sync/atomic"
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
	// Belt-and-suspenders alongside Service.Server()'s own fix (see that
	// method's doc comment on why a failed tsnet.Server.Start() used to get
	// permanently cached and handed back here as if healthy, crashing the
	// whole agent process the moment TailscaleIPs() below touched its
	// never-initialized internals): an unrecovered panic in any goroutine,
	// this one included, is fatal to the entire process, not just this
	// stream-relay session -- confirmed live (see git history for this
	// comment) that exactly that happened every time SetStreamBackend
	// restarted this proxy against a broken cached server. This can't
	// un-crash whatever underlying tsnet state caused a panic, but it keeps
	// one broken stream-proxy attempt from taking Sunshine/RustShine, the
	// admin API, and every other in-process service down with it -- the
	// same reasoning internal/api/server.go's withRecovery middleware
	// already applies to HTTP handlers.
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🛰️ [StreamProxy] PANIC in run(): %v", r)
		}
	}()

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
	// Mark controlPort seen before opening it directly: the "control" SETUP
	// response also carries "server_port=<controlPort>" (handle_setup replies
	// with the same Transport header for every stream type), so scanServerPorts
	// would otherwise call ensureUDPRelay(controlPort) again once a session's
	// RTSP SETUP is snooped -- seenUDP wasn't set by this direct call, so that
	// second call always re-dialed ListenPacket on the same port and failed
	// with "already in use" (harmless on its own, but see initTailscale's
	// comment on startSunshineTSNetForwarding for why a bind collision here
	// isn't always harmless).
	p.mu.Lock()
	p.seenUDP[controlPort] = true
	p.mu.Unlock()
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

	// Deliberately *unconnected* (ListenUDP, not Dial): gamestream-server
	// closes and rebinds its own local video/audio ping socket on every
	// single session retry (each "starting video pipeline" cycle logs its
	// own fresh "waiting for client video ping"), sometimes several times a
	// minute while a client repeatedly retries a stalled connection. A
	// *connected* UDP socket (`net.Dial`, what this used to be) latches
	// onto that first bind and, once Windows delivers a delayed ICMP
	// port-unreachable for a send that landed in the gap between the old
	// socket's close and the new one's bind, keeps surfacing that same
	// stale error on every future call -- confirmed live: after the first
	// successful session on a freshly (re)started gamestream-server, every
	// later retry within that same process's lifetime sat forever in
	// "waiting for client video ping" even though `[v1] Accept: UDP{...}
	// ok` in the tsnet log showed the client's ping packets still arriving.
	// An unconnected socket has no single latched peer for the kernel to
	// blame a stale ICMP on -- each send explicitly re-targets
	// `backendAddr` fresh, so a rebind on gamestream-server's side is
	// invisible here.
	backendAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		logrus.Errorf("🛰️ [StreamProxy] resolve local udp :%d: %v", port, err)
		return
	}
	local, err := net.ListenUDP("udp4", nil)
	if err != nil {
		logrus.Errorf("🛰️ [StreamProxy] listen local udp :%d: %v", port, err)
		return
	}
	if !p.addListener(local) {
		local.Close()
		return
	}

	// Match the client's moonlight_tsnet_proxy.go buffer sizing (see its
	// createServerUDPProxy): a Sunshine keyframe lands as a burst of
	// 200-600+ RTP packets sent back-to-back in single-digit milliseconds
	// (see pipeline's "packetize_us"/"send_us" timing). The OS default UDP
	// receive buffer (~208KB on Linux) can't absorb that burst, so without
	// this the kernel silently drops the tail of every keyframe here —
	// before it ever reaches tsnet — which is exactly the "received < N
	// needed" FEC-unrecoverable pattern seen client-side. Widening the
	// buffer alone doesn't fix it though; see the read/write split below.
	local.SetReadBuffer(4194304)
	local.SetWriteBuffer(4194304)
	if sb, ok := pc.(interface{ SetReadBuffer(int) error }); ok {
		sb.SetReadBuffer(4194304)
	}
	if sb, ok := pc.(interface{ SetWriteBuffer(int) error }); ok {
		sb.SetWriteBuffer(4194304)
	}

	// remoteAddr is written on every inbound client packet and read on every
	// outbound one -- both video-direction hot paths (see pumpBurstTolerant
	// below). A sync.Mutex lock/unlock pair per packet is cheap in absolute
	// terms but still a needless syscall-adjacent op on a path processing
	// hundreds of packets in single-digit milliseconds during a keyframe
	// burst; atomic.Pointer gives the same one-writer/many-readers safety
	// with a lock-free read.
	var remoteAddr atomic.Pointer[net.Addr]

	// remote (tsnet) → local (Sunshine): client pings and RTCP feedback,
	// low volume and never bursty — a plain synchronous relay is fine.
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
				remoteAddr.Store(&addr)
				local.WriteToUDP(buf[:n], backendAddr) //nolint:errcheck
			}
		}
	}()

	// local (Sunshine) → remote (tsnet): the video/audio RTP direction,
	// where keyframe bursts happen. A single goroutine doing
	// read-then-synchronously-push-through-tsnet couples the read rate to
	// however long pc.WriteTo takes (userspace WireGuard encryption +
	// netstack queueing, not a cheap syscall) — during a burst that stalls
	// draining the kernel socket and the widened buffer above just delays
	// the same overflow instead of preventing it. Splitting into a fast
	// read loop feeding a buffered channel, drained by a separate write
	// goroutine, decouples the two so a slow tsnet write never blocks the
	// socket read — same shape as the client's s2cQueue for its mirror-image
	// bottleneck (its downstream 127.0.0.1 loopback to the video decoder).
	const relayQueueDepth = 4096
	go pumpBurstTolerant(func(buf []byte) (int, error) {
		for {
			n, _, err := local.ReadFromUDP(buf)
			if err == nil {
				return n, nil
			}
			select {
			case <-p.ctx.Done():
				return 0, err
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return 0, err
			}
			// ECONNREFUSED from an ICMP port-unreachable while Sunshine's
			// own RTP socket for this session is still binding is a
			// startup race, not a permanent failure — keep retrying (same
			// fix as the client's tsnet proxy). On an unconnected socket
			// this practically never fires (see the comment on `local`'s
			// creation above) but costs nothing to keep.
		}
	}, func(packet []byte) {
		if ra := remoteAddr.Load(); ra != nil {
			pc.WriteTo(packet, *ra) //nolint:errcheck
		}
	}, relayQueueDepth, func(dropped int) {
		if dropped <= 5 || dropped%200 == 0 {
			logrus.Warnf("🛰️ [StreamProxy] udp :%d DROPPED packet, relayQueue full (#%d)", port, dropped)
		}
	})
}

// pumpBurstTolerant decouples a synchronous read loop from a synchronous
// write loop via a bounded channel, so a burst of reads arriving faster than
// write can drain doesn't block read — which would otherwise let the OS-level
// receive buffer upstream of read overflow and silently drop datagrams before
// this code ever sees them. See startUDPRelay's "local (Sunshine) → remote
// (tsnet)" comment for the concrete keyframe-burst failure this fixes.
//
// read is called repeatedly, filling buf and returning the byte count; it
// must handle its own transient-error retries internally (via its own loop)
// and only return a non-nil error to signal the pump should stop. Each
// returned packet is hard-copied out of buf (via a rotating slab, to avoid
// a per-packet allocation) and queued for write on a separate goroutine.
//
// When the queue is full (write can't keep up), the packet is dropped and
// onDrop is invoked with the running drop count instead of blocking read —
// for a live RTP stream, timeliness for packets that do get through matters
// far more than never dropping one.
//
// Returns once read returns a non-nil error, after the write goroutine has
// drained and exited.
func pumpBurstTolerant(read func(buf []byte) (int, error), write func(packet []byte), queueDepth int, onDrop func(dropped int)) {
	queue := make(chan []byte, queueDepth)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for packet := range queue {
			write(packet)
		}
	}()

	const slabSize = 1 << 20 // 1MB, sized well above any single RTP datagram
	slab := make([]byte, slabSize)
	slabOffset := 0
	dropped := 0
	for {
		if slabOffset+2048 > slabSize {
			slab = make([]byte, slabSize)
			slabOffset = 0
		}
		buf := slab[slabOffset : slabOffset+2048]
		n, err := read(buf)
		if err != nil {
			break
		}
		if n == 0 {
			continue
		}
		packet := buf[:n]
		slabOffset += n
		select {
		case queue <- packet:
		default:
			dropped++
			if onDrop != nil {
				onDrop(dropped)
			}
		}
	}
	close(queue)
	<-writerDone
}
