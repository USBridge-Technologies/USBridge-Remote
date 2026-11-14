package gui

import "testing"

func TestInheritAPIPort(t *testing.T) {
	cases := []struct{ host, from, want string }{
		{"100.127.250.21", "192.168.200.29:8081", "100.127.250.21:8081"},
		{"100.127.250.21:9000", "192.168.200.29:8081", "100.127.250.21:9000"},
		{"100.127.250.21", "192.168.200.29", "100.127.250.21"},
		{"agent.tail1.ts.net", "agent.device.usbridge.io:8081", "agent.tail1.ts.net:8081"},
		{"fd7a:115c:a1e0::1", "192.168.200.29:8081", "[fd7a:115c:a1e0::1]:8081"},
		{"", "192.168.200.29:8081", ""},
	}
	for _, c := range cases {
		if got := inheritAPIPort(c.host, c.from); got != c.want {
			t.Errorf("inheritAPIPort(%q, %q) = %q, want %q", c.host, c.from, got, c.want)
		}
	}
}
