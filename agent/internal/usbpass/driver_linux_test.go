//go:build linux

package usbpass

import (
	"errors"
	"strings"
	"testing"
)

func TestUsbipInstallCommand(t *testing.T) {
	cases := []struct {
		name    string
		present []string
		want    string // substring of the expected command; "" = none
	}{
		{"arch", []string{"pacman"}, "pacman -S"},
		{"debian", []string{"apt-get"}, "apt-get install -y usbip"},
		{"fedora", []string{"dnf", "yum"}, "dnf install"},
		{"alpine", []string{"apk"}, "usbip-utils"},
		{"none", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lookPath := func(name string) (string, error) {
				for _, p := range c.present {
					if p == name {
						return "/usr/bin/" + name, nil
					}
				}
				return "", errors.New("not found")
			}
			got := usbipInstallCommand(lookPath)
			if c.want == "" {
				if got != "" {
					t.Fatalf("got %q, want none", got)
				}
				return
			}
			if !strings.Contains(got, c.want) {
				t.Fatalf("got %q, want it to contain %q", got, c.want)
			}
			if c.name == "arch" && strings.Contains(got, "apt-get") {
				t.Fatalf("arch command still uses apt-get: %q", got)
			}
		})
	}
}
