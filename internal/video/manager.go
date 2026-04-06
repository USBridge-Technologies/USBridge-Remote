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
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan error
	mode   string
	codec  string
}

type Manager struct {
	cfg            config.Config
	mu             sync.Mutex
	proc           *runningProcess
	info           api.VideoStartRequest
	preferredCodec string
}

func New(cfg config.Config) *Manager { return &Manager{cfg: cfg} }

func (m *Manager) Start(req api.VideoStartRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.proc != nil {
		return fmt.Errorf("video already running")
	}

	req = m.normalize(req)
	proc, err := m.startWithFallback(req)
	if err != nil {
		return err
	}

	m.proc = proc
	m.info = req

	go func(expected *exec.Cmd, mode string, codec string) {
		err := <-proc.done
		log.Printf("[video] process stopped mode=%s codec=%s err=%v", mode, codec, err)

		m.mu.Lock()
		defer m.mu.Unlock()
		if m.proc != nil && m.proc.cmd == expected {
			m.proc = nil
		}
	}(proc.cmd, proc.mode, proc.codec)

	return nil
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.proc != nil && m.proc.cancel != nil {
		log.Printf("[video] stop requested mode=%s", m.proc.mode)
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

func (m *Manager) startWithFallback(req api.VideoStartRequest) (*runningProcess, error) {
	modes := []string{strings.ToLower(m.cfg.VideoCapture)}
	if len(modes) == 0 || modes[0] == "" {
		modes[0] = "dxgi"
	}
	if modes[0] == "dxgi" {
		modes = append(modes, "gdigrab")
	}

	var lastErr error
	for _, mode := range modes {
		for _, codec := range m.candidateCodecs() {
			proc, err := m.startProcess(req, mode, codec)
			if err == nil {
				m.preferredCodec = codec
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
	case <-time.After(1500 * time.Millisecond):
		log.Printf("[video] started mode=%s codec=%s", mode, codec)
		return &runningProcess{cmd: cmd, cancel: cancel, done: done, mode: mode, codec: codec}, nil
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

func (m *Manager) candidateCodecs() []string {
	if preferred := strings.TrimSpace(m.preferredCodec); preferred != "" {
		return appendUnique(preferred, "h264_amf", "h264_nvenc", "h264_qsv", "libx264")
	}

	primary := strings.TrimSpace(m.cfg.VideoCodec)
	if primary == "" {
		primary = "auto"
	}
	switch strings.ToLower(primary) {
	case "auto":
		return []string{"h264_nvenc", "h264_qsv", "h264_amf", "libx264"}
	case "h264_nvenc", "h264_qsv", "h264_amf", "libx264":
		return appendUnique(primary, "libx264")
	default:
		return appendUnique(primary, "libx264")
	}
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
