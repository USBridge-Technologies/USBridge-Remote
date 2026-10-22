package view

import "testing"

func TestParsePercentRect(t *testing.T) {
	rect, err := parsePercentRect("5, 0, 90, 100")
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{0.05, 0, 0.9, 1}
	for i := range want {
		if d := rect[i] - want[i]; d > 1e-9 || d < -1e-9 {
			t.Fatalf("got %v, want %v", rect, want)
		}
	}
	for _, bad := range []string{"5, 0, 90", "a, b, c, d", "50, 0, 90, 100", "0, 0, 0, 100", "-1, 0, 50, 50"} {
		if _, err := parsePercentRect(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestAspectLabel(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want string
	}{
		{1920, 1080, "16:9"}, {1440, 900, "16:10"}, {1280, 1024, "5:4"}, {1024, 768, "4:3"},
		{3440, 1440, "2.39:1"}, {1366, 768, "1.78:1"}, {1080, 1920, "9:16"},
	} {
		if got := aspectLabel(tc.w, tc.h); got != tc.want {
			t.Errorf("%dx%d: %q, want %q", tc.w, tc.h, got, tc.want)
		}
	}
}
