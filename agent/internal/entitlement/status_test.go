package entitlement

import "testing"

func TestStatusProtocol(t *testing.T) {
	t.Parallel()
	cases := []struct {
		backend, tier, want string
	}{
		{"sunshine", "", "opensource"},
		{"sunshine", "pro", "opensource"},
		{"", "enterprise", "opensource"},
		{"rustshine", "", "free"},
		{"rustshine", "free", "free"},
		{"rustshine", "Pro", "pro"},
		{"rustshine", "enterprise", "enterprise"},
	}
	for _, tc := range cases {
		got := Status{ActiveBackend: tc.backend, Tier: tc.tier}.Protocol()
		if got != tc.want {
			t.Fatalf("backend=%q tier=%q: got %q, want %q", tc.backend, tc.tier, got, tc.want)
		}
	}
}
