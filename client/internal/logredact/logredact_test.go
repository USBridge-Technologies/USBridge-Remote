package logredact

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	cases := []struct{ in, gone, keep string }{
		{`raw: <root><pairingsecret>a1b2c3d4e5f60718293a4b5c6d7e8f90</pairingsecret></root>`, "e5f60718293a4b5c6d", "a1b2…8f90"},
		{`raw: <root><challengeresponse>00112233445566778899aabbccddeeff</challengeresponse>`, "445566778899", "0011…eeff"},
		{`<plaincert>2d2d2d2d2d424547494e2043455254</plaincert>`, "", "2d2d2d2d2d424547494e2043455254"},
		{`New AuthURL: https://login.tailscale.com/a/226e5fb0154ed9`, "5fb0154", "login.tailscale.com/a/226e…4ed9"},
		{`key tskey-auth-kXYZ123456-ABCDEFGHIJKLMNOP`, "123456-ABCDEFGH", "tske…MNOP"},
		{`body {"token":"abcdefghijklmnopqrstuvwxyz","name":"x"}`, "ghijklmnopqrst", `"token":"abcd…wxyz"`},
		{`url /pair?devicename=x&salt=0123456789abcdef0123&clientcert=2d2d`, "456789abcdef", "salt=0123…0123&clientcert=2d2d"},
		{`Authorization: Bearer abcdef1234567890ghijkl`, "1234567890", "Bearer abcd…ijkl"},
		{"-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----", "MIIEow", "[redacted]"},
		{`magicsock: disco key = d:62b0f1a2c3d4e5f6`, "", "d:62b0f1a2c3d4e5f6"},
	}
	for _, c := range cases {
		got := String(c.in)
		if c.gone != "" && strings.Contains(got, c.gone) {
			t.Errorf("%q: secret left in %q", c.in, got)
		}
		if !strings.Contains(got, c.keep) {
			t.Errorf("%q: want %q in %q", c.in, c.keep, got)
		}
		if String(got) != got {
			t.Errorf("%q: not idempotent: %q", c.in, String(got))
		}
	}
}
