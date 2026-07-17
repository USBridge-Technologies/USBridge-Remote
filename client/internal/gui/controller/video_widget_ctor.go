package controller

import (
	"usbridge-client/internal/api"
	"usbridge-client/internal/media"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// NewVideoWidget creates a VideoWidget with a Moonlight-only video and input path.
func NewVideoWidget(parent fyne.Window, usbClient *api.USBClient, videoClient service.VideoClient, updateStatus func()) *VideoWidget {
	vw := &VideoWidget{
		usbClient:    usbClient,
		videoClient:  videoClient,
		updateStatus: updateStatus,
		frameDecoder: media.NewFrameDecoder(),
	}

	if videoClient != nil {
		videoClient.SetOnFrameReceived(vw.handleVideoFrame)
		videoClient.SetOnStateChanged(func(state string) {
			logrus.Infof("🎬 [VideoWidget] video state changed: %s", state)
			switch state {
			case "disconnected", "error", "stopped":
				vw.isStreaming = false
				vw.isVideoConnected = false
				// Tear down the native overlay immediately instead of leaving it frozen
				// on its last rendered frame. On Android the Vulkan SurfaceView keeps its
				// own render thread looping (repainting the same last-good frame, FPS
				// counter still climbing) until explicitly destroyed — without this the
				// screen looks like a live, working stream for as long as the reconcile
				// loop's reconnect attempts take (or forever, if they all fail), which
				// reads as "the video is stuck" rather than "the stream disconnected".
				// clearVideo() destroys the overlay and shows a darkened last frame via
				// the Fyne canvas instead, an unambiguous "stopped" visual.
				go vw.clearVideo()
				vw.scheduleVideoReconcile("state-" + state)
			case "connected", "streaming":
				// Ignore stale "connected" callbacks that arrive after the user
				// disconnected — they would otherwise start the Metal/Vulkan overlay
				// on top of the connection-manager screen.
				if vw.isClosing.Load() || !vw.desiredStreamingState() {
					logrus.Infof("🎬 [VideoWidget] ignoring stale state=%s (closing=%v desired=%v)", state, vw.isClosing.Load(), vw.desiredStreamingState())
					return
				}
				vw.isVideoConnected = true
			}
		})
		videoClient.SetOnError(func(err error) {
			logrus.Errorf("🎬 [VideoWidget] video error: %v", err)
		})
	}

	vw.createInterface()
	if parent != nil {
		vw.SetParentWindow(parent)
	}
	vw.startVideoOpsLoop()

	return vw
}
