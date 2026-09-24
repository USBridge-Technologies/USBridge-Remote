package webrtcweb

import "strings"

// answerVideoCodec returns the video codec rustshine's SDP answer put on its
// video m-line -- "h265", "h264", "av1", or "" if none is recognizable. The
// answer carries exactly one video codec (plus RTX), so the first rtpmap in
// that section is the one actually streaming.
func answerVideoCodec(sdp string) string {
	inVideo := false
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "m=") {
			inVideo = strings.HasPrefix(line, "m=video ")
			continue
		}
		if !inVideo || !strings.HasPrefix(line, "a=rtpmap:") {
			continue
		}
		_, encoding, ok := strings.Cut(strings.TrimPrefix(line, "a=rtpmap:"), " ")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(encoding, "/")
		switch strings.ToUpper(name) {
		case "H265", "HEVC":
			return "h265"
		case "H264":
			return "h264"
		case "AV1":
			return "av1"
		}
	}
	return ""
}
