package video

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kbinani/screenshot"
	"usbridge_agent/internal/api"
	"usbridge_agent/internal/config"
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
		TailnetIPv4(context.Context) (string, error)
		IsUserspace(context.Context) (bool, error)
	}
	mu            sync.Mutex
	proc          *runningProcess
	info          api.VideoStartRequest
	sessionCodec  string
	stopRequested bool
	startTraceSeq atomic.Uint64
	lastFrameAt   atomic.Pointer[time.Time]
	ctx           context.Context
	cancel        context.CancelFunc
}

func New(cfg config.Config, frp interface{ UpdateVideoVisitor(int) error }, ts interface {
	TailnetIPv4(context.Context) (string, error)
	IsUserspace(context.Context) (bool, error)
}) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		cfg:    cfg,
		frp:    frp,
		ts:     ts,
		ctx:    ctx,
		cancel: cancel,
	}
	go m.monitorVideoHealth(ctx)
	return m
}

func (m *Manager) Start(req api.VideoStartRequest) error {
	traceID, startedAt := m.beginStartTrace(req)
	m.mu.Lock()
	defer m.mu.Unlock()

	req = m.normalize(req)

	// Auto-detect native resolution to skip scaling
	if strings.HasPrefix(req.VideoDevice, "display:") {
		var idx int
		if _, err := fmt.Sscanf(req.VideoDevice, "display:%d", &idx); err == nil {
			if idx >= 0 && idx < screenshot.NumActiveDisplays() {
				bounds := screenshot.GetDisplayBounds(idx)
				if req.VideoWidth == bounds.Dx() && req.VideoHeight == bounds.Dy() {
					log.Printf("[video] requested resolution %dx%d matches display %d native size; disabling scaler", req.VideoWidth, req.VideoHeight, idx)
					req.VideoWidth = -1
					req.VideoHeight = -1
				}
			}
		}
	} else if req.VideoDevice == "desktop" || req.VideoDevice == "" {
		if screenshot.NumActiveDisplays() > 0 {
			bounds := screenshot.GetDisplayBounds(0)
			if req.VideoWidth == bounds.Dx() && req.VideoHeight == bounds.Dy() {
				log.Printf("[video] requested resolution %dx%d matches primary display native size; disabling scaler", req.VideoWidth, req.VideoHeight)
				req.VideoWidth = -1
				req.VideoHeight = -1
			}
		}
	}

	if m.proc != nil {
		if m.info.VideoWidth == req.VideoWidth &&
			m.info.VideoHeight == req.VideoHeight &&
			m.info.VideoFPS == req.VideoFPS &&
			m.info.ClientHost == req.ClientHost &&
			m.info.ClientPort == req.ClientPort &&
			m.info.VideoMode == req.VideoMode &&
			m.info.ShowMouse == req.ShowMouse {
			log.Println("ℹ️ Видео стриминг уже запущен с актуальными параметрами")
			return nil
		}

		log.Printf("⚠️ Параметры или цель стриминга изменились, перезапуск...")
		m.mu.Unlock()
		m.Stop()
		m.mu.Lock()
	}
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

	mode := captureModeForPlatform(m.cfg.VideoCapture)
	codecs := m.resolveCodecList()
	m.traceStep(traceID, startedAt, "session-plan", "capture=%s codecs=%s source_format=%s", mode, strings.Join(codecs, ","), sourceFormatForPlatform())

	var proc *runningProcess
	var lastErr error
	for _, codec := range codecs {
		m.traceStep(traceID, startedAt, "ffmpeg-start-begin", "mode=%s codec=%s target=%s:%d", mode, codec, req.ClientHost, req.ClientPort)
		proc, lastErr = m.startProcess(req, mode, codec)
		if lastErr == nil {
			break
		}
		m.traceStep(traceID, startedAt, "ffmpeg-codec-failed", "codec=%s err=%v", codec, lastErr)
	}
	if lastErr != nil {
		m.traceStep(traceID, startedAt, "ffmpeg-start-failed", "err=%v", lastErr)
		return lastErr
	}

	m.proc = proc
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
	m.stopRequested = true
	m.proc = nil
	m.sessionCodec = ""
	m.mu.Unlock()

	if proc != nil {
		log.Printf("[video] stop requested mode=%s codec=%s", proc.mode, proc.codec)
	}
	stopRunningProcess(proc, 3*time.Second)
	return nil
}

func (m *Manager) Close() error {
	m.cancel()
	return m.Stop()
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

func (m *Manager) listenForClientPunch(port int) {
	// Try to listen on the client's port, but don't fail if we can't
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		// This is expected if FFmpeg or another process already took the port
		log.Printf("[video/probe] info: local port :%d busy, punch packet will be handled by system stack", port)
		return
	}
	defer conn.Close()

	log.Printf("[video/probe] listening for client PUNCH on :%d...", port)
	buf := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, remoteAddr, err := conn.ReadFrom(buf)
	if err == nil {
		log.Printf("🎯 [video/probe] GOT PUNCH from %v: %q", remoteAddr, string(buf[:n]))
	}
}

func (m *Manager) streamFFmpegStats(r io.Reader, mode string) {
	scanner := bufio.NewScanner(r)
	lastLog := time.Now()
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		now := time.Now()
		m.lastFrameAt.Store(&now)

		if strings.Contains(line, "bitrate=") || strings.Contains(line, "fps=") || strings.Contains(line, "frame=") {
			now := time.Now()
			m.lastFrameAt.Store(&now)

			if time.Since(lastLog) < 5*time.Second {
				continue
			}
			lastLog = now

			parts := strings.Fields(line)
			var out []string
			for _, p := range parts {
				if strings.HasPrefix(p, "frame=") || strings.HasPrefix(p, "fps=") || strings.HasPrefix(p, "bitrate=") || strings.HasPrefix(p, "speed=") {
					out = append(out, p)
				}
			}
			if len(out) > 0 {
				log.Printf("📊 [video/%s/stats] %s", mode, strings.Join(out, " "))
				continue
			}
		}
		// Log EVERYTHING else from stderr to catch errors like "Configuration failed"
		if !strings.Contains(line, "frame=") {
			log.Printf("[video/%s/stderr] %s", mode, line)
		}
	}
}

func (m *Manager) monitorVideoHealth(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			proc := m.proc
			m.mu.Unlock()

			if proc == nil {
				continue
			}

			lastFrame := m.lastFrameAt.Load()
			if lastFrame != nil && time.Since(*lastFrame) > 15*time.Second {
				log.Printf("🚨 [video] STALL DETECTED! No frames from FFmpeg for %v", time.Since(*lastFrame).Round(time.Second))
				// We don't automatically restart here yet, but we log it as requested.
			}
		}
	}
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
	req.ClientHost = strings.TrimSpace(req.ClientHost)
	if req.ClientHost == "" {
		req.ClientHost = "127.0.0.1"
	}
	if req.ClientHost == "127.0.0.1" || req.ClientPort <= 0 {
		req.ClientPort = m.cfg.VideoUDPPort
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

func (m *Manager) startProcess(req api.VideoStartRequest, mode, codec string) (*runningProcess, error) {
	args := m.buildArgs(req, mode, codec)
	hardware := !strings.EqualFold(codec, "libx264")
	
	log.Printf("[video] starting mode=%s codec=%s hardware=%t target=%s:%d fps=%d size=%dx%d bitrate=%s", mode, codec, hardware, req.ClientHost, req.ClientPort, req.VideoFPS, req.VideoWidth, req.VideoHeight, req.VideoBitrate)

	var cmd *exec.Cmd
	if len(args) > 0 && args[0] == "gst-launch-1.0" {
		log.Printf("[video] gst-launch args mode=%s :: %s", mode, strings.Join(args, " "))
		cmd = exec.Command(args[0], args[1:]...)
	} else if len(args) > 0 && args[0] == "bash" {
		log.Printf("[video] custom shell wrapper mode=%s :: %s", mode, strings.Join(args, " "))
		cmd = exec.Command(args[0], args[1:]...)
	} else {
		log.Printf("[video] ffmpeg args mode=%s codec=%s :: %s %s", mode, codec, m.cfg.FFmpegPath, strings.Join(args, " "))
		cmd = exec.Command(m.cfg.FFmpegPath, args...)
	}

	attachPipeWireFD(cmd)

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
	go m.streamFFmpegStats(stderr, mode)

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
	platformArgs := buildPlatformArgs(m.cfg, req, mode, codec)
	if len(platformArgs) > 0 && (platformArgs[0] == "bash" || platformArgs[0] == "gst-launch-1.0") {
		return platformArgs
	}
	return append([]string{"-nostdin"}, platformArgs...)
}

// resolveCodecList returns an ordered list of codecs to try, starting from the best candidate.
// The caller should attempt each codec in sequence and stop on first success.
func (m *Manager) resolveCodecList() []string {
	configured := strings.TrimSpace(m.cfg.VideoCodec)
	if configured == "" {
		configured = "auto"
	}
	if !strings.EqualFold(configured, "auto") {
		return []string{configured}
	}
	if codec := platformAutoCodec(m.cfg.FFmpegPath); codec != "" {
		return []string{codec}
	}

	available := detectAvailableCodecs(m.cfg.FFmpegPath)
	order := preferredCodecsForAdapters(detectPlatformVideoAdapters())
	if len(order) == 0 {
		order = platformCodecFallbacks()
	}

	var list []string
	for _, codec := range appendUnique(append(order, "libx264")...) {
		if len(available) == 0 || codec == "libx264" {
			list = append(list, codec)
		} else if _, ok := available[codec]; ok {
			list = append(list, codec)
		}
	}
	if len(list) == 0 {
		return []string{"libx264"}
	}
	return list
}

func (m *Manager) watchProcess(proc *runningProcess, req api.VideoStartRequest) {
	err := <-proc.done
	runtime := time.Since(proc.startedAt).Round(time.Millisecond)
	
	m.mu.Lock()
	current := m.proc != nil && m.proc.cmd == proc.cmd
	stopping := m.stopRequested
	if current {
		m.proc = nil
		m.sessionCodec = ""
	}
	m.mu.Unlock()

	if stopping {
		log.Printf("🛑 [video] streaming stopped by request mode=%s codec=%s runtime=%s", proc.mode, proc.codec, runtime)
	} else if err != nil {
		log.Printf("❌ [video] process crashed! mode=%s codec=%s runtime=%s err=%v", proc.mode, proc.codec, runtime, err)
	} else {
		log.Printf("⚠️ [video] process exited unexpectedly mode=%s codec=%s runtime=%s", proc.mode, proc.codec, runtime)
	}
	
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

func captureModeForPlatform(configured string) string {
	modes := captureModesForPlatform(configured)
	if len(modes) == 0 {
		return ""
	}
	return modes[0]
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
	scale := ""
	if width > 0 && height > 0 {
		scale = fmt.Sprintf("scale=%d:%d,", width, height)
	}

	if prefersNV12(codec) {
		return fmt.Sprintf("%sformat=nv12", scale)
	}
	return fmt.Sprintf("%sformat=yuv420p", scale)
}

func dxgiFilter(width, height int, codec string) string {
	scale := ""
	if width > 0 && height > 0 {
		scale = fmt.Sprintf("format=bgra,scale=%d:%d,", width, height)
	}

	if prefersNV12(codec) {
		return fmt.Sprintf("hwdownload,%sformat=nv12", scale)
	}
	return fmt.Sprintf("hwdownload,%sformat=yuv420p", scale)
}

func prefersNV12(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264_nvenc", "h264_qsv", "h264_vaapi", "h264_amf", "h264_videotoolbox":
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
	configureFFmpegCommand(cmd)
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[video] codec probe skipped: %v", err)
		return nil
	}
	available := make(map[string]struct{}, 4)
	for _, codec := range []string{"h264_videotoolbox", "h264_amf", "h264_nvenc", "h264_qsv", "h264_vaapi", "libx264"} {
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
