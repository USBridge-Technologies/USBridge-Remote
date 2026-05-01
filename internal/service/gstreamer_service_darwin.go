//go:build darwin
// +build darwin

package service

import (
	"bufio"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"
)

// GStreamerService service for working with GStreamer on macOS via an external process
type GStreamerService struct {
	config    *models.AppConfig
	videoMode string

	// GStreamer process
	cmd                    *exec.Cmd
	stdout                 io.ReadCloser
	nativeFullscreenCmd    *exec.Cmd
	nativeFullscreenActive bool

	// State
	isConnected    bool
	isConnecting   bool
	isReconnecting bool

	// Auto-reconnect
	autoReconnect        bool
	reconnectAttempts    int
	maxReconnectAttempts int
	manualDisconnect     bool

	// Channel for non-blocking frame transfer
	frameChan chan videoFramePacket
	stop      *stopSignal // safe single closure

	// Flags for goroutine management
	frameProcessorRunning bool
	frameReaderRunning    bool
	monitorRunning        bool

	// Statistics
	lastFrameTime   time.Time
	lastFrameReport time.Time
	frameCount      int64
	framesDropped   int64
	latencyProfile  videoLatencyProfile

	// Frame dimensions (default 1280x720 for HD capture)
	width  int
	height int

	// Mutexes
	mutex sync.RWMutex

	// Callbacks
	onFrameReceived func(image.Image)
	onStateChanged  func(string)
	onError         func(error)
}

type darwinPipelineCandidate struct {
	name string
	args []string
}

type darwinSinkSpec struct {
	name string
	args []string
}

// NewGStreamerService creates a new GStreamer service for macOS
func NewGStreamerService(config *models.AppConfig) *GStreamerService {
	gs := &GStreamerService{
		config:                config,
		autoReconnect:         true,
		reconnectAttempts:     0,
		maxReconnectAttempts:  5,
		videoMode:             models.VideoModeH264,
		frameChan:             make(chan videoFramePacket, 1),
		stop:                  newStopSignal(),
		frameProcessorRunning: false,
		frameReaderRunning:    false,
		monitorRunning:        false,
		width:                 1920,
		height:                1080,
	}

	logrus.Info("✅ GStreamer service for macOS initialized (external process)")
	return gs
}

// getGStreamerEnv returns environment for gst-launch with plugin paths (applemedia/vtdec)
func (gs *GStreamerService) getGStreamerEnv() []string {
	env := os.Environ()

	for _, binDir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		if info, err := os.Stat(binDir); err == nil && info.IsDir() {
			env = appendOrPrependDarwinEnv(env, "PATH", binDir)
		}
	}

	for _, pluginPath := range []string{"/opt/homebrew/lib/gstreamer-1.0", "/usr/local/lib/gstreamer-1.0"} {
		if info, err := os.Stat(pluginPath); err == nil && info.IsDir() {
			env = appendOrPrependDarwinEnv(env, "GST_PLUGIN_PATH", pluginPath)
			env = appendOrPrependDarwinEnv(env, "GST_PLUGIN_SYSTEM_PATH", pluginPath)
			logrus.Infof("🔧 macOS: GST plugin dir=%s (vtdec/applemedia)", pluginPath)
		}
	}

	for _, scannerPath := range []string{
		"/opt/homebrew/libexec/gstreamer-1.0/gst-plugin-scanner",
		"/usr/local/libexec/gstreamer-1.0/gst-plugin-scanner",
	} {
		if info, err := os.Stat(scannerPath); err == nil && !info.IsDir() {
			env = appendOrPrependDarwinEnv(env, "GST_PLUGIN_SCANNER", scannerPath)
			logrus.Infof("🔧 macOS: GST plugin scanner=%s", scannerPath)
			break
		}
	}

	return env
}

func appendOrPrependDarwinEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			continue
		}
		current := strings.TrimPrefix(entry, prefix)
		if current == "" {
			env[i] = prefix + value
			return env
		}
		for _, part := range strings.Split(current, string(os.PathListSeparator)) {
			if part == value {
				return env
			}
		}
		env[i] = prefix + value + string(os.PathListSeparator) + current
		return env
	}
	return append(env, prefix+value)
}

func findDarwinGStreamerTool(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	candidates := []string{
		filepath.Join("/opt/homebrew/bin", name),
		filepath.Join("/usr/local/bin", name),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("%s not found in PATH, /opt/homebrew/bin, or /usr/local/bin", name)
}

// buildPipelineArgs forms pipeline arguments for RTP video (via QUIC/SUDP tunnel)
func (gs *GStreamerService) buildPipelineArgs(udpPort int) []string {
	bindHost := gs.darwinBindHost()
	if gs.videoMode == models.VideoModeJPEGRTP {
		return gs.buildPipelineArgsJPEG(udpPort)
	}

	// Low latency: choke jitterbuffer and allow dropping late frames.
	return []string{
		"-q",
		"udpsrc",
		fmt.Sprintf("address=%s", bindHost),
		fmt.Sprintf("port=%d", udpPort),
		"buffer-size=131072", // 128KB instead of 2MB — less buffering
		`caps=application/x-rtp,media=video,encoding-name=H264,payload=96`,
		"!",
		"rtpjitterbuffer",
		"latency=15",
		"faststart-min-packets=1",
		"drop-on-latency=true",
		"!",
		"rtph264depay",
		"!",
		"h264parse",
		"config-interval=-1",
		"!",
		"video/x-h264,stream-format=avc,alignment=au",
		"!",
		"vtdec",
		"!",
		"videoscale",
		"!",
		fmt.Sprintf("video/x-raw,width=%d,height=%d", gs.width, gs.height),
		"!",
		"videoconvert",
		"!",
		"video/x-raw,format=RGBA",
		"!",
		"fdsink",
		"fd=1",
		"sync=false",
	}
}

func (gs *GStreamerService) buildPipelineArgsJPEG(udpPort int) []string {
	candidates := gs.buildDarwinFrameSinkCandidates(udpPort, 15)
	if len(candidates) == 0 {
		return nil
	}
	return candidates[0].args
}

func (gs *GStreamerService) buildPipelineArgsJPEGCandidates(udpPort int) [][]string {
	candidates := gs.buildDarwinFrameSinkCandidates(udpPort, 15)
	result := make([][]string, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.args)
	}
	return result
}

func (gs *GStreamerService) darwinBindHost() string {
	if gs == nil || gs.config == nil {
		return "0.0.0.0"
	}
	host := strings.TrimSpace(gs.config.VideoBindHost)
	if host == "" || host == "127.0.0.1" {
		return "0.0.0.0"
	}
	return host
}

// GetBindHost returns the address where GStreamer listens for incoming UDP packets
func (gs *GStreamerService) GetBindHost() string {
	return gs.darwinBindHost()
}

func (gs *GStreamerService) buildDarwinRTPBaseArgs(udpPort int, latency int) []string {
	base := []string{
		"-q",
		"udpsrc",
		fmt.Sprintf("address=%s", gs.darwinBindHost()),
		fmt.Sprintf("port=%d", udpPort),
		"buffer-size=65536",
	}

	if gs.videoMode == models.VideoModeJPEGRTP {
		return append(base,
			`caps=application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26`,
			"!",
			"rtpjitterbuffer", fmt.Sprintf("latency=%d", latency), "faststart-min-packets=1", "drop-on-latency=true",
			"!",
			"rtpjpegdepay",
			"!",
			"jpegparse",
		)
	}

	return append(base,
		`caps=application/x-rtp,media=video,encoding-name=H264,payload=96`,
		"!",
		"rtpjitterbuffer", fmt.Sprintf("latency=%d", latency), "faststart-min-packets=1", "drop-on-latency=true",
		"!",
		"rtph264depay",
		"!",
		"h264parse", "config-interval=-1",
	)
}

func (gs *GStreamerService) darwinDecodeChains() [][]string {
	if gs.videoMode == models.VideoModeJPEGRTP {
		return [][]string{
			{"vtdec_hw"},
			{"vtdec"},
			{"jpegdec"},
			{"decodebin"},
		}
	}
	return [][]string{
		{"vtdec"},
		{"decodebin"},
	}
}

func (gs *GStreamerService) buildDarwinPipelineCandidates(base []string, decodeChains [][]string, middle []string, sinks []darwinSinkSpec) []darwinPipelineCandidate {
	candidates := make([]darwinPipelineCandidate, 0, len(decodeChains)*len(sinks))
	for _, chain := range decodeChains {
		for _, sink := range sinks {
			args := append([]string{}, base...)
			args = append(args, "!")
			args = append(args, chain...)
			args = append(args, "!")
			args = append(args, middle...)
			args = append(args, sink.args...)

			candidates = append(candidates, darwinPipelineCandidate{
				name: fmt.Sprintf("%s+%s", chain[0], sink.name),
				args: args,
			})
		}
	}
	return candidates
}

func (gs *GStreamerService) buildDarwinFrameSinkCandidates(udpPort int, latency int) []darwinPipelineCandidate {
	middle := []string{
		"videoscale",
		"!",
		fmt.Sprintf("video/x-raw,width=%d,height=%d", gs.width, gs.height),
		"!",
		"videoconvert",
		"!",
		"video/x-raw,format=RGBA",
		"!",
	}
	sinks := []darwinSinkSpec{
		{name: "fdsink", args: []string{"fdsink", "fd=1", "sync=false"}},
	}
	return gs.buildDarwinPipelineCandidates(gs.buildDarwinRTPBaseArgs(udpPort, latency), gs.darwinDecodeChains(), middle, sinks)
}

func (gs *GStreamerService) buildDarwinFullscreenCandidates(udpPort int, latency int) []darwinPipelineCandidate {
	middle := []string{"queue", "max-size-buffers=2", "leaky=downstream", "!"}
	sinks := []darwinSinkSpec{
		{name: "glimagesink", args: []string{"glimagesink", "fullscreen=true", "sync=false"}},
		{name: "osxvideosink", args: []string{"osxvideosink", "sync=false"}},
	}
	return gs.buildDarwinPipelineCandidates(gs.buildDarwinRTPBaseArgs(udpPort, latency), gs.darwinDecodeChains(), middle, sinks)
}

// buildPipelineArgsPipe — fdsrc for RTP H.264 from pipe (UDP relay with keepalive)
func (gs *GStreamerService) buildPipelineArgsPipe(fd int) []string {
	return []string{
		"-q",
		"fdsrc",
		fmt.Sprintf("fd=%d", fd),
		`caps=application/x-rtp,media=video,encoding-name=H264,payload=96`,
		"!",
		"rtpjitterbuffer",
		"latency=15",
		"faststart-min-packets=1",
		"drop-on-latency=true",
		"!",
		"rtph264depay",
		"!",
		"h264parse",
		"config-interval=-1",
		"!",
		"video/x-h264,stream-format=avc,alignment=au",
		"!",
		"vtdec",
		"!",
		"videoscale",
		"!",
		fmt.Sprintf("video/x-raw,width=%d,height=%d", gs.width, gs.height),
		"!",
		"videoconvert",
		"!",
		"video/x-raw,format=RGBA",
		"!",
		"fdsink",
		"fd=1",
		"sync=false",
	}
}

// ConnectToUDP connects to UDP H.264 stream (new protocol, minimal latency)
func (gs *GStreamerService) ConnectToUDP(udpPort int) error {
	gs.mutex.Lock()

	if gs.isConnecting || gs.isConnected {
		gs.mutex.Unlock()
		logrus.Warnf("⚠️ macOS: Already connected or connecting (isConnecting=%v, isConnected=%v)", gs.isConnecting, gs.isConnected)
		return fmt.Errorf("already connected or connecting")
	}

	gs.manualDisconnect = false
	gs.lastFrameTime = time.Time{}

	// Signal stop and wait for goroutines to finish
	if gs.stop != nil {
		gs.stop.signal()
	}
	for gs.frameProcessorRunning || gs.frameReaderRunning || gs.monitorRunning {
		gs.mutex.Unlock()
		time.Sleep(50 * time.Millisecond)
		gs.mutex.Lock()
	}

	gs.stop = newStopSignal()

	// Start new frame processor ONCE
	gs.frameProcessorRunning = true
	go gs.frameProcessor()
	logrus.Info("🔍 macOS DEBUG: frameProcessor started")

	gs.isConnecting = true
	gs.mutex.Unlock()

	logrus.Infof("🔗 [VIDEO] Step 2: Connecting to RTP video mode=%s (port=%d)", gs.videoMode, udpPort)

	// Kill stale processes on this port BEFORE starting new one
	gs.killStaleGStreamerProcesses(udpPort)

	if gs.videoMode == models.VideoModeJPEGRTP {
		var lastErr error
		candidates := gs.buildPipelineArgsJPEGCandidates(udpPort)
		for idx, args := range candidates {
			logrus.Infof("🔄 macOS JPEG pipeline attempt %d/%d: %v", idx+1, len(candidates), args)
			if err := gs.runGStreamerPipeline(args, nil); err != nil {
				lastErr = err
				logrus.Warnf("⚠️ macOS JPEG pipeline attempt %d failed: %v", idx+1, err)
				continue
			}
			return nil
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no usable macOS MJPEG pipeline candidates")
		}
		return lastErr
	}
	return gs.runGStreamerPipeline(gs.buildPipelineArgs(udpPort), nil)
}

// ConnectToUDPViaPipe connects to H.264 via pipe (UDP relay with keepalive for FRP).
func (gs *GStreamerService) ConnectToUDPViaPipe(pipeReader *os.File) error {
	gs.mutex.Lock()
	if gs.isConnecting || gs.isConnected {
		gs.mutex.Unlock()
		return fmt.Errorf("already connected or connecting")
	}
	gs.manualDisconnect = false
	gs.lastFrameTime = time.Time{}
	if gs.stop != nil {
		gs.stop.signal()
	}
	for gs.frameProcessorRunning || gs.frameReaderRunning || gs.monitorRunning {
		gs.mutex.Unlock()
		time.Sleep(50 * time.Millisecond)
		gs.mutex.Lock()
	}
	gs.stop = newStopSignal()
	gs.frameProcessorRunning = true
	go gs.frameProcessor()
	gs.isConnecting = true
	gs.mutex.Unlock()

	logrus.Infof("🔗 [VIDEO] Connecting via pipe (UDP relay, mode=%s)", gs.videoMode)
	// ExtraFiles[0] → fd 3 in child process
	return gs.runGStreamerPipeline(gs.buildPipelineArgsPipe(3), pipeReader)
}

// killStaleGStreamerProcesses kills orphaned gst-launch processes on the same UDP port.
// Without this, an old zombie process intercepts all UDP packets and the new GStreamer receives nothing.
func (gs *GStreamerService) killStaleGStreamerProcesses(udpPort int) {
	myPID := os.Getpid()
	portStr := fmt.Sprintf("port=%d", udpPort)

	// Look for all gst-launch processes
	out, err := exec.Command("pgrep", "-f", "gst-launch.*udpsrc").Output()
	if err != nil {
		return // no processes to kill
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid == 0 {
			continue
		}

		// Don't kill our current child process
		gs.mutex.RLock()
		ownCmd := gs.cmd
		gs.mutex.RUnlock()
		if ownCmd != nil && ownCmd.Process != nil && ownCmd.Process.Pid == pid {
			continue
		}

		// Check command line — does port match
		cmdline, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		if err != nil {
			continue
		}
		cmdStr := string(cmdline)
		if !strings.Contains(cmdStr, portStr) {
			continue
		}

		// Check PPID — orphaned (PPID=1) or stranger (PPID != our PID)
		ppidOut, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "ppid=").Output()
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(strings.TrimSpace(string(ppidOut)))
		if err != nil {
			continue
		}
		if ppid == myPID {
			continue // our child — don't touch
		}

		logrus.Warnf("🧹 Killing old gst-launch (PID=%d, PPID=%d) on port %d", pid, ppid, udpPort)
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Signal(syscall.SIGTERM)
			// Allow time for graceful shutdown, then SIGKILL
			go func(p *os.Process) {
				time.Sleep(500 * time.Millisecond)
				_ = p.Signal(syscall.SIGKILL)
			}(proc)
		}
	}
}

// runGStreamerPipeline starts gst-launch. pipeReader != nil → fdsrc (ExtraFiles[0]=fd 3).
func (gs *GStreamerService) runGStreamerPipeline(pipelineArgs []string, pipeReader *os.File) error {
	logrus.Infof("🔧 macOS: GStreamer pipeline: gst-launch-1.0 %v", pipelineArgs)

	gstLaunchPath, err := findDarwinGStreamerTool("gst-launch-1.0")
	if gstLaunchPath == "" {
		gs.mutex.Lock()
		gs.isConnecting = false
		gs.frameProcessorRunning = false
		gs.mutex.Unlock()
		if gs.stop != nil {
			gs.stop.signal()
		}
		return fmt.Errorf("gst-launch-1.0 not found. Install with: brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad")
	}

	// Kill orphaned gst-launch on our port (zombies from previous runs intercept UDP)
	for _, arg := range pipelineArgs {
		if strings.HasPrefix(arg, "port=") {
			if p, err := strconv.Atoi(strings.TrimPrefix(arg, "port=")); err == nil && p > 0 {
				gs.killStaleGStreamerProcesses(p)
				time.Sleep(200 * time.Millisecond) // give OS time to free socket
				break
			}
		}
	}

	gs.mutex.Lock()
	gs.cmd = exec.Command(gstLaunchPath, pipelineArgs...)
	gs.cmd.Env = gs.getGStreamerEnv()
	if pipeReader != nil {
		gs.cmd.ExtraFiles = []*os.File{pipeReader}
	}
	gs.mutex.Unlock()

	stdout, err := gs.cmd.StdoutPipe()
	if err != nil {
		gs.mutex.Lock()
		gs.isConnecting = false
		gs.frameProcessorRunning = false
		gs.mutex.Unlock()
		if gs.stop != nil {
			gs.stop.signal()
		}
		logrus.Errorf("❌ macOS: Error creating stdout pipe: %v", err)
		return fmt.Errorf("error creating stdout pipe: %v", err)
	}

	stderr, err := gs.cmd.StderrPipe()
	if err != nil {
		gs.mutex.Lock()
		gs.isConnecting = false
		gs.frameProcessorRunning = false
		gs.mutex.Unlock()
		if gs.stop != nil {
			gs.stop.signal()
		}
		logrus.Errorf("❌ macOS: Error creating stderr pipe: %v", err)
		return fmt.Errorf("error creating stderr pipe: %v", err)
	}

	gs.mutex.Lock()
	gs.stdout = stdout
	gs.mutex.Unlock()

	// Start goroutine to read stderr (for diagnostics)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			// Skip harmless GTK warnings (duplicate classes when gtk3+gtk4)
			if strings.Contains(line, "implemented in both") && strings.Contains(line, "libgtk") {
				continue
			}
			logrus.Warnf("📺 gst-launch stderr: %s", line)
		}
	}()

	if err := gs.cmd.Start(); err != nil {
		gs.mutex.Lock()
		gs.isConnecting = false
		gs.frameProcessorRunning = false
		gs.mutex.Unlock()
		if gs.stop != nil {
			gs.stop.signal()
		}
		logrus.Errorf("❌ macOS: Error starting gst-launch-1.0: %v", err)
		return fmt.Errorf("error starting gst-launch-1.0: %v", err)
	}

	logrus.Infof("✅ macOS: gst-launch-1.0 process started (PID: %d)", gs.cmd.Process.Pid)

	// Start reading frames
	gs.mutex.Lock()
	gs.frameReaderRunning = true
	gs.mutex.Unlock()
	go gs.readFrames()

	// Start process monitoring
	gs.mutex.Lock()
	gs.monitorRunning = true
	gs.mutex.Unlock()
	go gs.monitorProcess()

	// DO NOT set isConnected = true here!
	// This will be done in readFrames() after receiving the first frame
	gs.mutex.Lock()
	gs.isConnecting = false
	// isConnected stays false until first frame
	gs.mutex.Unlock()

	logrus.Infof("🔗 [VIDEO] Step 3: GStreamer process started, waiting for first frame (mode=%s)...", gs.videoMode)
	return nil
}

// firstByteLogReader wraps io.Reader and logs upon first byte reception
type firstByteLogReader struct {
	io.Reader
	port int
	once sync.Once
}

func (r *firstByteLogReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.once.Do(func() {
		if n > 0 {
			logrus.Infof("📥 [VIDEO] Step 5: First bytes from GStreamer (%d bytes) — udpsrc RTP receiving data", n)
		}
	})
	return n, err
}

// readFrames reads RGBA frames from gst-launch stdout
func (gs *GStreamerService) readFrames() {
	defer func() {
		gs.mutex.Lock()
		gs.frameReaderRunning = false
		gs.isConnected = false // Important! Reset flag on finish
		gs.mutex.Unlock()
		logrus.Info("🛑 macOS: readFrames finished")
	}()

	gs.mutex.RLock()
	width := gs.width
	height := gs.height
	stdout := gs.stdout
	done := gs.stop.done()
	udpPort := 0
	if gs.config != nil {
		udpPort = gs.config.VideoUDPPort
	}
	gs.mutex.RUnlock()

	logrus.Infof("🎬 [VIDEO] Step 1: readFrames started (udpsrc RTP port=%d, frame=%dx%d RGBA)", udpPort, width, height)

	frameSize := width * height * 4 // RGBA
	buffer := make([]byte, frameSize)
	reader := bufio.NewReader(&firstByteLogReader{Reader: stdout, port: udpPort})
	firstFrameReceived := false
	waitStart := time.Now()

	// Periodic log for waiting first frame (ReadFull blocks, so separate goroutine)
	waitDone := make(chan struct{})
	var waitDoneOnce sync.Once
	closeWaitDone := func() { waitDoneOnce.Do(func() { close(waitDone) }) }
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-waitDone:
				return
			case <-done:
				return
			case <-ticker.C:
				logrus.Warnf("⚠️ [VIDEO] Step 4: Waiting for first frame (%.0f sec) — udpsrc RTP port=%d, check FRP visitor→GStreamer", time.Since(waitStart).Seconds(), udpPort)
			}
		}
	}()

	for {
		select {
		case <-done:
			closeWaitDone()
			logrus.Info("🛑 macOS: Stopping frame reading by stop signal")
			return
		default:
		}

		// Read exactly one frame
		n, err := io.ReadFull(reader, buffer)
		if err != nil {
			closeWaitDone()
			if err == io.EOF {
				logrus.Warn("⚠️ [VIDEO] EOF - stream finished (GStreamer received no data?)")
				gs.mutex.RLock()
				shouldReconnect := !gs.manualDisconnect && gs.autoReconnect
				gs.mutex.RUnlock()
				if shouldReconnect {
					go gs.attemptReconnect()
				}
				return
			}
			logrus.Errorf("❌ [VIDEO] Frame read error: %v", err)
			gs.mutex.RLock()
			shouldReconnect := !gs.manualDisconnect && gs.autoReconnect
			gs.mutex.RUnlock()
			if shouldReconnect {
				go gs.attemptReconnect()
			}
			return
		}

		if n != frameSize {
			logrus.Warnf("⚠️ macOS: Incomplete frame: %d bytes (expected %d)", n, frameSize)
			continue
		}

		producedAt := time.Now()
		img := rgbaToImage(buffer, width, height)
		if img != nil {
			now := time.Now()
			meta := videoLatencyFrameMeta{
				producedAt:  producedAt,
				copyTime:    time.Since(producedAt),
				frameWidth:  width,
				frameHeight: height,
			}
			gs.mutex.Lock()
			gs.frameCount++
			frameNum := gs.frameCount

			if gs.lastFrameTime.IsZero() {
				logrus.Infof("🎬 macOS: FIRST FRAME received (%dx%d)", width, height)
			} else if now.Sub(gs.lastFrameTime) > 1*time.Second {
				logrus.Infof("🎬 macOS: RESUMED after %.1fs gap", now.Sub(gs.lastFrameTime).Seconds())
			}
			gs.lastFrameTime = now

			// Set isConnected = true upon first frame
			if !firstFrameReceived {
				gs.isConnected = true
				firstFrameReceived = true
				closeWaitDone()
				logrus.Info("✅ [VIDEO] Step 6: First frame received! Connection established.")

				// Call callback if present
				stateCallback := gs.onStateChanged
				if stateCallback != nil {
					go stateCallback("connected")
				}
			}

			// Log every 300th frame (~10 sec at 30fps)
			if frameNum%300 == 0 || now.Sub(gs.lastFrameReport) > 10*time.Second {
				gs.lastFrameReport = now
				dropped := gs.framesDropped
				chanLen := len(gs.frameChan)
				chanCap := cap(gs.frameChan)
				logrus.Infof("🎬 macOS status: %d frames total | Dropped: %d | Channel: %d/%d | Size: %dx%d", frameNum, dropped, chanLen, chanCap, width, height)
			}
			gs.mutex.Unlock()

			// Send frame to channel in NON-BLOCKING way
			select {
			case gs.frameChan <- videoFramePacket{img: img, meta: meta}:
				// Frame sent successfully
				gs.recordIngressLatency(meta)
			default:
				// Channel full - drop frame (critical for realtime!)
				gs.mutex.Lock()
				gs.framesDropped++
				dropped := gs.framesDropped
				gs.mutex.Unlock()
				// Log every 30th dropped frame
				if dropped%120 == 1 {
					chanLen := len(gs.frameChan)
					logrus.Debugf("⏭️ macOS GStreamer: dropped frame #%d (total dropped: %d, channel: %d/%d)", frameNum, dropped, chanLen, cap(gs.frameChan))
				}
			}
		}
	}
}

// frameProcessor processes frames from channel and sends to UI
func (gs *GStreamerService) frameProcessor() {
	defer func() {
		// Clear remaining frames from channel on finish
		for {
			select {
			case _, ok := <-gs.frameChan:
				if !ok {
					// Channel closed
					goto cleanup
				}
				// Ignore remaining frames
			default:
				// Channel empty
				goto cleanup
			}
		}
	cleanup:
		gs.mutex.Lock()
		gs.frameProcessorRunning = false
		gs.mutex.Unlock()
		logrus.Info("🛑 macOS: frameProcessor finished and cleared")
	}()

	processedCount := int64(0)
	lastLogTime := time.Now()

	done := gs.stop.done()
	for {
		select {
		case <-done:
			logrus.Info("🛑 macOS: Stopping frame processor by stop signal")
			return
		case packet, ok := <-gs.frameChan:
			if !ok {
				logrus.Info("🛑 macOS: frameChan closed, stopping processor")
				return
			}

			processedCount++

			if time.Since(lastLogTime) > 5*time.Second {
				chanLen := len(gs.frameChan)
				logrus.Debugf("📤 macOS frameProcessor: processed %d frames, channel: %d/%d", processedCount, chanLen, cap(gs.frameChan))
				lastLogTime = time.Now()
			}

			gs.mutex.RLock()
			callback := gs.onFrameReceived
			gs.mutex.RUnlock()

			if callback != nil {
				gs.mutex.Lock()
				gs.lastFrameTime = time.Now()
				gs.mutex.Unlock()

				gs.recordUIDelay(time.Since(packet.meta.producedAt), packet.meta, "macOS")
				callback(packet.img)
			}
		}
	}
}

// monitorProcess monitors GStreamer process state
func (gs *GStreamerService) monitorProcess() {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("❌ macOS: Panic in monitorProcess: %v", r)
		}

		gs.mutex.Lock()
		gs.monitorRunning = false
		gs.mutex.Unlock()
		logrus.Info("🛑 macOS: monitorProcess finished")
	}()

	logrus.Info("📊 macOS: Starting gst-launch process monitoring")

	gs.mutex.RLock()
	cmd := gs.cmd
	done := gs.stop.done()
	gs.mutex.RUnlock()

	if cmd == nil {
		logrus.Warn("⚠️ macOS: cmd is nil, cannot monitor")
		return
	}

	err := cmd.Wait()

	gs.mutex.RLock()
	manualDisc := gs.manualDisconnect
	shouldReconnect := !gs.manualDisconnect && gs.autoReconnect
	gs.mutex.RUnlock()

	if err != nil && !manualDisc {
		logrus.Errorf("❌ macOS: gst-launch process exited with error: %v", err)
		gs.mutex.RLock()
		errCallback := gs.onError
		gs.mutex.RUnlock()
		if errCallback != nil {
			errCallback(fmt.Errorf("gst-launch process finished: %v", err))
		}
	} else if !manualDisc {
		logrus.Warn("⚠️ macOS: gst-launch process finished")
	}

	select {
	case <-done:
		logrus.Info("🛑 macOS: Stopping monitoring by stop signal")
		return
	default:
	}

	// Start reconnect if needed
	if shouldReconnect {
		logrus.Info("🔄 macOS: Starting automatic reconnection...")
		go gs.attemptReconnect()
	}
}

// Disconnect disconnects from RTP/UDP stream
func (gs *GStreamerService) Disconnect() error {
	gs.mutex.Lock()

	if !gs.isConnected && !gs.isConnecting {
		gs.mutex.Unlock()
		logrus.Info("🔌 macOS: Disconnect: already disconnected")
		return nil
	}

	logrus.Info("🔌 macOS: Disconnecting from RTP/UDP stream...")

	gs.manualDisconnect = true
	gs.isConnected = false
	gs.isConnecting = false

	cmd := gs.cmd
	stop := gs.stop
	gs.mutex.Unlock()

	if stop != nil {
		stop.signal()
	}

	// Stop gst-launch process
	if cmd != nil && cmd.Process != nil {
		logrus.Info("🛑 macOS: Stopping gst-launch process...")
		cmd.Process.Kill()
		cmd.Wait()

		// Explicitly kill any other processes on this port
		if gs.config != nil && gs.config.VideoUDPPort > 0 {
			gs.killStaleGStreamerProcesses(gs.config.VideoUDPPort)
		}

		logrus.Info("✅ macOS: gst-launch process stopped")
	}

	// Wait for goroutines to finish (max 2 seconds)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gs.mutex.RLock()
		running := gs.frameProcessorRunning || gs.frameReaderRunning || gs.monitorRunning
		gs.mutex.RUnlock()

		if !running {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Check if goroutines finished
	gs.mutex.RLock()
	stillRunning := gs.frameProcessorRunning || gs.frameReaderRunning || gs.monitorRunning
	gs.mutex.RUnlock()

	if stillRunning {
		logrus.Warn("⚠️ macOS: Some goroutines didn't finish within 2 seconds, continuing...")
	}

	// Cleanup
	gs.mutex.Lock()
	gs.cmd = nil
	gs.stdout = nil

	// Clear frame channel from remaining data
	if gs.frameChan != nil {
		// Non-blocking channel clearing
		for {
			select {
			case <-gs.frameChan:
				// Ignore remaining frames
			default:
				// Channel empty
				goto doneClearing
			}
		}
	doneClearing:
		logrus.Info("✅ macOS: Frame channel cleared")
	}
	gs.mutex.Unlock()

	logrus.Info("✅ macOS: GStreamer connection closed")
	return nil
}

// attemptReconnect attempts to reconnect to RTP/UDP stream
func (gs *GStreamerService) attemptReconnect() {
	gs.mutex.Lock()

	// Check conditions for reconnection
	if !gs.autoReconnect || gs.manualDisconnect || gs.isConnecting || gs.isReconnecting {
		gs.mutex.Unlock()
		logrus.Infof("🔄 macOS: Reconnection skipped: autoReconnect=%v, manualDisconnect=%v, isConnecting=%v, isReconnecting=%v",
			gs.autoReconnect, gs.manualDisconnect, gs.isConnecting, gs.isReconnecting)
		return
	}

	if gs.reconnectAttempts >= gs.maxReconnectAttempts {
		logrus.Errorf("❌ macOS: Maximum reconnection attempts reached (%d)", gs.maxReconnectAttempts)
		gs.autoReconnect = false
		gs.mutex.Unlock()
		return
	}

	// Set reconnection flag
	gs.isReconnecting = true
	gs.isConnected = false
	gs.isConnecting = false

	oldCmd := gs.cmd
	stop := gs.stop
	gs.cmd = nil
	gs.mutex.Unlock()

	logrus.Info("🧹 macOS: Cleaning up old process before reconnection...")

	if stop != nil {
		stop.signal()
	}

	// Stop old process
	if oldCmd != nil && oldCmd.Process != nil {
		logrus.Info("🛑 macOS: Stopping old process...")
		oldCmd.Process.Kill()
		oldCmd.Wait()
		time.Sleep(200 * time.Millisecond)
		logrus.Info("✅ macOS: Old process stopped")
	}

	// Wait for goroutines to finish
	logrus.Info("⏳ macOS: Waiting for goroutines to finish...")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		gs.mutex.RLock()
		running := gs.frameProcessorRunning || gs.frameReaderRunning || gs.monitorRunning
		gs.mutex.RUnlock()

		if !running {
			logrus.Info("✅ macOS: All goroutines finished")
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	gs.mutex.Lock()
	gs.reconnectAttempts++
	attempt := gs.reconnectAttempts
	maxAttempts := gs.maxReconnectAttempts
	gs.mutex.Unlock()

	logrus.Infof("🔄 macOS: GStreamer reconnection attempt #%d/%d...", attempt, maxAttempts)

	// Delay before reconnection
	delay := time.Duration(attempt) * 2 * time.Second
	if delay > 10*time.Second {
		delay = 10 * time.Second
	}
	logrus.Infof("⏳ macOS: Delay before reconnection: %v", delay)
	time.Sleep(delay)

	if err := gs.ConnectToRTP(); err != nil {
		logrus.Errorf("❌ macOS: GStreamer reconnection attempt #%d failed: %v", attempt, err)
		gs.mutex.Lock()
		gs.isReconnecting = false
		gs.mutex.Unlock()

		// Try again if not reached limit
		if attempt < maxAttempts {
			logrus.Info("🔄 macOS: Next reconnection attempt scheduled...")
			go gs.attemptReconnect()
		}
	} else {
		logrus.Info("✅ macOS: GStreamer reconnection successful!")
		gs.mutex.Lock()
		gs.reconnectAttempts = 0
		gs.isReconnecting = false
		gs.mutex.Unlock()
	}
}

// SetOnFrameReceived sets callback for frame reception
func (gs *GStreamerService) SetOnFrameReceived(callback func(image.Image)) {
	gs.onFrameReceived = callback
}

// SetOnStateChanged sets callback for state change
func (gs *GStreamerService) SetOnStateChanged(callback func(string)) {
	gs.onStateChanged = callback
}

// SetOnError sets callback for errors
func (gs *GStreamerService) SetOnError(callback func(error)) {
	gs.onError = callback
}

// IsConnected returns connection state
func (gs *GStreamerService) IsConnected() bool {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()
	return gs.isConnected
}

// GetStats returns connection statistics
func (gs *GStreamerService) GetStats() map[string]interface{} {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()

	return map[string]interface{}{
		"connected":        gs.isConnected,
		"connecting":       gs.isConnecting,
		"frame_count":      gs.frameCount,
		"frames_dropped":   gs.framesDropped,
		"last_frame_time":  gs.lastFrameTime,
		"low_latency_mode": gs.config.LowLatencyMode,
	}
}

// UpdateHost updates video stream host
func (gs *GStreamerService) UpdateHost(host string) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.VideoHost = host
	logrus.Debugf("🔧 macOS: GStreamer service: host updated to %s", host)
}

// UpdateVideoPort updates video stream port (RTP/UDP)
func (gs *GStreamerService) UpdateVideoPort(port int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.VideoUDPPort = port
	logrus.Debugf("🔧 macOS: GStreamer service: video UDP port updated to %d", port)
}

// UpdateVideoUDPPort updates video UDP reception port
func (gs *GStreamerService) UpdateVideoUDPPort(port int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.VideoUDPPort = port
	logrus.Debugf("🔧 macOS: GStreamer service: video UDP port updated to %d", port)
}

func (gs *GStreamerService) SetVideoMode(mode string) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	if mode == "" {
		mode = models.VideoModeH264
	}
	gs.videoMode = mode
}

func (gs *GStreamerService) SetExpectedVideoSize(width, height int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	if width > 0 {
		gs.width = width
	}
	if height > 0 {
		gs.height = height
	}
}

// GetConfig returns configuration
func (gs *GStreamerService) GetConfig() *models.AppConfig {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()
	return gs.config
}

func (gs *GStreamerService) SupportsNativeFullscreen() bool {
	// Native macOS fullscreen depends on a global event tap that can trigger
	// Input Monitoring / Accessibility prompts and aggressively capture the mouse.
	// Keep fullscreen on the regular window path until a safer implementation exists.
	return false
}

func (gs *GStreamerService) IsNativeFullscreenActive() bool {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()
	return gs.nativeFullscreenActive
}

func (gs *GStreamerService) StartNativeFullscreen() error {
	if err := gs.Disconnect(); err != nil {
		return err
	}

	udpPort := gs.config.VideoUDPPort
	if udpPort <= 0 {
		udpPort = models.DefaultVideoUDPPort
	}

	candidates := gs.nativeFullscreenCandidates(udpPort)
	var lastErr error
	for _, candidate := range candidates {
		cmd, err := gs.startNativeFullscreenProcess(candidate.name, candidate.args)
		if err == nil {
			gs.mutex.Lock()
			gs.nativeFullscreenCmd = cmd
			gs.nativeFullscreenActive = true
			gs.mutex.Unlock()
			logrus.Infof("✅ macOS native fullscreen started via %s", candidate.name)
			return nil
		}
		lastErr = err
		logrus.Warnf("⚠️ macOS native fullscreen candidate %s failed: %v", candidate.name, err)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no native fullscreen candidates available")
	}
	return lastErr
}

func (gs *GStreamerService) StopNativeFullscreen() error {
	gs.mutex.Lock()
	cmd := gs.nativeFullscreenCmd
	gs.nativeFullscreenCmd = nil
	gs.nativeFullscreenActive = false
	gs.mutex.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = cmd.Process.Signal(syscall.SIGINT)
	time.Sleep(200 * time.Millisecond)
	_ = cmd.Process.Kill()
	return nil
}

func (gs *GStreamerService) nativeFullscreenCandidates(udpPort int) []struct {
	name string
	args []string
} {
	built := gs.buildDarwinFullscreenCandidates(udpPort, 10)
	candidates := make([]struct {
		name string
		args []string
	}, 0, len(built))
	for _, candidate := range built {
		candidates = append(candidates, struct {
			name string
			args []string
		}{
			name: candidate.name,
			args: candidate.args,
		})
	}
	return candidates
}

func (gs *GStreamerService) startNativeFullscreenProcess(name string, args []string) (*exec.Cmd, error) {
	path, err := findDarwinGStreamerTool("gst-launch-1.0")
	if err != nil {
		return nil, fmt.Errorf("gst-launch-1.0 not found: %w", err)
	}

	cmd := exec.Command(path, args...)
	cmd.Env = gs.getGStreamerEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		err := cmd.Wait()
		gs.mutex.Lock()
		if gs.nativeFullscreenCmd == cmd {
			gs.nativeFullscreenCmd = nil
			gs.nativeFullscreenActive = false
		}
		gs.mutex.Unlock()
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			err = fmt.Errorf("%s exited immediately", name)
		}
		return nil, err
	case <-time.After(1200 * time.Millisecond):
		return cmd, nil
	}
}

// SetAutoReconnect enables/disables automatic reconnection
func (gs *GStreamerService) SetAutoReconnect(enabled bool) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	gs.autoReconnect = enabled
}

func (gs *GStreamerService) ResetRuntimeDecoderFallback() {}

// SetMaxReconnectAttempts sets maximum reconnection attempts
func (gs *GStreamerService) SetMaxReconnectAttempts(max int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	gs.maxReconnectAttempts = max
	gs.reconnectAttempts = 0
}

// ConnectToRTP connects to RTP H.264 (via QUIC/SUDP tunnel or directly)
func (gs *GStreamerService) ConnectToRTP() error {
	port := gs.config.VideoUDPPort
	if port <= 0 {
		port = models.DefaultVideoUDPPort
	}
	return gs.ConnectToUDP(port)
}

// Reconnect forcibly reconnects to UDP stream (for device switching)
func (gs *GStreamerService) Reconnect() error {
	logrus.Info("🔄 macOS: Forced reconnection (device switch)...")

	// Disconnect first
	if err := gs.Disconnect(); err != nil {
		logrus.Warnf("⚠️ macOS: Error disconnecting before reconnection: %v", err)
	}

	// Wait a bit for proper disconnection
	time.Sleep(500 * time.Millisecond)

	// Reset reconnection attempt counter
	gs.mutex.Lock()
	gs.reconnectAttempts = 0
	gs.autoReconnect = true
	gs.manualDisconnect = false
	gs.mutex.Unlock()

	// Connect again
	logrus.Info("🔗 macOS: Connecting to new device...")
	return gs.ConnectToRTP()
}
