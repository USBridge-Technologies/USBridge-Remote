//go:build android

package service

/*
extern int android_hevc444_decode_probe(void);
extern int android_amc_444_failed(void);
*/
import "C"

import (
	"sync"

	"github.com/sirupsen/logrus"
)

// Color444DecodeSupported reports whether this device's hardware decoder
// takes an HEVC RExt 4:4:4 stream (VIDEO_FORMAT_H265_REXT8_444).
//
// Android has no HEVC RExt profile in MediaCodecInfo.CodecProfileLevel, so a
// decoder can't advertise it: the answer comes from decoding one 4:4:4 frame
// (android_hevc444_decode_probe, moonlight_cgo_android.go) once per process,
// started at init so the video dialog doesn't wait on it. Most phone decoders
// take 4:2:0 only -- Qualcomm c2.qti.hevc.* (Snapdragon 8 Elite included)
// fail on the first 4:4:4 SPS, and a stream then never shows a picture.
//
// A 4:4:4 session whose decoder still fails before its first frame
// (android_amc_444_failed) turns it off for the rest of the process, so the
// automatic reconnect asks the host for 4:2:0.
func Color444DecodeSupported() bool {
	if C.android_amc_444_failed() != 0 {
		color444RuntimeFailLog.Do(func() {
			logrus.Warnf("🎨 [4:4:4] the decoder failed on a 4:4:4 stream -- asking the host for 4:2:0 from now on")
		})
		return false
	}
	color444Probe.Do(runColor444Probe)
	return color444ProbeOK
}

var (
	color444Probe          sync.Once
	color444ProbeOK        bool
	color444RuntimeFailLog sync.Once
)

func runColor444Probe() {
	r := C.android_hevc444_decode_probe()
	color444ProbeOK = r == 1
	logrus.Infof("🎨 [4:4:4] HEVC 4:4:4 decode probe: result=%d -> supported=%v", int(r), color444ProbeOK)
}

func init() {
	go color444Probe.Do(runColor444Probe)
}
