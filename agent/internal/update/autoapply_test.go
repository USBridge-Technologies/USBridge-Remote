package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShouldAutoApplyHoldsBackSameVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // os.UserConfigDir on Linux
	t.Setenv("HOME", dir)            // and on macOS
	t.Setenv("AppData", dir)         // and on Windows

	now := time.Unix(1_800_000_000, 0)
	if !shouldAutoApply("3.2.29", now) {
		t.Fatal("first attempt must run")
	}
	if shouldAutoApply("3.2.29", now.Add(30*time.Second)) {
		t.Fatal("same version again right after an attempt must wait")
	}
	if !shouldAutoApply("3.2.30", now.Add(time.Minute)) {
		t.Fatal("a newer version must not wait for the previous one's backoff")
	}
	if !shouldAutoApply("3.2.30", now.Add(time.Minute+autoRetryAfter)) {
		t.Fatal("same version must be retried once the backoff is over")
	}
}

func TestCleanStaleTempRemovesOnlyOldUpdateLeftovers(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := now.Add(-2 * staleTempAge)

	mk := func(name string, dirEntry bool, mod time.Time) string {
		p := filepath.Join(dir, name)
		if dirEntry {
			if err := os.MkdirAll(filepath.Join(p, "sub"), 0o755); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
		return p
	}
	oldZip := mk("usbridge-agent-update-123", false, old)
	oldStaging := mk("usbridge-agent-update-456", true, old)
	fresh := mk("usbridge-agent-update-789", false, now)
	other := mk("usbridge-clipboard-blobs-1", true, old)
	client := mk("usbridge-client-update-1", false, old)

	cleanStaleTemp(dir, now)

	for _, p := range []string{oldZip, oldStaging} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be removed", filepath.Base(p))
		}
	}
	for _, p := range []string{fresh, other, client} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should stay: %v", filepath.Base(p), err)
		}
	}
}
