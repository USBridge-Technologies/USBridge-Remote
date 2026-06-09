//go:build !android || gstreamer

package service

import (
	"image"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	videoLatencyLogEverySamples = 600
	videoLatencyLogMinInterval  = 20 * time.Second
)

type videoFramePacket struct {
	img  image.Image
	meta videoLatencyFrameMeta
}

type videoLatencyFrameMeta struct {
	producedAt  time.Time
	rtpAge      time.Duration
	appsinkAge  time.Duration
	copyTime    time.Duration
	frameWidth  int
	frameHeight int
}

type videoLatencyProfile struct {
	windowStarted time.Time
	samples       int64
	sumRTPAge     time.Duration
	sumAppsinkAge time.Duration
	sumCopyTime   time.Duration
	sumUIDelay    time.Duration
	maxRTPAge     time.Duration
	maxAppsinkAge time.Duration
	maxCopyTime   time.Duration
	maxUIDelay    time.Duration
}

func (gs *GStreamerService) recordIngressLatency(meta videoLatencyFrameMeta) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	if gs.latencyProfile.windowStarted.IsZero() {
		gs.latencyProfile.windowStarted = time.Now()
	}
	gs.latencyProfile.samples++
	gs.latencyProfile.sumRTPAge += meta.rtpAge
	gs.latencyProfile.sumAppsinkAge += meta.appsinkAge
	gs.latencyProfile.sumCopyTime += meta.copyTime
	if meta.rtpAge > gs.latencyProfile.maxRTPAge {
		gs.latencyProfile.maxRTPAge = meta.rtpAge
	}
	if meta.appsinkAge > gs.latencyProfile.maxAppsinkAge {
		gs.latencyProfile.maxAppsinkAge = meta.appsinkAge
	}
	if meta.copyTime > gs.latencyProfile.maxCopyTime {
		gs.latencyProfile.maxCopyTime = meta.copyTime
	}
}

func (gs *GStreamerService) recordUIDelay(uiDelay time.Duration, meta videoLatencyFrameMeta, platform string) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	if gs.latencyProfile.windowStarted.IsZero() {
		gs.latencyProfile.windowStarted = time.Now()
	}
	gs.latencyProfile.sumUIDelay += uiDelay
	if uiDelay > gs.latencyProfile.maxUIDelay {
		gs.latencyProfile.maxUIDelay = uiDelay
	}

	if gs.latencyProfile.samples == 0 {
		return
	}
	if gs.latencyProfile.samples%videoLatencyLogEverySamples != 0 && time.Since(gs.latencyProfile.windowStarted) < videoLatencyLogMinInterval {
		return
	}

	samples := gs.latencyProfile.samples
	logrus.Infof(
		"📊 [VideoLatency][%s] samples=%d frame=%dx%d rtp->client_avg=%s rtp->client_max=%s appsink->client_avg=%s appsink->client_max=%s copy_avg=%s copy_max=%s client->ui_avg=%s client->ui_max=%s",
		platform,
		samples,
		meta.frameWidth,
		meta.frameHeight,
		gs.latencyProfile.sumRTPAge/time.Duration(samples),
		gs.latencyProfile.maxRTPAge,
		gs.latencyProfile.sumAppsinkAge/time.Duration(samples),
		gs.latencyProfile.maxAppsinkAge,
		gs.latencyProfile.sumCopyTime/time.Duration(samples),
		gs.latencyProfile.maxCopyTime,
		gs.latencyProfile.sumUIDelay/time.Duration(samples),
		gs.latencyProfile.maxUIDelay,
	)
	gs.latencyProfile = videoLatencyProfile{}
}
