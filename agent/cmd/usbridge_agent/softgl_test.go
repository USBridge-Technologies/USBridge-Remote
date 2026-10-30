package main

import "testing"

func TestGLVersionAtLeast(t *testing.T) {
	for _, c := range []struct {
		version string
		want    bool
	}{
		{"1.1.0", false},
		{"2.0 Mesa", false},
		{"2.1 Mesa 26.2.4", true},
		{"4.6.0 NVIDIA 560.94", true},
		{"3.3.0 - Build 31.0.101.5186", true},
		{"", false},
		{"garbage", false},
	} {
		if got := glVersionAtLeast(c.version, 2, 1); got != c.want {
			t.Errorf("glVersionAtLeast(%q) = %v, want %v", c.version, got, c.want)
		}
	}
}
