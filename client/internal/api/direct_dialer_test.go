package api

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.1:8080", true},
		{"localhost", true},
		{"localhost:8080", true},
		{"::1", true},
		{"192.168.1.100", false},
		{"192.168.1.100:8080", false},
		{"100.104.80.51", false}, // Tailscale CGNAT range, not loopback
		{"example.com", false},
	}
	for _, c := range cases {
		if got := isLoopbackHost(c.host); got != c.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

// This is the exact bug: buildDirectDialer used to force every socket onto
// the physical LAN interface via IP_BOUND_IF (darwin) / a LAN source IP
// (other platforms), including ones destined for 127.0.0.1 -- which can
// never be reached that way, so the connection failed outright with
// EADDRNOTAVAIL ("can't assign requested address"). Loopback destinations
// must get a plain, unconstrained dialer.
func TestBuildDirectDialer_LoopbackGetsUnconstrainedDialer(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "127.0.0.1:8080"} {
		d := buildDirectDialer(host)
		if d.Control != nil {
			t.Errorf("buildDirectDialer(%q).Control should be nil (no IP_BOUND_IF forcing) for a loopback destination", host)
		}
		if d.LocalAddr != nil {
			t.Errorf("buildDirectDialer(%q).LocalAddr should be nil (no LAN source IP forcing) for a loopback destination", host)
		}
	}
}
