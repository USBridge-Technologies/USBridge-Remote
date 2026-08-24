package tailscale

import "testing"

// TestSkipRegisterAlreadyRunning reproduces the bug behind "agent hangs after
// a few Tailscale connect/disconnect cycles": /api/auth/sync calls
// Register("", hostname) on every client reconnect, and without this
// short-circuit that unconditionally re-ran Start + StartLoginInteractive
// even on an already-Running node -- which tsnet doesn't treat as a no-op,
// occasionally interrupting its in-flight control-plane cycle badly enough
// that it regenerates the node's WireGuard key mid-session, silently
// breaking every peer's already-established handshake. See Register's doc
// comment for the full confirmed-live trace.
func TestSkipRegisterAlreadyRunning(t *testing.T) {
	cases := []struct {
		name    string
		authKey string
		status  *Status
		want    bool
	}{
		{
			name:    "no authKey, already logged in and running: skip",
			authKey: "",
			status:  &Status{LoggedIn: true, Running: true},
			want:    true,
		},
		{
			name:    "no authKey, not logged in: must register",
			authKey: "",
			status:  &Status{LoggedIn: false, Running: false},
			want:    false,
		},
		{
			name:    "no authKey, logged in but not yet Running: must register",
			authKey: "",
			status:  &Status{LoggedIn: true, Running: false},
			want:    false,
		},
		{
			name:    "explicit authKey always re-registers, even if already running",
			authKey: "tskey-auth-abc123",
			status:  &Status{LoggedIn: true, Running: true},
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := skipRegister(tc.authKey, tc.status)
			if got != tc.want {
				t.Errorf("skipRegister(%q, %+v) = %v, want %v", tc.authKey, tc.status, got, tc.want)
			}
		})
	}
}
