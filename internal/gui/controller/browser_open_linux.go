//go:build (linux && !android) || freebsd || openbsd || netbsd

package controller

import (
	"net/url"
	"os"
	"os/exec"
	"strings"

	"fyne.io/fyne/v2"
)

type browserCommand struct {
	name string
	args []string
}

func openExternalURL(app fyne.App, uri *url.URL) error {
	// Standard Linux way to open a URL in background
	go func() {
		cmd := exec.Command("xdg-open", uri.String())
		_ = cmd.Start()
	}()
	
	// Also trigger Fyne's built-in opener as it might have better integration 
	// with some desktop environments.
	return app.OpenURL(uri)
}

func browserOpenCandidates(rawURL string) []browserCommand {
	return []browserCommand{
		{name: "/usr/bin/xdg-open", args: []string{rawURL}},
		{name: "/bin/xdg-open", args: []string{rawURL}},
		{name: "xdg-open", args: []string{rawURL}},
		{name: "/usr/bin/gio", args: []string{"open", rawURL}},
		{name: "/bin/gio", args: []string{"open", rawURL}},
		{name: "gio", args: []string{"open", rawURL}},
		{name: "/usr/bin/sensible-browser", args: []string{rawURL}},
		{name: "/bin/sensible-browser", args: []string{rawURL}},
		{name: "sensible-browser", args: []string{rawURL}},
		{name: "/usr/bin/gnome-open", args: []string{rawURL}},
		{name: "gnome-open", args: []string{rawURL}},
		{name: "/usr/bin/kde-open", args: []string{rawURL}},
		{name: "kde-open", args: []string{rawURL}},
		{name: "/usr/bin/kde-open5", args: []string{rawURL}},
		{name: "kde-open5", args: []string{rawURL}},
	}
}

func browserCommandAvailable(name string) bool {
	if strings.ContainsRune(name, os.PathSeparator) {
		info, err := os.Stat(name)
		return err == nil && !info.IsDir()
	}

	_, err := exec.LookPath(name)
	return err == nil
}

func browserCommandLabel(cmd browserCommand) string {
	return strings.TrimSpace(strings.Join(append([]string{cmd.name}, cmd.args...), " "))
}
