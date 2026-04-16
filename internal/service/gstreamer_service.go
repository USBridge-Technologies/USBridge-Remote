//go:build !android && !darwin && !windows
// +build !android,!darwin,!windows

package service

import (
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"
)

type GStreamerService struct {
	config    *models.AppConfig
	videoMode string

	cmd    *exec.Cmd
	stdout io.ReadCloser
	
	mutex     sync.RWMutex
	stopChan  chan struct{}
	running   bool
	
	width  int
	height int

	lastFrameTime  time.Time
	frameCount     int64
	framesDropped  int64
	latencyProfile videoLatencyProfile

	onFrameReceived func(image.Image)
	onStateChanged  func(string)
	onError         func(error)
}

func NewGStreamerService(config *models.AppConfig) *GStreamerService {
	return &GStreamerService{
		config:    config,
		videoMode: models.VideoModeH264,
	}
}

func (gs *GStreamerService) ConnectToRTP() error {
	gs.mutex.Lock()
	if gs.running {
		gs.mutex.Unlock()
		return nil
	}
	gs.stopChan = make(chan struct{})
	gs.running = true
	gs.width, gs.height = gs.getDimensions()
	udpPort := gs.getUDPPort()
	gs.mutex.Unlock()

	// ИСПОЛЬЗУЕМ В ТОЧНОСТИ ТВОЙ РАБОЧИЙ КОНВЕЙЕР
	pipeline := fmt.Sprintf("udpsrc port=%d caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "+
		"rtpjitterbuffer latency=100 ! rtph264depay ! h264parse ! avdec_h264 ! "+
		"videoconvert ! video/x-raw,format=RGBA,width=%d,height=%d ! fdsink fd=1 sync=false", 
		udpPort, gs.width, gs.height)

	logrus.Infof("🎬 Running manual-tested pipeline: %s", pipeline)

	cmd := exec.Command("gst-launch-1.0", "-q")
	cmd.Args = append(cmd.Args, "udpsrc", fmt.Sprintf("port=%d", udpPort), 
		"caps=application/x-rtp,media=video,encoding-name=H264,payload=96", "!",
		"rtpjitterbuffer", "latency=100", "!",
		"rtph264depay", "!", "h264parse", "!", "avdec_h264", "!",
		"videoconvert", "!", fmt.Sprintf("video/x-raw,format=RGBA,width=%d,height=%d", gs.width, gs.height), "!",
		"fdsink", "fd=1", "sync=false")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	
	if err := cmd.Start(); err != nil {
		return err
	}

	gs.mutex.Lock()
	gs.cmd = cmd
	gs.stdout = stdout
	gs.mutex.Unlock()

	go gs.readLoop()

	if gs.onStateChanged != nil {
		gs.onStateChanged("connected")
	}
	return nil
}

func (gs *GStreamerService) readLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	gs.mutex.RLock()
	stdout := gs.stdout
	stopChan := gs.stopChan
	w, h := gs.width, gs.height
	gs.mutex.RUnlock()

	frameSize := w * h * 4
	buffer := make([]byte, frameSize)

	for {
		select {
		case <-stopChan:
			return
		default:
			// Читаем строго один кадр. Если GStreamer выдал меньше - это ошибка.
			_, err := io.ReadFull(stdout, buffer)
			if err != nil {
				return
			}

			// Копируем данные в НОВЫЙ объект изображения.
			// Это единственный способ избежать серых кадров в Fyne.
			img := image.NewRGBA(image.Rect(0, 0, w, h))
			copy(img.Pix, buffer)

			producedAt := time.Now()
			gs.mutex.Lock()
			gs.frameCount++
			gs.lastFrameTime = producedAt
			gs.mutex.Unlock()

			if gs.onFrameReceived != nil {
				gs.onFrameReceived(img)
			}
		}
	}
}

func (gs *GStreamerService) Disconnect() error {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	if !gs.running { return nil }
	gs.running = false
	if gs.stopChan != nil { close(gs.stopChan) }
	if gs.cmd != nil && gs.cmd.Process != nil {
		_ = gs.cmd.Process.Kill()
		_ = gs.cmd.Wait()
	}
	gs.cmd = nil
	gs.stdout = nil
	if gs.onStateChanged != nil { gs.onStateChanged("disconnected") }
	return nil
}

func (gs *GStreamerService) getDimensions() (int, int) {
	if gs.config.VideoWidth > 0 { return gs.config.VideoWidth, gs.config.VideoHeight }
	return 1280, 720
}
func (gs *GStreamerService) getUDPPort() int {
	if gs.config.VideoUDPPort > 0 { return gs.config.VideoUDPPort }
	return 55000
}
func (gs *GStreamerService) getBindHost() string {
	host := strings.TrimSpace(gs.config.VideoBindHost)
	if host == "" || host == "127.0.0.1" {
		return "0.0.0.0"
	}
	return host
}

func (gs *GStreamerService) SetOnFrameReceived(cb func(image.Image)) { gs.onFrameReceived = cb }
func (gs *GStreamerService) SetOnStateChanged(cb func(string))      { gs.onStateChanged = cb }
func (gs *GStreamerService) SetOnError(cb func(error))             { gs.onError = cb }
func (gs *GStreamerService) IsConnected() bool                     { gs.mutex.RLock(); defer gs.mutex.RUnlock(); return gs.running }
func (gs *GStreamerService) SetVideoMode(m string)                 { gs.videoMode = m }
func (gs *GStreamerService) UpdateHost(h string)                   {}
func (gs *GStreamerService) UpdateVideoPort(p int)                 { gs.config.VideoUDPPort = p }
func (gs *GStreamerService) UpdateVideoUDPPort(p int)              { gs.config.VideoUDPPort = p }
func (gs *GStreamerService) SetExpectedVideoSize(w, h int)         { gs.width, gs.height = w, h }
func (gs *GStreamerService) Reconnect() error                      { _ = gs.Disconnect(); return gs.ConnectToRTP() }
func (gs *GStreamerService) GetStats() map[string]interface{} {
	gs.mutex.RLock(); defer gs.mutex.RUnlock()
	return map[string]interface{}{"connected": gs.running, "frame_count": gs.frameCount}
}
func (gs *GStreamerService) GetConfig() *models.AppConfig { return gs.config }
func (gs *GStreamerService) SupportsNativeFullscreen() bool { return false }
func (gs *GStreamerService) IsNativeFullscreenActive() bool { return false }
func (gs *GStreamerService) StartNativeFullscreen() error { return nil }
func (gs *GStreamerService) StopNativeFullscreen() error { return nil }
func (gs *GStreamerService) ResetRuntimeDecoderFallback() {}
func (gs *GStreamerService) SetAutoReconnect(b bool) {}
func (gs *GStreamerService) SetMaxReconnectAttempts(i int) {}
func (gs *GStreamerService) ConnectToUDPViaPipe(f *os.File) error { return nil }
func (gs *GStreamerService) GetBindHost() string { return gs.getBindHost() }
