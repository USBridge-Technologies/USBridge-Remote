package api

import "testing"

func TestPictureShape(t *testing.T) {
	for _, c := range []struct {
		goos, device, want string
	}{
		{"windows", "winid:{abc}", PictureShapeSource},
		{"windows", "virtual:2560x1600@120", PictureShapeSource},
		{"linux", "drm:1", PictureShapeDesktop},
		{"linux", "raw:/dev/dri/card1|HDMI-A-1", PictureShapeDesktop},
		{"linux", "/dev/video0", ""},
		{"darwin", "display:0", ""},
	} {
		if got := pictureShape(c.goos, c.device); got != c.want {
			t.Errorf("pictureShape(%q, %q) = %q, want %q", c.goos, c.device, got, c.want)
		}
	}
}
