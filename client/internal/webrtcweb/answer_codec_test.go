package webrtcweb

import "testing"

func TestAnswerVideoCodec(t *testing.T) {
	answer := "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=rtpmap:111 opus/48000/2\r\n" +
		"m=video 9 UDP/TLS/RTP/SAVPF 49 50\r\na=rtpmap:50 rtx/90000\r\na=rtpmap:49 H265/90000\r\n"
	if got := answerVideoCodec(answer); got != "h265" {
		t.Fatalf("got %q, want h265", got)
	}
	if got := answerVideoCodec("m=video 9 X 102\r\na=rtpmap:102 H264/90000\r\n"); got != "h264" {
		t.Fatalf("got %q, want h264", got)
	}
	if got := answerVideoCodec("m=audio 9 X 111\r\na=rtpmap:111 opus/48000/2\r\n"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
