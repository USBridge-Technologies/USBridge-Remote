package service

import "usbridge-client/internal/models"

// videoFormatCodecName maps a VIDEO_FORMAT_* bitmask (Limelight.h) to the
// client's codec name constants ("h264"/"h265"/"av1"). Mirrors the bit
// layout moonlightVideoFormat() in moonlight_service.go encodes, plus the
// mask bits the platform CGO files already use to branch HEVC vs H264
// (VIDEO_FORMAT_MASK_H265 = 0x0F00, VIDEO_FORMAT_MASK_AV1 = 0xF000).
func videoFormatCodecName(format int32) (string, bool) {
	switch {
	case format < 0:
		return "", false
	case format&0xF0000 != 0: // VIDEO_FORMAT_MASK_PYROWAVE
		return models.VideoModePyroWave, true
	case format&0x0F00 != 0:
		return models.VideoModeH265, true
	case format&0xF000 != 0:
		return models.VideoModeAV1, true
	case format&0x00FF != 0:
		return models.VideoModeH264, true
	default:
		return "", false
	}
}
