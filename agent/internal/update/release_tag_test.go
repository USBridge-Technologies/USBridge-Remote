package update

import "testing"

func TestNewestReleaseTag(t *testing.T) {
	// The API's real order on 2026-11-06: 3.2.19 listed before 3.2.21/3.2.20.
	got, err := newestReleaseTag([]string{"release-v3.2.19", "release-v3.2.21", "release-v3.2.20", "release-v3.2.15"})
	if err != nil || got != "release-v3.2.21" {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, _ := newestReleaseTag([]string{"release-v3.2.9", "test-3.2.10-abc"}); got != "test-3.2.10-abc" {
		t.Fatalf("got %q", got)
	}
	if got, _ := newestReleaseTag([]string{"nightly", "other"}); got != "nightly" {
		t.Fatalf("no-version fallback: %q", got)
	}
	if _, err := newestReleaseTag(nil); err == nil {
		t.Fatal("no error without releases")
	}
	for tag, want := range map[string]string{"release-v3.2.21": "3.2.21", "v2026.1106.2.usbridge": "2026.1106.2", "x1y": "", "release-2026.07.15": "2026.07.15"} {
		if got := tagVersion(tag); got != want {
			t.Errorf("tagVersion(%q) = %q, want %q", tag, got, want)
		}
	}
}
