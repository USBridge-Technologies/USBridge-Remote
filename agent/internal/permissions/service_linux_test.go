//go:build linux

package permissions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// TestPkgManagersInstallRequestedPackages is a lightweight sanity check
// that every registered package manager's install script actually targets
// whatever packages it was asked to install -- a copy/paste mistake here
// (e.g. a dropped package, or a stray unrelated one) would otherwise only
// surface live, on whatever distro that entry corresponds to, days after
// the fact. Covers both the xclip-only call (X11 sessions) and the
// xclip+wl-clipboard call RequestClipboardTool makes on Wayland.
func TestPkgManagersInstallRequestedPackages(t *testing.T) {
	for _, pm := range pkgManagers {
		for _, pkgs := range [][]string{{"xclip"}, {"xclip", "wl-clipboard"}} {
			script := pm.install(pkgs)
			for _, pkg := range pkgs {
				if !containsWord(script, pkg) {
					t.Errorf("pkgManager %q install(%v) does not mention %q: %q", pm.name, pkgs, pkg, script)
				}
			}
		}
	}
}

func writeFakeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

// TestClipboardInstallPreviewMatchesWhatRequestClipboardToolWouldRun covers
// the UI's "?" tooltip: it must show the real pkexec command (including
// wl-clipboard on a Wayland session, not just xclip), and must come back
// empty -- not a broken/misleading command -- when nothing would actually
// run at all.
func TestClipboardInstallPreviewMatchesWhatRequestClipboardToolWouldRun(t *testing.T) {
	s := &Service{}

	t.Run("no pkexec: empty preview", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if got := s.ClipboardInstallPreview(); got != "" {
			t.Fatalf("ClipboardInstallPreview() = %q, want \"\" (no pkexec on PATH)", got)
		}
	})

	t.Run("pkexec but no known package manager: empty preview", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeExecutable(t, dir, "pkexec")
		t.Setenv("PATH", dir)
		if got := s.ClipboardInstallPreview(); got != "" {
			t.Fatalf("ClipboardInstallPreview() = %q, want \"\" (no package manager on PATH)", got)
		}
	})

	t.Run("X11 session: xclip only", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeExecutable(t, dir, "pkexec")
		writeFakeExecutable(t, dir, "apt-get")
		t.Setenv("PATH", dir)
		t.Setenv("WAYLAND_DISPLAY", "")

		got := s.ClipboardInstallPreview()
		if !strings.Contains(got, "pkexec") || !strings.Contains(got, "xclip") {
			t.Fatalf("ClipboardInstallPreview() = %q, want it to mention pkexec and xclip", got)
		}
		if strings.Contains(got, "wl-clipboard") {
			t.Fatalf("ClipboardInstallPreview() = %q, should not mention wl-clipboard off Wayland", got)
		}
	})

	t.Run("Wayland session: xclip and wl-clipboard", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeExecutable(t, dir, "pkexec")
		writeFakeExecutable(t, dir, "apt-get")
		t.Setenv("PATH", dir)
		t.Setenv("WAYLAND_DISPLAY", "wayland-0")

		got := s.ClipboardInstallPreview()
		if !strings.Contains(got, "xclip") || !strings.Contains(got, "wl-clipboard") {
			t.Fatalf("ClipboardInstallPreview() = %q, want it to mention both xclip and wl-clipboard", got)
		}
	})
}

func containsWord(s, word string) bool {
	for i := 0; i+len(word) <= len(s); i++ {
		if s[i:i+len(word)] == word {
			return true
		}
	}
	return false
}
