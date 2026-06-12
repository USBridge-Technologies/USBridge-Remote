//go:build windows
// +build windows

package service

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"
	"github.com/tinyzimmer/go-gst/gst"
	"github.com/tinyzimmer/go-gst/gst/app"
)

// GStreamerService service for working with GStreamer H264 stream on Windows
// Uses d3d11h264dec for hardware decoding (Direct3D11/DXVA)
type GStreamerService struct {
	config    *models.AppConfig
	videoMode string

	// GStreamer pipeline
	pipeline               *gst.Pipeline
	appsink                *app.Sink
	nativeFullscreenCmd    *exec.Cmd
	nativeFullscreenActive bool
	jpegCandidateIndex     int
	lastJPEGPipeline       string
	expectedWidth          int
	expectedHeight         int

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
	stopChan  chan struct{}

	// Flags for goroutine management
	frameProcessorRunning bool
	monitorRunning        bool

	// Statistics
	lastFrameTime    time.Time
	lastSampleTime   time.Time
	lastSampleReport time.Time
	frameCount       int64
	framesDropped  int64
	latencyProfile videoLatencyProfile

	// Pool of *image.RGBA buffers — reused across frames to reduce GC pressure.
	// frameChan cap=1 means at most 2 frames live at once.
	framePool sync.Pool

	// Mutexes
	mutex sync.RWMutex

	// Callbacks
	onFrameReceived func(image.Image)
	onStateChanged  func(string)
	onError         func(error)
}

// NewGStreamerService creates a new GStreamer service for Windows
func (gs *GStreamerService) GetBindHost() string {
	if gs == nil || gs.config == nil || strings.TrimSpace(gs.config.VideoBindHost) == "" {
		return "127.0.0.1"
	}
	return strings.TrimSpace(gs.config.VideoBindHost)
}

// UpdateHost updates video stream host
func (gs *GStreamerService) UpdateHost(host string) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	if gs.config != nil {
		gs.config.VideoHost = host
	}
	logrus.Debugf("🔧 [Windows] GStreamer service: host updated to %s", host)
}

func NewGStreamerService(config *models.AppConfig) *GStreamerService {
	gs := &GStreamerService{
		config:                config,
		autoReconnect:         true,
		reconnectAttempts:     0,
		maxReconnectAttempts:  5,
		videoMode:             models.VideoModeH264,
		frameChan:             make(chan videoFramePacket, 1), // One frame — minimal latency
		stopChan:              make(chan struct{}),
		frameProcessorRunning: false,
		monitorRunning:        false,
	}

	logrus.Info("✅ GStreamer service for Windows initialized (d3d11h264dec - hardware decoding)")
	return gs
}

// ConnectToRTP connects to RTP H.264 stream (UDP, new protocol)
func (gs *GStreamerService) ConnectToRTP() error {
	gs.mutex.Lock()

	if gs.isConnecting || gs.isConnected {
		gs.mutex.Unlock()
		return fmt.Errorf("already connected or connecting")
	}

	gs.manualDisconnect = false
	gs.lastFrameTime = time.Time{}

	// Close old stopChan if exists
	if gs.stopChan != nil {
		select {
		case <-gs.stopChan: // Channel already closed
		default:
			close(gs.stopChan) // Close channel
		}
	}

	// Wait for goroutines to stop (3-second timeout guards against stuck goroutines)
	deadline := time.Now().Add(3 * time.Second)
	for gs.frameProcessorRunning || gs.monitorRunning {
		if time.Now().After(deadline) {
			logrus.Warn("⚠️ [Windows] ConnectToRTP: goroutines did not exit within 3s, proceeding anyway")
			break
		}
		gs.mutex.Unlock()
		time.Sleep(50 * time.Millisecond)
		gs.mutex.Lock()
	}

	// Recreate stopChan for new connection
	gs.stopChan = make(chan struct{})

	// Start new frame processor ONCE
	gs.frameProcessorRunning = true
	go gs.frameProcessor()

	gs.isConnecting = true
	gs.mutex.Unlock()

	logrus.Infof("🔗 [Windows] Connecting to RTP stream mode=%s...", gs.videoMode)

	// Initialize GStreamer (gst.Init does not return error)
	gst.Init(nil)

	// Create pipeline with hardware decoding (d3d11h264dec + d3d11download to enter PLAYING)
	if err := gs.createPipeline(); err != nil {
		gs.mutex.Lock()
		gs.isConnecting = false
		gs.mutex.Unlock()
		return fmt.Errorf("pipeline creation error: %v", err)
	}

	// Start pipeline
	if err := gs.pipeline.SetState(gst.StatePlaying); err != nil {
		// Hardware decoder created but didn't enter PLAYING (typically without d3d11download) — try software
		logrus.Warnf("⚠️ [Windows] d3d11h264dec didn't enter PLAYING (%v), switching to avdec_h264", err)
		gs.pipeline.SetState(gst.StateNull)
		gs.pipeline = nil
		gs.appsink = nil
		if errSW := gs.createPipelineSoftware(); errSW != nil {
			gs.mutex.Lock()
			gs.isConnecting = false
			gs.mutex.Unlock()
			return fmt.Errorf("pipeline start and fallback error: %v", errSW)
		}
		if errPlay := gs.pipeline.SetState(gst.StatePlaying); errPlay != nil {
			gs.mutex.Lock()
			gs.isConnecting = false
			gs.mutex.Unlock()
			return fmt.Errorf("pipeline start error: %v", errPlay)
		}
	}

	// Start state monitoring ONCE
	gs.mutex.Lock()
	gs.monitorRunning = true
	gs.mutex.Unlock()
	go gs.monitorPipeline()

	gs.mutex.Lock()
	gs.isConnecting = false
	gs.isConnected = true
	gs.mutex.Unlock()

	logrus.Infof("✅ [Windows] GStreamer RTP connection established (mode=%s)", gs.videoMode)
	return nil
}

// ConnectToUDPViaPipe — pipe mode for FRP relay (Windows: stub for now)
func (gs *GStreamerService) ConnectToUDPViaPipe(pipeReader *os.File) error {
	_ = pipeReader
	return fmt.Errorf("UDP relay (pipe) not yet implemented on Windows, use direct connection")
}

// createPipeline creates GStreamer pipeline for current RTP video mode on Windows.
func (gs *GStreamerService) createPipeline() error {
	udpPort := gs.config.VideoUDPPort
	if udpPort <= 0 {
		udpPort = models.DefaultVideoUDPPort
	}
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	logrus.Infof("📹 [Windows] UDP RTP video reception port: %d (mode=%s)", udpPort, gs.videoMode)

	if gs.videoMode == models.VideoModeJPEGRTP {
		return gs.createPipelineJPEG(udpPort)
	}
	if gs.videoMode == models.VideoModeRawYUYV {
		return gs.createPipelineRawYUYV(udpPort)
	}

	logrus.Info("🔧 [Windows] GStreamer pipeline (hardware decoding d3d11h264dec + d3d11download)")

	baseCandidates := []string{
		fmt.Sprintf(
			"udpsrc name=udpsrc0 port=%d buffer-size=131072 timeout=0 caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "+
				"rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "+
				"rtph264depay ! "+
				"h264parse config-interval=-1 ! ",
			udpPort,
		),
		fmt.Sprintf(
			"udpsrc name=udpsrc0 address=%s port=%d buffer-size=131072 timeout=0 caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "+
				"rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "+
				"rtph264depay ! "+
				"h264parse config-interval=-1 ! ",
			bindHost, udpPort,
		),
	}

	var lastErr error
	for _, base := range baseCandidates {
		for _, suffix := range []string{
			"d3d11h264dec ! d3d11download ! videoconvert ! video/x-raw,format=RGBA ! appsink name=sink sync=false max-buffers=1 drop=true",
			"d3d11h264dec ! videoconvert ! video/x-raw,format=RGBA ! appsink name=sink sync=false max-buffers=1 drop=true",
		} {
			pipeline, err := gst.NewPipelineFromString(base + suffix)
			if err != nil {
				lastErr = err
				continue
			}
			logrus.Info("✅ [Windows] GStreamer pipeline created (d3d11h264dec - hardware decoding)")
			gs.pipeline = pipeline
			return gs.attachAppsink()
		}
	}

	logrus.Warnf("⚠️ [Windows] d3d11h264dec unavailable (%v), switching to avdec_h264 (software decoder)", lastErr)
	return gs.createPipelineSoftware()
}

func (gs *GStreamerService) createPipelineRawYUYV(udpPort int) error {
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	width := gs.expectedWidth
	height := gs.expectedHeight
	if width <= 0 {
		width = gs.config.VideoWidth
	}
	if height <= 0 {
		height = gs.config.VideoHeight
	}
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}

	logrus.Infof("🔧 [Windows/RAW] Building RAW YUYV pipeline: %dx%d", width, height)

	hwPipeline := fmt.Sprintf(
		"udpsrc address=%s port=%d buffer-size=4194304 caps=\"application/x-rtp,media=(string)video,clock-rate=(int)90000,encoding-name=(string)RAW,sampling=(string)YCbCr-4:2:2,depth=(string)8,width=(string)%d,height=(string)%d,colorimetry=(string)BT601-5,payload=(int)96\" ! "+
			"rtpjitterbuffer latency=5 faststart-min-packets=1 drop-on-latency=true ! "+
			"rtpvrawdepay ! video/x-raw,format=UYVY,width=%d,height=%d ! "+
			"d3d11upload ! d3d11convert ! d3d11download ! "+
			"video/x-raw,format=RGBA ! "+
			"appsink name=sink sync=false max-buffers=1 drop=true",
		bindHost, udpPort, width, height, width, height,
	)

	swPipeline := fmt.Sprintf(
		"udpsrc address=%s port=%d buffer-size=4194304 caps=\"application/x-rtp,media=(string)video,clock-rate=(int)90000,encoding-name=(string)RAW,sampling=(string)YCbCr-4:2:2,depth=(string)8,width=(string)%d,height=(string)%d,colorimetry=(string)BT601-5,payload=(int)96\" ! "+
			"rtpjitterbuffer latency=5 faststart-min-packets=1 drop-on-latency=true ! "+
			"rtpvrawdepay ! video/x-raw,format=UYVY,width=%d,height=%d ! "+
			"videoconvert ! video/x-raw,format=RGBA ! "+
			"appsink name=sink sync=false max-buffers=1 drop=true",
		bindHost, udpPort, width, height, width, height,
	)

	var pipeline *gst.Pipeline
	var err error
	if gst.Find("d3d11upload") != nil && gst.Find("d3d11convert") != nil && gst.Find("d3d11download") != nil {
		pipeline, err = gst.NewPipelineFromString(hwPipeline)
		if err == nil {
			logrus.Info("✅ [Windows/RAW] GStreamer pipeline created: d3d11upload -> d3d11convert -> d3d11download")
			gs.pipeline = pipeline
			return gs.attachAppsink()
		}
		logrus.Warnf("⚠️ [Windows/RAW] D3D11 RAW pipeline unavailable (%v), falling back to software conversion", err)
	} else {
		logrus.Info("ℹ️ [Windows/RAW] D3D11 upload/convert/download elements not fully available, using software conversion")
	}

	pipeline, err = gst.NewPipelineFromString(swPipeline)
	if err != nil {
		return fmt.Errorf("error creating RAW YUYV pipeline: %v", err)
	}

	logrus.Info("✅ [Windows/RAW] GStreamer pipeline created: videoconvert fallback")
	gs.pipeline = pipeline
	return gs.attachAppsink()
}

func (gs *GStreamerService) createPipelineJPEG(udpPort int) error {
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	pipelines := []struct {
		name            string
		requiredElement string
		str             string
	}{
		{
			name:            "qsvjpegdec (HW Intel)",
			requiredElement: "qsvjpegdec",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegparse ! qsvjpegdec ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "nvjpegdec (HW NVIDIA)",
			requiredElement: "nvjpegdec",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegparse ! nvjpegdec ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "wicjpegdec (HW/OS)",
			requiredElement: "wicjpegdec",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! wicjpegdec ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "msdkmjpegdec (legacy HW Intel)",
			requiredElement: "msdkmjpegdec",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegparse ! msdkmjpegdec ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "jpegdec (SW preferred)",
			requiredElement: "jpegdec",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegdec ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "avdec_mjpeg (SW libav)",
			requiredElement: "avdec_mjpeg",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegparse ! avdec_mjpeg ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "decodebin",
			requiredElement: "decodebin",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegparse ! decodebin ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
	}

	availableJPEGElements := []string{"qsvjpegdec", "nvjpegdec", "wicjpegdec", "msdkmjpegdec", "jpegdec", "avdec_mjpeg", "decodebin"}
	var detected []string
	for _, element := range availableJPEGElements {
		if gst.Find(element) != nil {
			detected = append(detected, element)
		}
	}
	if len(detected) > 0 {
		logrus.Infof("🔎 [Windows/JPEG] available decoders/elements: %s", strings.Join(detected, ", "))
	} else {
		logrus.Warn("⚠️ [Windows/JPEG] no JPEG decoders detected in current GStreamer runtime")
	}

	var lastErr error
	for idx := range pipelines {
		candidateIdx := (gs.jpegCandidateIndex + idx) % len(pipelines)
		candidate := pipelines[candidateIdx]
		if candidate.requiredElement != "" && gst.Find(candidate.requiredElement) == nil {
			logrus.Infof("ℹ️ [Windows/JPEG] skipping %s: element %q not installed in current runtime", candidate.name, candidate.requiredElement)
			continue
		}
		pipeline, err := gst.NewPipelineFromString(candidate.str)
		if err != nil {
			lastErr = err
			logrus.Warnf("⚠️ [Windows/JPEG] pipeline unavailable (%s): %v", candidate.name, err)
			continue
		}

		gs.pipeline = pipeline
		if err := gs.attachAppsink(); err != nil {
			lastErr = err
			gs.pipeline.SetState(gst.StateNull)
			gs.pipeline = nil
			continue
		}
		gs.lastJPEGPipeline = candidate.name
		logrus.Infof("✅ [Windows/JPEG] GStreamer pipeline created: %s", candidate.name)
		return nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no suitable GStreamer JPEG decoders found")
	}
	return lastErr
}

// createPipelineSoftware creates pipeline with software decoder avdec_h264 only (fallback on PLAYING error or no d3d11).
func (gs *GStreamerService) createPipelineSoftware() error {
	udpPort := gs.config.VideoUDPPort
	if udpPort <= 0 {
		udpPort = models.DefaultVideoUDPPort
	}
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}

	candidates := []string{
		fmt.Sprintf(
			"udpsrc name=udpsrc0 port=%d buffer-size=131072 timeout=0 caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "+
				"rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "+
				"rtph264depay ! "+
				"h264parse config-interval=-1 ! "+
				"avdec_h264 max-threads=4 ! "+
				"videoconvert ! "+
				"video/x-raw,format=RGBA ! "+
				"appsink name=sink sync=false max-buffers=1 drop=true",
			udpPort,
		),
		fmt.Sprintf(
			"udpsrc name=udpsrc0 address=%s port=%d buffer-size=131072 timeout=0 caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "+
				"rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "+
				"rtph264depay ! "+
				"h264parse config-interval=-1 ! "+
				"avdec_h264 max-threads=4 ! "+
				"videoconvert ! "+
				"video/x-raw,format=RGBA ! "+
				"appsink name=sink sync=false max-buffers=1 drop=true",
			bindHost, udpPort,
		),
	}

	var lastErr error
	for _, candidate := range candidates {
		pipeline, err := gst.NewPipelineFromString(candidate)
		if err != nil {
			lastErr = err
			continue
		}
		logrus.Info("✅ [Windows] GStreamer pipeline created (avdec_h264 - software decoder)")
		gs.pipeline = pipeline
		return gs.attachAppsink()
	}
	return fmt.Errorf("pipeline creation error (avdec_h264): %v", lastErr)
}

// attachAppsink finds appsink in current pipeline and connects frame callback.
func (gs *GStreamerService) attachAppsink() error {
	sinkElement, err := gs.pipeline.GetElementByName("sink")
	if err != nil {
		return fmt.Errorf("appsink retrieval error: %v", err)
	}
	gs.appsink = app.SinkFromElement(sinkElement)
	gs.appsink.SetCallbacks(&app.SinkCallbacks{
		NewSampleFunc: func(sink *app.Sink) gst.FlowReturn {
			sample := sink.PullSample()
			if sample == nil {
				return gst.FlowEOS
			}
			gs.processSample(sample)
			return gst.FlowOK
		},
	})
	return nil
}

// processSample processes one sample
func (gs *GStreamerService) processSample(sample *gst.Sample) {
	producedAt := time.Now()
	// Get buffer
	buffer := sample.GetBuffer()
	if buffer == nil {
		return
	}

	// Get caps to determine frame size
	caps := sample.GetCaps()
	if caps == nil {
		return
	}

	structure := caps.GetStructureAt(0)
	width, _ := structure.GetValue("width")
	height, _ := structure.GetValue("height")

	w := width.(int)
	h := height.(int)

	// Read data from buffer
	mapInfo := buffer.Map(gst.MapRead)
	if mapInfo == nil {
		return
	}

	data := mapInfo.Bytes()
	expectedSize := w * h * 4

	// Copy data to new slice BEFORE Unmap
	dataCopy := make([]byte, expectedSize)
	if len(data) >= expectedSize {
		copy(dataCopy, data[:expectedSize])
	} else {
		buffer.Unmap()
		return
	}

	// Release map
	buffer.Unmap()

	// Submit to native Vulkan/GDI overlay if active (fast path, bypasses Fyne canvas).
	if NativeVideoOverlayIsActive() {
		if !VKVideoTrySubmit(dataCopy, w, h, w*4) {
			GLVideoTrySubmit(dataCopy, w, h, w*4)
		}
	}

	// Convert to image.Image using copied data
	img := gs.rgbaToImage(dataCopy, w, h)
	if img != nil {
		now := time.Now()
		meta := videoLatencyFrameMeta{
			producedAt:  producedAt,
			copyTime:    time.Since(producedAt),
			frameWidth:  w,
			frameHeight: h,
		}
		gs.mutex.Lock()
		gs.frameCount++
		frameNum := gs.frameCount
		
		if gs.lastSampleTime.IsZero() {
			logrus.Infof("🎬 [Windows] GStreamer: FIRST SAMPLE received (%dx%d)", w, h)
		} else if now.Sub(gs.lastSampleTime) > 1*time.Second {
			logrus.Infof("🎬 [Windows] GStreamer: RESUMED after %.1fs gap", now.Sub(gs.lastSampleTime).Seconds())
		}
		gs.lastSampleTime = now
		
		// Log every 300th frame (~10 sec at 30fps)
		if frameNum%300 == 0 || now.Sub(gs.lastSampleReport) > 10*time.Second {
			gs.lastSampleReport = now
			dropped := gs.framesDropped
			chanLen := len(gs.frameChan)
			chanCap := cap(gs.frameChan)
			logrus.Infof("🎬 [Windows] GStreamer status: %d frames total | Dropped: %d | Channel: %d/%d | Size: %dx%d", frameNum, dropped, chanLen, chanCap, w, h)
		}
		gs.mutex.Unlock()

		// Send frame to channel in NON-BLOCKING way
		select {
		case gs.frameChan <- videoFramePacket{img: img, meta: meta}:
			// Frame sent successfully
			gs.recordIngressLatency(meta)
		default:
			// Channel full - return buffer to pool and drop frame (critical for realtime!)
			framePoolPut(&gs.framePool, img)
			gs.mutex.Lock()
			gs.framesDropped++
			dropped := gs.framesDropped
			gs.mutex.Unlock()
			// Log every 120th dropped frame
			if dropped%120 == 1 {
				chanLen := len(gs.frameChan)
				logrus.Debugf("⏭️ [Windows] GStreamer: dropped frame #%d (total dropped: %d, channel: %d/%d)", frameNum, dropped, chanLen, cap(gs.frameChan))
			}
		}
	}
}

// rgbaToImage converts RGBA data to image.Image, reusing a pooled buffer.
func (gs *GStreamerService) rgbaToImage(data []byte, width, height int) *image.RGBA {
	expectedSize := width * height * 4
	if len(data) < expectedSize {
		logrus.Warnf("⚠️ [Windows] Insufficient data for frame: %d bytes (expected %d)", len(data), expectedSize)
		return nil
	}
	img := framePoolGet(&gs.framePool, width, height)
	copy(img.Pix, data[:expectedSize])
	return img
}

// frameProcessor processes frames from channel and sends to UI
func (gs *GStreamerService) frameProcessor() {
	defer func() {
		// Drain remaining frames and return buffers to pool.
		for {
			select {
			case pkt, ok := <-gs.frameChan:
				if !ok {
					goto cleanup
				}
				if rgba, ok2 := pkt.img.(*image.RGBA); ok2 {
					framePoolPut(&gs.framePool, rgba)
				}
			default:
				goto cleanup
			}
		}
	cleanup:
		gs.mutex.Lock()
		gs.frameProcessorRunning = false
		gs.mutex.Unlock()
		logrus.Info("🛑 [Windows] frameProcessor finished and cleared")
	}()

	processedCount := int64(0)
	lastLogTime := time.Now()

	for {
		select {
		case <-gs.stopChan:
			logrus.Info("🛑 [Windows] Stopping frame processor by stopChan signal")
			return
		case packet, ok := <-gs.frameChan:
			if !ok {
				logrus.Info("🛑 [Windows] frameChan closed, stopping processor")
				return
			}

			processedCount++

			// Log every 5 seconds
			if time.Since(lastLogTime) > 5*time.Second {
				chanLen := len(gs.frameChan)
				logrus.Debugf("📤 [Windows] frameProcessor: processed %d frames, channel: %d/%d", processedCount, chanLen, cap(gs.frameChan))
				lastLogTime = time.Now()
			}

			// Send frame to callback
			gs.mutex.RLock()
			callback := gs.onFrameReceived
			gs.mutex.RUnlock()

			if callback != nil {
				gs.mutex.Lock()
				gs.lastFrameTime = time.Now()
				gs.mutex.Unlock()

				gs.recordUIDelay(time.Since(packet.meta.producedAt), packet.meta, "Windows")
				callback(packet.img)
			}
			// Return buffer to pool after callback has copied the pixels.
			if rgba, ok := packet.img.(*image.RGBA); ok {
				framePoolPut(&gs.framePool, rgba)
			}
		}
	}
}

// monitorPipeline monitors GStreamer pipeline state
func (gs *GStreamerService) monitorPipeline() {
	defer func() {
		// Safety panic handling
		if r := recover(); r != nil {
			logrus.Errorf("❌ [Windows] Panic in monitorPipeline: %v", r)
		}

		gs.mutex.Lock()
		gs.monitorRunning = false
		gs.mutex.Unlock()
		logrus.Info("🛑 [Windows] monitorPipeline finished and cleared")
	}()

	logrus.Info("📊 [Windows] Starting GStreamer pipeline monitoring")

	// Get bus for message monitoring
	gs.mutex.RLock()
	pipeline := gs.pipeline
	stopChan := gs.stopChan
	gs.mutex.RUnlock()

	if pipeline == nil {
		logrus.Warn("⚠️ [Windows] Pipeline is nil, cannot monitor")
		return
	}

	bus := pipeline.GetPipelineBus()
	if bus == nil {
		logrus.Warn("⚠️ [Windows] Bus is nil, cannot monitor")
		return
	}

	// Use TimedPop WITHOUT manual Unref - messages are freed automatically
	for {
		// Check stopChan for immediate stop
		select {
		case <-stopChan:
			logrus.Info("🛑 [Windows] Stopping monitoring by stopChan signal")
			return
		default:
		}

		// Check connection state before each cycle
		gs.mutex.RLock()
		connected := gs.isConnected
		manualDisc := gs.manualDisconnect
		currentPipeline := gs.pipeline
		lastSample := gs.lastSampleTime
		gs.mutex.RUnlock()

		if connected && !manualDisc && !lastSample.IsZero() && time.Since(lastSample) > 5*time.Second {
			logrus.Warnf("⚠️ [Windows] GStreamer SILENCE: no samples for %.1fs (pipeline is %s)", time.Since(lastSample).Seconds(), "PLAYING")
			// Reset lastSampleTime partially to avoid spamming every 100ms
			gs.mutex.Lock()
			gs.lastSampleTime = time.Now().Add(-4 * time.Second)
			gs.mutex.Unlock()
		}

		// If pipeline changed or was removed, finish monitoring
		if currentPipeline != pipeline {
			logrus.Info("🛑 [Windows] Stopping monitoring: pipeline changed")
			return
		}

		if !connected || manualDisc {
			logrus.Info("🛑 [Windows] Stopping monitoring: disconnected or manual disconnect")
			return
		}

		msg := bus.TimedPop(100 * time.Millisecond)
		if msg == nil {
			continue
		}

		switch msg.Type() {
		case gst.MessageEOS:
			logrus.Warn("⚠️ [Windows] GStreamer: end of stream (EOS)")
			gs.mutex.RLock()
			callback := gs.onStateChanged
			gs.mutex.RUnlock()
			if callback != nil {
				callback("eos")
			}
			// Start reconnect only if no manual disconnect
			gs.mutex.RLock()
			shouldReconnect := !gs.manualDisconnect && gs.autoReconnect
			gs.mutex.RUnlock()
			if shouldReconnect {
				go gs.attemptReconnect()
			}

		case gst.MessageError:
			err := msg.ParseError()
			logrus.Errorf("❌ [Windows] GStreamer error: %v", err)
			gs.mutex.Lock()
			if gs.videoMode == models.VideoModeJPEGRTP && strings.Contains(fmt.Sprint(err), "Internal data stream error") {
				gs.jpegCandidateIndex++
				logrus.Warnf("⚠️ [Windows/JPEG] Decoder %s failed at runtime, switching to next JPEG candidate (index=%d)", gs.lastJPEGPipeline, gs.jpegCandidateIndex)
			}
			gs.mutex.Unlock()
			gs.mutex.RLock()
			errCallback := gs.onError
			gs.mutex.RUnlock()
			if errCallback != nil {
				errCallback(fmt.Errorf("%v", err))
			}
			// Start reconnect only if no manual disconnect
			gs.mutex.RLock()
			shouldReconnect := !gs.manualDisconnect && gs.autoReconnect
			gs.mutex.RUnlock()
			if shouldReconnect {
				go gs.attemptReconnect()
			}

		case gst.MessageWarning:
			warn := msg.ParseWarning()
			logrus.Warnf("⚠️ [Windows] GStreamer warning: %v", warn)

		case gst.MessageStateChanged:
			old, new := msg.ParseStateChanged()
			logrus.Debugf("🔄 [Windows] GStreamer state: %s -> %s", old.String(), new.String())
			gs.mutex.RLock()
			stateCallback := gs.onStateChanged
			gs.mutex.RUnlock()
			if stateCallback != nil {
				stateCallback(new.String())
			}
		}

		// DO NOT call msg.Unref() - message is freed automatically!
	}
}

// Disconnect disconnects from RTP/UDP stream
func (gs *GStreamerService) Disconnect() error {
	gs.mutex.Lock()

	if !gs.isConnected && !gs.isConnecting {
		goroutinesRunning := gs.frameProcessorRunning || gs.monitorRunning
		if !goroutinesRunning {
			gs.mutex.Unlock()
			logrus.Info("🔌 [Windows] Disconnect: already disconnected")
			return nil
		}
		logrus.Warn("⚠️ [Windows] Disconnect: not marked connected but goroutines still running, forcing cleanup")
	}

	logrus.Info("🔌 [Windows] Disconnecting from RTP/UDP stream...")

	gs.manualDisconnect = true

	// Set stop flags BEFORE pipeline stop
	gs.isConnected = false
	gs.isConnecting = false

	// Save pipeline before unlock
	pipeline := gs.pipeline
	stopChan := gs.stopChan
	gs.mutex.Unlock()

	// Signal goroutines to stop by closing stopChan
	if stopChan != nil {
		select {
		case <-stopChan: // Channel already closed
			logrus.Info("🔌 [Windows] stopChan already closed")
		default:
			close(stopChan)
			logrus.Info("🔌 [Windows] stopChan closed")
		}
	}

	// Send EOS for graceful pipeline finish
	if pipeline != nil {
		logrus.Info("🛑 [Windows] Sending EOS to GStreamer pipeline...")
		pipeline.SendEvent(gst.NewEOSEvent())
	}

	// Wait for goroutines to finish (max 2 seconds)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gs.mutex.RLock()
		running := gs.frameProcessorRunning || gs.monitorRunning
		gs.mutex.RUnlock()

		if !running {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Check if goroutines finished
	gs.mutex.RLock()
	stillRunning := gs.frameProcessorRunning || gs.monitorRunning
	gs.mutex.RUnlock()

	if stillRunning {
		logrus.Warn("⚠️ [Windows] Some goroutines didn't finish within 2 seconds, continuing...")
	}

	// Stop pipeline
	gs.mutex.Lock()
	if gs.pipeline != nil {
		logrus.Info("🛑 [Windows] Stopping GStreamer pipeline...")
		gs.pipeline.SetState(gst.StateNull)

		// Small delay for proper StateNull transition
		time.Sleep(100 * time.Millisecond)

		// DO NOT call Unref() - pipeline is freed automatically upon SetState(StateNull)
		gs.pipeline = nil
		logrus.Info("✅ [Windows] GStreamer pipeline stopped")
	}

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
		logrus.Info("✅ [Windows] Frame channel cleared")
	}
	gs.mutex.Unlock()

	logrus.Info("✅ [Windows] GStreamer connection closed")
	return nil
}

// attemptReconnect attempts to reconnect to RTP/UDP stream
func (gs *GStreamerService) attemptReconnect() {
	gs.mutex.Lock()

	// Check conditions for reconnection
	if !gs.autoReconnect || gs.manualDisconnect || gs.isConnecting || gs.isReconnecting {
		gs.mutex.Unlock()
		logrus.Infof("🔄 [Windows] Reconnection skipped: autoReconnect=%v, manualDisconnect=%v, isConnecting=%v, isReconnecting=%v",
			gs.autoReconnect, gs.manualDisconnect, gs.isConnecting, gs.isReconnecting)
		return
	}

	if gs.reconnectAttempts >= gs.maxReconnectAttempts {
		logrus.Errorf("❌ [Windows] Maximum reconnection attempts reached (%d)", gs.maxReconnectAttempts)
		gs.autoReconnect = false
		gs.mutex.Unlock()
		return
	}

	// Set reconnection flag to avoid multiple attempts
	gs.isReconnecting = true
	gs.isConnected = false
	gs.isConnecting = false

	// Forcibly clear old pipeline before reconnection
	oldPipeline := gs.pipeline
	stopChan := gs.stopChan
	gs.pipeline = nil
	gs.mutex.Unlock()

	logrus.Info("🧹 [Windows] Cleaning up old pipeline before reconnection...")

	// Close stopChan to stop goroutines
	if stopChan != nil {
		select {
		case <-stopChan:
			logrus.Info("🔌 [Windows] stopChan was already closed")
		default:
			close(stopChan)
			logrus.Info("🔌 [Windows] stopChan closed for reconnection")
		}
	}

	// Stop old pipeline
	if oldPipeline != nil {
		logrus.Info("🛑 [Windows] Stopping old pipeline...")
		oldPipeline.SetState(gst.StateNull)
		time.Sleep(200 * time.Millisecond) // Increased delay for full stop
		logrus.Info("✅ [Windows] Old pipeline stopped")
	}

	// Wait for goroutines to finish
	logrus.Info("⏳ [Windows] Waiting for goroutines to finish...")
	deadline := time.Now().Add(3 * time.Second) // Increased to 3 seconds
	for time.Now().Before(deadline) {
		gs.mutex.RLock()
		running := gs.frameProcessorRunning || gs.monitorRunning
		gs.mutex.RUnlock()

		if !running {
			logrus.Info("✅ [Windows] All goroutines finished")
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Check if goroutines finished
	gs.mutex.RLock()
	stillRunning := gs.frameProcessorRunning || gs.monitorRunning
	gs.mutex.RUnlock()

	if stillRunning {
		logrus.Warn("⚠️ [Windows] Some goroutines are still running, but continuing reconnection")
	}

	gs.mutex.Lock()
	gs.reconnectAttempts++
	attempt := gs.reconnectAttempts
	maxAttempts := gs.maxReconnectAttempts
	gs.mutex.Unlock()

	logrus.Infof("🔄 [Windows] GStreamer reconnection attempt #%d/%d...", attempt, maxAttempts)

	// Delay before reconnection (increases with each attempt)
	delay := time.Duration(attempt) * 2 * time.Second
	if delay > 10*time.Second {
		delay = 10 * time.Second // Max 10 seconds
	}
	logrus.Infof("⏳ [Windows] Delay before reconnection: %v", delay)
	time.Sleep(delay)

	gs.mutex.RLock()
	abortReconnect := !gs.autoReconnect || gs.manualDisconnect
	gs.mutex.RUnlock()
	if abortReconnect {
		logrus.Info("🛑 [Windows] Reconnection cancelled before new ConnectToRTP")
		gs.mutex.Lock()
		gs.isReconnecting = false
		gs.mutex.Unlock()
		return
	}

	if err := gs.ConnectToRTP(); err != nil {
		logrus.Errorf("❌ [Windows] GStreamer reconnection attempt #%d failed: %v", attempt, err)
		gs.mutex.Lock()
		gs.isReconnecting = false
		gs.mutex.Unlock()

		// Try again if not reached limit
		if attempt < maxAttempts {
			logrus.Info("🔄 [Windows] Next reconnection attempt scheduled...")
			// Start next attempt asynchronously
			go gs.attemptReconnect()
		}
	} else {
		logrus.Info("✅ [Windows] GStreamer reconnection successful!")
		gs.mutex.Lock()
		gs.reconnectAttempts = 0
		gs.isReconnecting = false
		gs.mutex.Unlock()
	}
}

// SetOnFrameReceived sets callback for receiving frames
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

// UpdateVideoPort updates video stream port (RTP/UDP)
func (gs *GStreamerService) UpdateVideoPort(port int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.VideoUDPPort = port
	logrus.Debugf("🔧 [Windows] GStreamer service: video UDP port updated to %d", port)
}

// UpdateVideoUDPPort updates video UDP reception port
func (gs *GStreamerService) UpdateVideoUDPPort(port int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.VideoUDPPort = port
	logrus.Debugf("🔧 [Windows] GStreamer service: video UDP port updated to %d", port)
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
		gs.expectedWidth = width
	}
	if height > 0 {
		gs.expectedHeight = height
	}
}

// GetConfig returns configuration
func (gs *GStreamerService) GetConfig() *models.AppConfig {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()
	return gs.config
}

func (gs *GStreamerService) SupportsNativeFullscreen() bool {
	return true
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
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}

	candidates := gs.nativeFullscreenCandidates(bindHost, udpPort)
	var lastErr error
	for _, candidate := range candidates {
		cmd, err := gs.startNativeFullscreenProcess(candidate)
		if err == nil {
			gs.mutex.Lock()
			gs.nativeFullscreenCmd = cmd
			gs.nativeFullscreenActive = true
			gs.mutex.Unlock()
			logrus.Infof("✅ [Windows] native fullscreen started via %s", candidate.name)
			return nil
		}
		lastErr = err
		logrus.Warnf("⚠️ [Windows] native fullscreen candidate %s failed: %v", candidate.name, err)
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

	_ = cmd.Process.Kill()
	return nil
}

type nativeFullscreenCandidate struct {
	name string
	args []string
}

func (gs *GStreamerService) nativeFullscreenCandidates(bindHost string, udpPort int) []nativeFullscreenCandidate {
	base := []string{
		"-q",
		"udpsrc",
		fmt.Sprintf("address=%s", bindHost),
		fmt.Sprintf("port=%d", udpPort),
		"buffer-size=65536",
	}

	if gs.videoMode == models.VideoModeJPEGRTP {
		base = append(base,
			`caps=application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26`,
			"!",
			"rtpjitterbuffer", "latency=10", "faststart-min-packets=1", "drop-on-latency=true",
			"!",
			"rtpjpegdepay",
			"!",
			"jpegparse",
			"!",
			"decodebin",
			"!",
			"queue", "max-size-buffers=2", "leaky=downstream",
			"!",
		)
	} else if gs.videoMode == models.VideoModeRawYUYV {
		width := gs.expectedWidth
		height := gs.expectedHeight
		if width <= 0 {
			width = gs.config.VideoWidth
		}
		if height <= 0 {
			height = gs.config.VideoHeight
		}
		if width <= 0 {
			width = 1280
		}
		if height <= 0 {
			height = 720
		}
		base = append(base,
			fmt.Sprintf(`caps=application/x-rtp,media=video,clock-rate=90000,encoding-name=RAW,sampling=YCbCr-4:2:2,depth=8,width=%d,height=%d,colorimetry=BT601-5,payload=96`, width, height),
			"!",
			"rtpjitterbuffer", "latency=10", "faststart-min-packets=1", "drop-on-latency=true",
			"!",
			"rtpvrawdepay",
			"!",
			"d3d11upload",
			"!",
			"d3d11convert",
			"!",
			"queue", "max-size-buffers=2", "leaky=downstream",
			"!",
		)
	} else {
		base = append(base,
			`caps=application/x-rtp,media=video,encoding-name=H264,payload=96`,
			"!",
			"rtpjitterbuffer", "latency=10", "faststart-min-packets=1", "drop-on-latency=true",
			"!",
			"rtph264depay",
			"!",
			"h264parse", "config-interval=-1",
			"!",
			"decodebin",
			"!",
			"queue", "max-size-buffers=2", "leaky=downstream",
			"!",
		)
	}

	return []nativeFullscreenCandidate{
		{name: "d3d11videosink-fullscreen", args: append(append([]string{}, base...), "d3d11videosink", "fullscreen-toggle-mode=property", "fullscreen=true", "sync=false")},
		{name: "d3d11videosink", args: append(append([]string{}, base...), "d3d11videosink", "sync=false")},
		{name: "glimagesink", args: append(append([]string{}, base...), "glimagesink", "fullscreen=true", "sync=false")},
		{name: "autovideosink", args: append(append([]string{}, base...), "autovideosink", "sync=false")},
	}
}

func (gs *GStreamerService) startNativeFullscreenProcess(candidate nativeFullscreenCandidate) (*exec.Cmd, error) {
	path, err := findBundledGStreamerTool("gst-launch-1.0")
	if err != nil {
		return nil, fmt.Errorf("gst-launch-1.0 not found: %w", err)
	}

	cmd := exec.Command(path, candidate.args...)
	maybeHideWindow(cmd)
	cmd.Env = getWindowsNativeFullscreenEnv(path)
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr

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
			err = fmt.Errorf("native fullscreen process exited immediately")
		}
		if details := strings.TrimSpace(stderr.String()); details != "" {
			err = fmt.Errorf("%w: %s", err, details)
		}
		return nil, err
	case <-time.After(1200 * time.Millisecond):
		return cmd, nil
	}
}

func findBundledGStreamerTool(name string) (string, error) {
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(exeDir, "bin", name+".exe"),
			filepath.Join(exeDir, "bin", name),
			filepath.Join(exeDir, name+".exe"),
			filepath.Join(exeDir, name),
		}
		for _, candidate := range candidates {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}

	return exec.LookPath(name)
}

func getWindowsNativeFullscreenEnv(gstLaunchPath string) []string {
	env := os.Environ()
	for _, pluginDir := range nativeFullscreenPluginDirs(gstLaunchPath) {
		env = appendOrPrependEnv(env, "GST_PLUGIN_PATH", pluginDir)
		env = appendOrPrependEnv(env, "GST_PLUGIN_SYSTEM_PATH", pluginDir)
		logrus.Infof("🔧 [Windows] native fullscreen GST plugin dir: %s", pluginDir)
	}

	baseDir := filepath.Dir(filepath.Clean(gstLaunchPath))
	scannerCandidates := []string{
		filepath.Join(baseDir, "..", "libexec", "gstreamer-1.0", "gst-plugin-scanner.exe"),
		filepath.Join(baseDir, "..", "libexec", "gstreamer-1.0", "gst-plugin-scanner"),
		filepath.Join(baseDir, "gst-plugin-scanner.exe"),
		filepath.Join(baseDir, "gst-plugin-scanner"),
	}
	for _, scannerPath := range scannerCandidates {
		if info, err := os.Stat(scannerPath); err == nil && !info.IsDir() {
			env = appendOrPrependEnv(env, "GST_PLUGIN_SCANNER", scannerPath)
			logrus.Infof("🔧 [Windows] native fullscreen GST plugin scanner: %s", scannerPath)
			break
		}
	}

	env = appendOrPrependEnv(env, "PATH", baseDir)
	return env
}

func nativeFullscreenPluginDirs(gstLaunchPath string) []string {
	baseDir := filepath.Dir(filepath.Clean(gstLaunchPath))
	patterns := []string{
		filepath.Join(baseDir, "..", "lib", "gstreamer-1.0"),
		filepath.Join(baseDir, "..", "lib64", "gstreamer-1.0"),
		filepath.Join(baseDir, "..", "lib", "*", "gstreamer-1.0"),
		filepath.Join(baseDir, "..", "lib64", "*", "gstreamer-1.0"),
	}

	var dirs []string
	seen := make(map[string]struct{})
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Clean(pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || !info.IsDir() {
				continue
			}
			if _, exists := seen[match]; exists {
				continue
			}
			seen[match] = struct{}{}
			dirs = append(dirs, match)
		}
	}
	return dirs
}

func appendOrPrependEnv(env []string, key, value string) []string {
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
	gs.reconnectAttempts = 0 // Reset counter when max changes
}

// Reconnect forcibly reconnects to RTP/UDP stream (for device switching)
func (gs *GStreamerService) Reconnect() error {
	logrus.Info("🔄 [Windows] Forced reconnection (device switch)...")

	// Disconnect first
	if err := gs.Disconnect(); err != nil {
		logrus.Warnf("⚠️ [Windows] Error disconnecting before reconnection: %v", err)
	}

	// Wait a bit for proper disconnection
	time.Sleep(500 * time.Millisecond)

	// Reset reconnection attempt counter
	gs.mutex.Lock()
	gs.reconnectAttempts = 0
	gs.autoReconnect = true
	gs.manualDisconnect = false
	gs.mutex.Unlock()

	// Reconnect
	logrus.Info("🔗 [Windows] Connecting to new device...")
	return gs.ConnectToRTP()
}
