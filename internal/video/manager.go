package video

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/config"

	"tailscale.com/tsnet"
)

type runningProcess struct {
	cmd       *exec.Cmd
	done      chan error
	mode      string
	codec     string
	startedAt time.Time
}

type Manager struct {
	cfg config.Config
	frp interface{ UpdateVideoVisitor(int) error }
	ts  interface {
		Server() (*tsnet.Server, error)
		TailnetIPv4(context.Context) (string, error)
	}
	mu            sync.Mutex
	proc          *runningProcess
	relay         *videoRelay
	info          api.VideoStartRequest
	sessionCodec  string
	stopRequested bool
	startTraceSeq atomic.Uint64
}

type videoRelay struct {
	cancel         context.CancelFunc
	localConn      net.PacketConn
	tailConn       net.Conn
	targetAddr     string
	localPort      int
	firstPktCh     chan struct{}
	firstOnce      sync.Once
	localProbeCh   chan struct{}
	localProbeOnce sync.Once
	packetCount    atomic.Uint64
	lastSource     atomic.Value
	lastPacketAt   atomic.Int64
}

const (
	videoRelayProbePayload      = "USBRIDGE_VIDEO_RELAY_PROBE_V1"
	videoRelayAckPayload        = "USBRIDGE_VIDEO_RELAY_ACK_V1"
	videoRelayLocalProbePayload = "USBRIDGE_VIDEO_LOCAL_RELAY_PROBE_V1"
)

func New(cfg config.Config, frp interface{ UpdateVideoVisitor(int) error }, ts interface {
	Server() (*tsnet.Server, error)
	TailnetIPv4(context.Context) (string, error)
}) *Manager {
	return &Manager{cfg: cfg, frp: frp, ts: ts}
}

func (m *Manager) Start(req api.VideoStartRequest) error {
	traceID, startedAt := m.beginStartTrace(req)
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.proc != nil {
		m.traceStep(traceID, startedAt, "reject-already-running", "existing process mode=%s codec=%s", m.proc.mode, m.proc.codec)
		return fmt.Errorf("video already running")
	}

	req = m.normalize(req)
	if err := m.validateStartRequest(req); err != nil {
		m.traceStep(traceID, startedAt, "invalid-request", "err=%v", err)
		return err
	}
	m.traceStep(traceID, startedAt, "normalized-request", "device=%s size=%dx%d fps=%d bitrate=%s host=%s port=%d", req.VideoDevice, req.VideoWidth, req.VideoHeight, req.VideoFPS, req.VideoBitrate, req.ClientHost, req.ClientPort)
	if m.frp != nil {
		m.traceStep(traceID, startedAt, "update-frp-visitor-begin", "client_port=%d", req.ClientPort)
		if err := m.frp.UpdateVideoVisitor(req.ClientPort); err != nil {
			m.traceStep(traceID, startedAt, "update-frp-visitor-failed", "err=%v", err)
			return fmt.Errorf("update video visitor port: %w", err)
		}
		m.traceStep(traceID, startedAt, "update-frp-visitor-ok", "client_port=%d", req.ClientPort)
	}
	relay, processReq, err := m.startRelayLocked(req)
	if err != nil {
		m.traceStep(traceID, startedAt, "relay-setup-failed", "err=%v", err)
		return err
	}
	if relay != nil {
		m.traceStep(traceID, startedAt, "relay-setup-ok", "local=127.0.0.1:%d target=%s", relay.localPort, relay.targetAddr)
		if err := probeLocalRelay(relay, 1500*time.Millisecond); err != nil {
			stopVideoRelay(relay)
			m.traceStep(traceID, startedAt, "relay-local-probe-failed", "err=%v", err)
			return err
		}
	}

	mode := captureModeForPlatform(m.cfg.VideoCapture)
	codec := m.resolveCodec()
	m.traceStep(traceID, startedAt, "session-plan", "capture=%s codec=%s source_format=%s", mode, codec, sourceFormatForPlatform())
	m.traceStep(traceID, startedAt, "ffmpeg-start-begin", "mode=%s codec=%s target=%s:%d", mode, codec, processReq.ClientHost, processReq.ClientPort)
	proc, err := m.startProcess(processReq, mode, codec)
	if err != nil {
		stopVideoRelay(relay)
		m.traceStep(traceID, startedAt, "ffmpeg-start-failed", "err=%v", err)
		return err
	}
	if err := waitForFirstRelayPacket(relay, 12*time.Second); err != nil {
		stopRunningProcess(proc, 1500*time.Millisecond)
		stopVideoRelay(relay)
		m.traceStep(traceID, startedAt, "first-rtp-timeout", "mode=%s codec=%s err=%v", proc.mode, proc.codec, err)
		return fmt.Errorf("video session produced no RTP packets: %w", err)
	}

	m.proc = proc
	m.relay = relay
	m.info = req
	m.stopRequested = false
	m.sessionCodec = proc.codec
	m.traceStep(traceID, startedAt, "running-committed", "mode=%s codec=%s", proc.mode, proc.codec)

	go m.watchProcess(proc, req)

	return nil
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	proc := m.proc
	relay := m.relay
	m.stopRequested = true
	m.proc = nil
	m.relay = nil
	m.sessionCodec = ""
	m.mu.Unlock()

	if proc != nil {
		log.Printf("[video] stop requested mode=%s codec=%s", proc.mode, proc.codec)
	}
	stopRunningProcess(proc, 3*time.Second)
	stopVideoRelay(relay)
	return nil
}

func (m *Manager) beginStartTrace(req api.VideoStartRequest) (uint64, time.Time) {
	traceID := m.startTraceSeq.Add(1)
	startedAt := time.Now()
	log.Printf("[video/start #%d] begin client_trace=%s raw device=%s size=%dx%d fps=%d bitrate=%s mode=%s host=%s port=%d", traceID, req.TraceID, req.VideoDevice, req.VideoWidth, req.VideoHeight, req.VideoFPS, req.VideoBitrate, req.VideoMode, req.ClientHost, req.ClientPort)
	return traceID, startedAt
}

func (m *Manager) traceStep(traceID uint64, startedAt time.Time, step, format string, args ...any) {
	msg := ""
	if strings.TrimSpace(format) != "" {
		msg = fmt.Sprintf(format, args...)
	}
	if msg == "" {
		log.Printf("[video/start #%d] step=%s elapsed=%s", traceID, step, time.Since(startedAt).Round(time.Millisecond))
		return
	}
	log.Printf("[video/start #%d] step=%s elapsed=%s %s", traceID, step, time.Since(startedAt).Round(time.Millisecond), msg)
}

func (m *Manager) Info() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()

	return map[string]interface{}{
		"enabled":             true,
		"device":              "desktop",
		"width":               max(m.info.VideoWidth, m.cfg.VideoWidth),
		"height":              max(m.info.VideoHeight, m.cfg.VideoHeight),
		"fps":                 max(m.info.VideoFPS, m.cfg.VideoFPS),
		"quality":             80,
		"bitrate":             firstNonEmpty(m.info.VideoBitrate, m.cfg.VideoBitrate),
		"mode":                "h264",
		"transport":           "rtp",
		"encoding":            "h264",
		"source_format":       sourceFormatForPlatform(),
		"server_decodes_jpeg": true,
		"streaming":           m.proc != nil,
		"udp_port":            max(m.info.ClientPort, m.cfg.VideoUDPPort),
	}
}

func (m *Manager) normalize(req api.VideoStartRequest) api.VideoStartRequest {
	if req.VideoWidth == 0 {
		req.VideoWidth = m.cfg.VideoWidth
	}
	if req.VideoHeight == 0 {
		req.VideoHeight = m.cfg.VideoHeight
	}
	if req.VideoFPS == 0 {
		req.VideoFPS = m.cfg.VideoFPS
	}
	if req.VideoBitrate == "" {
		req.VideoBitrate = m.cfg.VideoBitrate
	}
	if req.ClientPort == 0 {
		req.ClientPort = m.cfg.VideoUDPPort
	}
	req.ClientHost = strings.TrimSpace(req.ClientHost)
	if req.ClientHost == "" {
		req.ClientHost = "127.0.0.1"
	}
	req.VideoMode = strings.TrimSpace(req.VideoMode)
	if req.VideoMode == "" {
		req.VideoMode = "h264"
	}
	return req
}

func (m *Manager) validateStartRequest(req api.VideoStartRequest) error {
	if req.ClientPort <= 0 {
		return fmt.Errorf("client UDP port is required")
	}
	if req.VideoMode != "" && !strings.EqualFold(req.VideoMode, "h264") {
		return fmt.Errorf("unsupported video mode %q: only h264 is supported", req.VideoMode)
	}
	return nil
}

func (m *Manager) startRelayLocked(req api.VideoStartRequest) (*videoRelay, api.VideoStartRequest, error) {
	if !shouldUseTailscaleVideoRelay(req.ClientHost) || m.ts == nil {
		return nil, req, nil
	}

	server, err := m.ts.Server()
	if err != nil {
		return nil, req, fmt.Errorf("tailscale server unavailable for video relay: %w", err)
	}
	localConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		return nil, req, fmt.Errorf("video relay local listen failed: %w", err)
	}
	localUDPAddr, ok := localConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		_ = localConn.Close()
		return nil, req, fmt.Errorf("video relay local addr type: %T", localConn.LocalAddr())
	}
	targetAddr := net.JoinHostPort(req.ClientHost, fmt.Sprintf("%d", req.ClientPort))
	tailConn, err := server.Dial(context.Background(), "udp4", targetAddr)
	if err != nil {
		_ = localConn.Close()
		return nil, req, fmt.Errorf("video relay tailscale dial failed: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	relay := &videoRelay{
		cancel:       cancel,
		localConn:    localConn,
		tailConn:     tailConn,
		targetAddr:   targetAddr,
		localPort:    localUDPAddr.Port,
		firstPktCh:   make(chan struct{}),
		localProbeCh: make(chan struct{}),
	}
	if err := probeVideoRelay(tailConn, targetAddr, 3, 1200*time.Millisecond); err != nil {
		cancel()
		_ = localConn.Close()
		_ = tailConn.Close()
		return nil, req, err
	}
	req.ClientHost = "127.0.0.1"
	req.ClientPort = localUDPAddr.Port
	log.Printf("[video] tailscale relay enabled local=127.0.0.1:%d target=%s", relay.localPort, targetAddr)
	go runVideoRelay(ctx, relay)
	return relay, req, nil
}

func (m *Manager) startProcess(req api.VideoStartRequest, mode, codec string) (*runningProcess, error) {
	args := m.buildArgs(req, mode, codec)
	hardware := !strings.EqualFold(codec, "libx264")
	log.Printf("[video] starting mode=%s codec=%s hardware=%t target=%s:%d fps=%d size=%dx%d bitrate=%s", mode, codec, hardware, req.ClientHost, req.ClientPort, req.VideoFPS, req.VideoWidth, req.VideoHeight, req.VideoBitrate)
	log.Printf("[video] ffmpeg args mode=%s codec=%s :: %s %s", mode, codec, m.cfg.FFmpegPath, strings.Join(args, " "))

	cmd := exec.Command(m.cfg.FFmpegPath, args...)
	configureFFmpegCommand(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go streamLogs(stdout, mode, "stdout")
	go streamLogs(stderr, mode, "stderr")

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return nil, fmt.Errorf("ffmpeg exited early: %w", err)
	case <-time.After(3 * time.Second):
		log.Printf("[video] started mode=%s codec=%s hardware=%t pid=%d", mode, codec, hardware, cmd.Process.Pid)
		return &runningProcess{cmd: cmd, done: done, mode: mode, codec: codec, startedAt: time.Now()}, nil
	}
}

func (m *Manager) buildArgs(req api.VideoStartRequest, mode, codec string) []string {
	return append([]string{"-nostdin"}, buildPlatformArgs(m.cfg, req, mode, codec)...)
}

func (m *Manager) resolveCodec() string {
	configured := strings.TrimSpace(m.cfg.VideoCodec)
	if configured == "" {
		configured = "auto"
	}
	if !strings.EqualFold(configured, "auto") {
		return configured
	}
	if codec := platformAutoCodec(m.cfg.FFmpegPath); codec != "" {
		return codec
	}

	available := detectAvailableCodecs(m.cfg.FFmpegPath)
	order := preferredCodecsForAdapters(detectPlatformVideoAdapters())
	if len(order) == 0 {
		order = platformCodecFallbacks()
	}
	for _, codec := range appendUnique(append(order, "libx264")...) {
		if len(available) == 0 || codec == "libx264" {
			return codec
		}
		if _, ok := available[codec]; ok {
			return codec
		}
	}
	return "libx264"
}

func (m *Manager) watchProcess(proc *runningProcess, req api.VideoStartRequest) {
	err := <-proc.done
	runtime := time.Since(proc.startedAt).Round(time.Millisecond)
	log.Printf("[video] process stopped mode=%s codec=%s runtime=%s err=%v", proc.mode, proc.codec, runtime, err)

	m.mu.Lock()
	current := m.proc != nil && m.proc.cmd == proc.cmd
	stopping := m.stopRequested
	if current {
		m.proc = nil
		stopVideoRelay(m.relay)
		m.relay = nil
		m.sessionCodec = ""
	}
	m.mu.Unlock()
	if !current || stopping {
		return
	}
	log.Printf("[video] session ended and is left stopped; automatic fallback/restart is disabled")
}

func stopRunningProcess(proc *runningProcess, timeout time.Duration) {
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return
	}
	if err := terminateFFmpegProcess(proc.cmd); err != nil {
		log.Printf("[video] terminate failed mode=%s codec=%s pid=%d err=%v", proc.mode, proc.codec, proc.cmd.Process.Pid, err)
	}
	select {
	case <-proc.done:
	case <-time.After(timeout):
		log.Printf("[video] process wait timeout mode=%s codec=%s pid=%d timeout=%s", proc.mode, proc.codec, proc.cmd.Process.Pid, timeout)
		if err := proc.cmd.Process.Kill(); err != nil {
			log.Printf("[video] force kill failed mode=%s codec=%s pid=%d err=%v", proc.mode, proc.codec, proc.cmd.Process.Pid, err)
		}
		select {
		case <-proc.done:
		case <-time.After(1500 * time.Millisecond):
			log.Printf("[video] process still running after force kill mode=%s codec=%s pid=%d", proc.mode, proc.codec, proc.cmd.Process.Pid)
		}
	}
}

func shouldUseTailscaleVideoRelay(host string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return false
	}
	if addr.Is4() {
		// Tailscale IPv4 range.
		return netip.MustParsePrefix("100.64.0.0/10").Contains(addr)
	}
	return addr.Is6()
}

func captureModeForPlatform(configured string) string {
	modes := captureModesForPlatform(configured)
	if len(modes) == 0 {
		return ""
	}
	return modes[0]
}

func runVideoRelay(ctx context.Context, relay *videoRelay) {
	defer stopVideoRelay(relay)

	log.Printf("[video] tailscale relay goroutine started local=127.0.0.1:%d target=%s", relay.localPort, relay.targetAddr)
	buf := make([]byte, 64*1024)
	firstPacket := true
	packetCount := 0
	time.AfterFunc(3*time.Second, func() {
		if packetCount == 0 {
			log.Printf("[video] tailscale relay has not received local RTP packets yet on 127.0.0.1:%d", relay.localPort)
		}
	})
	for {
		_ = relay.localConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, src, err := relay.localConn.ReadFrom(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			log.Printf("[video] tailscale relay read failed: %v", err)
			return
		}
		if n == 0 {
			continue
		}
		payload := string(buf[:n])
		if payload == videoRelayLocalProbePayload {
			relay.localProbeOnce.Do(func() {
				close(relay.localProbeCh)
			})
			continue
		}
		packetCount++
		relay.packetCount.Add(1)
		relay.lastSource.Store(fmt.Sprintf("%v bytes=%d", src, n))
		relay.lastPacketAt.Store(time.Now().UnixNano())
		if firstPacket {
			firstPacket = false
			relay.firstOnce.Do(func() {
				close(relay.firstPktCh)
			})
			log.Printf("[video] tailscale relay forwarding first RTP packet from %v to %s", src, relay.targetAddr)
		}
		if _, err := relay.tailConn.Write(buf[:n]); err != nil {
			log.Printf("[video] tailscale relay write failed: %v", err)
			return
		}
		if packetCount == 1 || packetCount%300 == 0 {
			log.Printf("[video] tailscale relay forwarded packets=%d local=127.0.0.1:%d target=%s last_source=%s", packetCount, relay.localPort, relay.targetAddr, relayLastSource(relay))
		}
	}
}

func stopVideoRelay(relay *videoRelay) {
	if relay == nil {
		return
	}
	if relay.cancel != nil {
		relay.cancel()
	}
	if relay.localConn != nil {
		_ = relay.localConn.Close()
	}
	if relay.tailConn != nil {
		_ = relay.tailConn.Close()
	}
}

func waitForFirstRelayPacket(relay *videoRelay, timeout time.Duration) error {
	if relay == nil {
		return nil
	}
	select {
	case <-relay.firstPktCh:
		log.Printf("[video] tailscale relay confirmed first RTP packet on 127.0.0.1:%d", relay.localPort)
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("ffmpeg produced no RTP packets for tailscale relay on 127.0.0.1:%d within %s (packets=%d last_source=%s)", relay.localPort, timeout, relay.packetCount.Load(), relayLastSource(relay))
	}
}

func probeLocalRelay(relay *videoRelay, timeout time.Duration) error {
	if relay == nil || relay.localConn == nil {
		return nil
	}
	target, err := net.ResolveUDPAddr("udp4", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", relay.localPort)))
	if err != nil {
		return fmt.Errorf("local relay probe resolve: %w", err)
	}
	conn, err := net.DialUDP("udp4", nil, target)
	if err != nil {
		return fmt.Errorf("local relay probe dial: %w", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(videoRelayLocalProbePayload)); err != nil {
		return fmt.Errorf("local relay probe write: %w", err)
	}
	select {
	case <-relay.localProbeCh:
		log.Printf("[video] local relay probe confirmed on 127.0.0.1:%d", relay.localPort)
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("local relay did not receive localhost UDP probe on 127.0.0.1:%d within %s", relay.localPort, timeout)
	}
}

func relayLastSource(relay *videoRelay) string {
	if relay == nil {
		return ""
	}
	value, _ := relay.lastSource.Load().(string)
	if value == "" {
		return "none"
	}
	return value
}

func probeVideoRelay(conn net.Conn, targetAddr string, attempts int, timeout time.Duration) error {
	if conn == nil {
		return fmt.Errorf("video relay probe failed: nil tailscale conn")
	}
	if attempts <= 0 {
		attempts = 1
	}
	buf := make([]byte, 256)
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		deadline := time.Now().Add(timeout)
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("video relay probe set deadline: %w", err)
		}
		if _, err := conn.Write([]byte(videoRelayProbePayload)); err != nil {
			lastErr = fmt.Errorf("video relay probe write to %s: %w", targetAddr, err)
			continue
		}
		n, err := conn.Read(buf)
		if err == nil && string(buf[:n]) == videoRelayAckPayload {
			_ = conn.SetDeadline(time.Time{})
			log.Printf("[video] tailscale relay probe acknowledged by %s on attempt %d", targetAddr, attempt)
			return nil
		}
		if err == nil {
			lastErr = fmt.Errorf("video relay probe unexpected reply from %s: %q", targetAddr, string(buf[:n]))
		} else {
			lastErr = fmt.Errorf("video relay probe timeout from %s: %w", targetAddr, err)
		}
	}
	_ = conn.SetDeadline(time.Time{})
	return lastErr
}

func codecArgs(codec string) []string {
	switch strings.ToLower(codec) {
	case "h264_videotoolbox":
		return []string{"-realtime", "true", "-profile:v", "baseline", "-pix_fmt", "nv12"}
	case "h264_nvenc":
		return []string{"-preset", "p1", "-tune", "ll", "-profile:v", "baseline", "-level", "3.2", "-rc", "cbr_ld_hq", "-pix_fmt", "nv12"}
	case "h264_qsv":
		return []string{"-preset", "veryfast", "-profile:v", "baseline", "-level", "3.2", "-look_ahead", "0", "-pix_fmt", "nv12"}
	case "h264_amf":
		return []string{
			"-usage", "ultralowlatency",
			"-quality", "speed",
			"-rc", "cbr",
			"-profile:v", "constrained_baseline",
			"-level", "3.2",
			"-coder", "cavlc",
			"-forced_idr", "1",
			"-aud", "1",
			"-header_spacing", "1",
			"-pix_fmt", "nv12",
		}
	default:
		return []string{"-preset", "ultrafast", "-tune", "zerolatency", "-profile:v", "baseline", "-level", "3.2", "-pix_fmt", "yuv420p"}
	}
}

func softwareFilter(width, height int, codec string) string {
	if prefersNV12(codec) {
		return fmt.Sprintf("scale=%d:%d,format=nv12", width, height)
	}
	return fmt.Sprintf("scale=%d:%d,format=yuv420p", width, height)
}

func dxgiFilter(width, height int, codec string) string {
	if prefersNV12(codec) {
		return fmt.Sprintf("hwdownload,format=bgra,scale=%d:%d,format=nv12", width, height)
	}
	return fmt.Sprintf("hwdownload,format=bgra,scale=%d:%d,format=yuv420p", width, height)
}

func prefersNV12(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264_nvenc", "h264_qsv", "h264_amf", "h264_videotoolbox":
		return true
	default:
		return false
	}
}

func appendUnique(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func detectAvailableCodecs(ffmpegPath string) map[string]struct{} {
	cmd := exec.Command(ffmpegPath, "-hide_banner", "-encoders")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[video] codec probe skipped: %v", err)
		return nil
	}
	available := make(map[string]struct{}, 4)
	for _, codec := range []string{"h264_videotoolbox", "h264_amf", "h264_nvenc", "h264_qsv", "libx264"} {
		if strings.Contains(string(output), codec) {
			available[codec] = struct{}{}
		}
	}
	return available
}

func containsAny(value string, needles ...string) bool {
	lower := strings.ToLower(value)
	for _, needle := range needles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func streamLogs(r io.Reader, mode, stream string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		log.Printf("[video/%s/%s] %s", mode, stream, line)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[video/%s/%s] pipe error: %v", mode, stream, err)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
