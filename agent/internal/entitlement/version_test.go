package entitlement

import "testing"

func TestVersionOlder(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"usbridge-streamer-v0.3.146", "usbridge-streamer-v0.3.147", true},
		{"usbridge-streamer-v0.3.147", "usbridge-streamer-v0.3.146", false},
		{"usbridge-streamer-v0.3.147", "usbridge-streamer-v0.3.147", false},
		{"usbridge-streamer-v0.3.99", "usbridge-streamer-v0.3.100", true},
		{"usbridge-streamer-v0.3.146", "", false},
		{"weird", "usbridge-streamer-v0.3.1", false},
	}
	for _, c := range cases {
		if got := versionOlder(c.a, c.b); got != c.want {
			t.Errorf("versionOlder(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
