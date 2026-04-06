package video

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/config"
)

type runningProcess struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	done      chan error
	mode      string
	codec     string
	startedAt time.Time
}

type Manager struct {
	cfg            config.Config
	mu             sync.Mutex
	proc           *runningProcess
	info           api.VideoStartRequest
	preferredCodec string
	codecOrder     []string
	codecOrderOnce sync.Once
	stopRequested  bool
}

func New(cfg config.Config) *Manager { return &Manager{cfg: cfg} }

func (m *Manager) Start(req api.VideoStartRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.proc != nil {
		return fmt.Errorf("video already running")
	}

	req = m.normalize(req)
	proc, err := m.startWithFallback(req, nil)
	if err != nil {
		return err
	}

	m.proc = proc
	m.info = req
	m.stopRequested = false
	m.preferredCodec = proc.codec

	go m.watchProcess(proc, req)

	return nil
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.proc != nil && m.proc.cancel != nil {
		log.Printf("[video] stop requested mode=%s", m.proc.mode)
		m.stopRequested = true
		m.proc.cancel()
	}
	m.proc = nil
	return nil
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
		"source_format":       "BGRA",
		"server_decodes_jpeg": true,
		"streaming":           m.proc != nil,
		"udp_port":            m.cfg.VideoUDPPort,
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
	return req
}

func (m *Manager) startWithFallback(req api.VideoStartRequest, skipCodecs map[string]struct{}) (*runningProcess, error) {
	modes := []string{strings.ToLower(m.cfg.VideoCapture)}
	if len(modes) == 0 || modes[0] == "" {
		modes[0] = "dxgi"
	}
	if modes[0] == "dxgi" {
		modes = append(modes, "gdigrab")
	}

	var lastErr error
	for _, mode := range modes {
		for _, codec := range m.candidateCodecs(skipCodecs) {
			proc, err := m.startProcess(req, mode, codec)
			if err == nil {
				return proc, nil
			}
			lastErr = err
			log.Printf("[video] start failed mode=%s codec=%s err=%v", mode, codec, err)
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unable to start video capture")
	}
	return nil, lastErr
}

func (m *Manager) startProcess(req api.VideoStartRequest, mode, codec string) (*runningProcess, error) {
	ctx, cancel := context.WithCancel(context.Background())
	args := m.buildArgs(req, mode, codec)
	log.Printf("[video] starting mode=%s codec=%s target=127.0.0.1:%d fps=%d size=%dx%d bitrate=%s", mode, codec, m.cfg.VideoUDPPort, req.VideoFPS, req.VideoWidth, req.VideoHeight, req.VideoBitrate)
	log.Printf("[video] ffmpeg args mode=%s codec=%s :: %s %s", mode, codec, m.cfg.FFmpegPath, strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, m.cfg.FFmpegPath, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	go streamLogs(stderr, mode)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		cancel()
		return nil, fmt.Errorf("ffmpeg exited early: %w", err)
	case <-time.After(3 * time.Second):
		log.Printf("[video] started mode=%s codec=%s", mode, codec)
		return &runningProcess{cmd: cmd, cancel: cancel, done: done, mode: mode, codec: codec, startedAt: time.Now()}, nil
	}
}

func (m *Manager) buildArgs(req api.VideoStartRequest, mode, codec string) []string {
	input := []string{"-f", "gdigrab", "-framerate", fmt.Sprintf("%d", req.VideoFPS), "-draw_mouse", "1", "-i", "desktop"}
	videoFilter := softwareFilter(req.VideoWidth, req.VideoHeight, codec)
	if strings.EqualFold(mode, "dxgi") {
		input = []string{"-f", "lavfi", "-i", fmt.Sprintf("ddagrab=framerate=%d:draw_mouse=1", req.VideoFPS)}
		videoFilter = dxgiFilter(req.VideoWidth, req.VideoHeight, codec)
	}

	args := append(input,
		"-probesize", "32",
		"-analyzeduration", "0",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-fps_mode", "passthrough",
		"-an",
		"-vf", videoFilter,
		"-c:v", codec,
	)
	args = append(args, codecArgs(codec)...)
	args = append(args,
		"-sc_threshold", "0",
		"-g", fmt.Sprintf("%d", req.VideoFPS),
		"-keyint_min", fmt.Sprintf("%d", req.VideoFPS),
		"-bf", "0",
		"-bsf:v", "dump_extra=freq=keyframe",
		"-b:v", firstNonEmpty(req.VideoBitrate, m.cfg.VideoBitrate),
		"-maxrate", firstNonEmpty(req.VideoBitrate, m.cfg.VideoBitrate),
		"-bufsize", firstNonEmpty(req.VideoBitrate, m.cfg.VideoBitrate),
		"-payload_type", "96",
		"-f", "rtp",
		fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1200", m.cfg.VideoUDPPort),
	)
	return args
}

func (m *Manager) candidateCodecs(skipCodecs map[string]struct{}) []string {
	filter := func(values []string) []string {
		if len(skipCodecs) == 0 {
			return values
		}
		out := make([]string, 0, len(values))
		for _, value := range values {
			if _, skip := skipCodecs[strings.TrimSpace(value)]; skip {
				continue
			}
			out = append(out, value)
		}
		return out
	}

	if preferred := strings.TrimSpace(m.preferredCodec); preferred != "" {
		return filter(appendUnique(preferred, "h264_amf", "h264_nvenc", "h264_qsv", "libx264"))
	}

	primary := strings.TrimSpace(m.cfg.VideoCodec)
	if primary == "" {
		primary = "auto"
	}
	switch strings.ToLower(primary) {
	case "auto":
		return filter(m.autoCodecOrder())
	case "h264_nvenc", "h264_qsv", "h264_amf", "libx264":
		return filter(appendUnique(primary, "libx264"))
	default:
		return filter(appendUnique(primary, "libx264"))
	}
}

func (m *Manager) autoCodecOrder() []string {
	m.codecOrderOnce.Do(func() {
		available := detectAvailableCodecs(m.cfg.FFmpegPath)
		adapters := detectVideoAdapters()
		order := preferredCodecsForAdapters(adapters)
		if len(order) == 0 {
			order = []string{"h264_amf", "h264_nvenc", "h264_qsv", "libx264"}
		}
		if len(available) != 0 {
			filtered := make([]string, 0, len(order))
			for _, codec := range order {
				if _, ok := available[codec]; ok || codec == "libx264" {
					filtered = append(filtered, codec)
				}
			}
			order = filtered
		}
		m.codecOrder = appendUnique(append(order, "libx264")...)
		log.Printf("[video] auto codec order adapters=%v order=%v", adapters, m.codecOrder)
	})
	return append([]string(nil), m.codecOrder...)
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
	}
	if m.preferredCodec == proc.codec {
		m.preferredCodec = ""
	}
	m.mu.Unlock()

	if !current || stopping {
		return
	}

	log.Printf("[video] attempting automatic fallback after mode=%s codec=%s", proc.mode, proc.codec)
	skip := map[string]struct{}{proc.codec: {}}
	recovered, recoverErr := m.startWithFallback(req, skip)
	if recoverErr != nil {
		log.Printf("[video] fallback failed after mode=%s codec=%s err=%v", proc.mode, proc.codec, recoverErr)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proc != nil || m.stopRequested {
		if recovered.cancel != nil {
			recovered.cancel()
		}
		return
	}
	m.proc = recovered
	m.info = req
	m.preferredCodec = recovered.codec
	log.Printf("[video] recovered with mode=%s codec=%s", recovered.mode, recovered.codec)
	go m.watchProcess(recovered, req)
}

func codecArgs(codec string) []string {
	switch strings.ToLower(codec) {
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
	case "h264_nvenc", "h264_qsv", "h264_amf":
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
	for _, codec := range []string{"h264_amf", "h264_nvenc", "h264_qsv", "libx264"} {
		if strings.Contains(string(output), codec) {
			available[codec] = struct{}{}
		}
	}
	return available
}

func detectVideoAdapters() []string {
	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		"Get-CimInstance Win32_VideoController | Select-Object -ExpandProperty Name",
	)
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[video] adapter probe skipped: %v", err)
		return nil
	}
	lines := strings.Split(string(output), "\n")
	adapters := make([]string, 0, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		adapters = append(adapters, name)
	}
	return adapters
}

func preferredCodecsForAdapters(adapters []string) []string {
	order := make([]string, 0, 4)
	for _, adapter := range adapters {
		switch {
		case containsAny(adapter, "amd", "radeon"):
			order = append(order, "h264_amf")
		case containsAny(adapter, "nvidia", "geforce", "quadro", "rtx", "gtx"):
			order = append(order, "h264_nvenc")
		case containsAny(adapter, "intel", "iris", "uhd", "arc"):
			order = append(order, "h264_qsv")
		}
	}
	return appendUnique(append(order, "h264_amf", "h264_nvenc", "h264_qsv", "libx264")...)
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

func streamLogs(stderr io.Reader, mode string) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.Contains(line, "frame=") || strings.Contains(line, "fps=") {
			continue
		}
		log.Printf("[video/%s] %s", mode, line)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[video/%s] stderr error: %v", mode, err)
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
