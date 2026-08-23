//go:build linux

package permissions

import (
	"errors"
	"testing"
)

// TestDetectPkgManagerWith covers distro-family selection for
// RequestClipboardTool: given a set of binaries that happen to be on PATH,
// the first pkgManagers entry that matches wins, and an unsupported distro
// (no known package manager on PATH at all) is reported as such rather than
// panicking or silently picking the wrong one.
func TestDetectPkgManagerWith(t *testing.T) {
	lookPathAmong := func(present ...string) func(string) (string, error) {
		set := make(map[string]bool, len(present))
		for _, p := range present {
			set[p] = true
		}
		return func(name string) (string, error) {
			if set[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		}
	}

	cases := []struct {
		name    string
		present []string
		want    string // pkgManager.name, or "" for nil
	}{
		{name: "debian/ubuntu family", present: []string{"apt-get"}, want: "apt"},
		{name: "fedora/rhel8+", present: []string{"dnf"}, want: "dnf"},
		{name: "older rhel/centos7", present: []string{"yum"}, want: "yum"},
		{name: "arch family", present: []string{"pacman"}, want: "pacman"},
		{name: "opensuse", present: []string{"zypper"}, want: "zypper"},
		{name: "alpine", present: []string{"apk"}, want: "apk"},
		{name: "unsupported distro: nothing on PATH", present: nil, want: ""},
		{
			// apt-get is listed first in pkgManagers -- a system that
			// somehow has more than one manager's binary present (e.g. a
			// distro-hopper's leftover installs) should still prefer the
			// one actually driving its packages, not whichever sorts last.
			name:    "multiple present: earliest entry wins",
			present: []string{"pacman", "apt-get"},
			want:    "apt",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectPkgManagerWith(lookPathAmong(tc.present...))
			if tc.want == "" {
				if got != nil {
					t.Fatalf("detectPkgManagerWith() = %+v, want nil", got)
				}
				return
			}
			if got == nil || got.name != tc.want {
				t.Fatalf("detectPkgManagerWith() = %+v, want name %q", got, tc.want)
			}
		})
	}
}

// TestPkgManagersInstallXclip is a lightweight sanity check that every
// registered package manager's install script actually targets the xclip
// package -- a copy/paste mistake here (e.g. a stray "wl-clipboard") would
// otherwise only surface live, on whatever distro that entry corresponds
// to, days after the fact.
func TestPkgManagersInstallXclip(t *testing.T) {
	for _, pm := range pkgManagers {
		if !containsWord(pm.script, "xclip") {
			t.Errorf("pkgManager %q script does not mention xclip: %q", pm.name, pm.script)
		}
	}
}

func containsWord(s, word string) bool {
	for i := 0; i+len(word) <= len(s); i++ {
		if s[i:i+len(word)] == word {
			return true
		}
	}
	return false
}
