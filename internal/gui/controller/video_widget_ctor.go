package controller

import (
	"usbridge-client/internal/api"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// NewVideoWidget создаёт VideoWidget с Moonlight-only путём видео и ввода.
func NewVideoWidget(parent fyne.Window, usbClient *api.USBClient, videoClient service.VideoClient, updateStatus func()) *VideoWidget {
	vw := &VideoWidget{
		parentWindow: parent,
		usbClient:    usbClient,
		videoClient:  videoClient,
		updateStatus: updateStatus,
		videoOps:     make(chan videoOperation, 16),
	}

	if videoClient != nil {
		videoClient.SetOnFrameReceived(vw.handleVideoFrame)
		videoClient.SetOnStateChanged(func(state string) {
			logrus.Infof("🎬 [VideoWidget] video state changed: %s", state)
			switch state {
			case "disconnected", "error", "stopped":
				vw.isStreaming = false
				vw.isVideoConnected = false
				vw.scheduleVideoReconcile("state-" + state)
			case "connected", "streaming":
				vw.isVideoConnected = true
			}
		})
		videoClient.SetOnError(func(err error) {
			logrus.Errorf("🎬 [VideoWidget] video error: %v", err)
		})
	}

	vw.createInterface()
	vw.startVideoOpsLoop()

	return vw
}
