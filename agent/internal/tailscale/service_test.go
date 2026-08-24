package tailscale

import (
	"os"
	"path/filepath"
	"testing"
)

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

// TestDirIsWritable_TrueForARealDirectory and its sibling below pin
// dirIsWritable's actual contract: it's not just "does MkdirAll return an
// error", it's a real write probe -- see resolveWritableStateDir's own doc
// comment for why that distinction is the whole point (a broken-ACL
// directory that already exists makes MkdirAll a silent no-op success).
func TestDirIsWritable_TrueForARealDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub")
	if !dirIsWritable(dir) {
		t.Fatal("expected a freshly creatable temp subdirectory to be writable")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("dirIsWritable should have created %s: %v", dir, err)
	}
}

func TestDirIsWritable_FalseWhenAFileBlocksTheDirectory(t *testing.T) {
	// A regular file sitting at the exact path a directory is wanted makes
	// os.MkdirAll itself fail (ENOTDIR) -- the simplest portable stand-in
	// for "this path exists but isn't a directory this process can write
	// into", the same class of failure (existing-but-unusable) a broken-ACL
	// directory produces in the real bug this guards against, without
	// needing a real permission-denial trick that would be fragile across
	// platforms/CI.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if dirIsWritable(blocker) {
		t.Fatal("expected a path blocked by an existing regular file to be reported unwritable")
	}
}

// TestResolveWritableStateDir_FallsBackWhenPrimaryIsUnusable reproduces the
// exact bug this mechanism exists to fix: a real machine had
// %AppData%/usbridge-agent/tailscale already existing with broken
// permissions (confirmed live: even Get-Acl/takeown from the same user
// account failed with "Access is denied", unfixable without an admin
// elevation this process never has) -- os.MkdirAll alone reported success
// against it every time (it already existed), so the old code kept
// handing tsnet.Server.Start() the same permanently-unusable directory
// forever, and Sign In could never produce an AuthURL. Simulated here the
// same portable way TestDirIsWritable_FalseWhenAFileBlocksTheDirectory
// does (a file blocking the primary path) rather than a real ACL trick.
func TestResolveWritableStateDir_FallsBackWhenPrimaryIsUnusable(t *testing.T) {
	base := t.TempDir()
	primary := filepath.Join(base, "tailscale")
	if err := os.WriteFile(primary, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	s := &Service{stateDir: base}
	got := s.resolveWritableStateDir()

	want := filepath.Join(base, "tailscale2")
	if got != want {
		t.Errorf("resolveWritableStateDir() = %q, want fallback %q", got, want)
	}
	if !dirIsWritable(got) {
		t.Errorf("resolveWritableStateDir() returned %q which isn't actually writable", got)
	}
}

func TestResolveWritableStateDir_UsesPrimaryWhenUsable(t *testing.T) {
	base := t.TempDir()
	s := &Service{stateDir: base}

	got := s.resolveWritableStateDir()

	want := filepath.Join(base, "tailscale")
	if got != want {
		t.Errorf("resolveWritableStateDir() = %q, want primary %q", got, want)
	}
}
