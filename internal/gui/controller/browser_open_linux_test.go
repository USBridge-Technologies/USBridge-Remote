//go:build linux || freebsd || openbsd || netbsd

package controller

import "testing"

func TestBrowserOpenCandidatesIncludeFallbacks(t *testing.T) {
	candidates := browserOpenCandidates("https://example.com/docs")
	if len(candidates) < 6 {
		t.Fatalf("expected multiple browser open candidates, got %d", len(candidates))
	}

	assertCandidate := func(name string, args ...string) {
		t.Helper()

		for _, candidate := range candidates {
			if candidate.name != name {
				continue
			}
			if len(candidate.args) != len(args) {
				continue
			}

			match := true
			for i := range args {
				if candidate.args[i] != args[i] {
					match = false
					break
				}
			}
			if match {
				return
			}
		}

		t.Fatalf("candidate %q with args %v not found", name, args)
	}

	assertCandidate("/usr/bin/xdg-open", "https://example.com/docs")
	assertCandidate("xdg-open", "https://example.com/docs")
	assertCandidate("/usr/bin/gio", "open", "https://example.com/docs")
	assertCandidate("gio", "open", "https://example.com/docs")
}
