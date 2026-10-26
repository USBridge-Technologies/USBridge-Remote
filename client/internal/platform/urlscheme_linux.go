//go:build linux && !android

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const handlerDesktopName = "usbridge-client-handler.desktop"

// launcherPath is what a link should start: the AppImage file itself when
// running from one (os.Executable would be a throw-away mount), else the binary.
func launcherPath() (string, error) {
	if p := os.Getenv("APPIMAGE"); p != "" {
		return p, nil
	}
	return os.Executable()
}

func userApplicationsDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "applications")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "applications")
}

// desktopExecQuote quotes a path for the Exec key of a .desktop file.
func desktopExecQuote(p string) string {
	r := strings.NewReplacer(`\`, `\\\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return `"` + r.Replace(p) + `"`
}

func handlerDesktopFile(exe string) string {
	return "[Desktop Entry]\nType=Application\nName=USBridge Client\n" +
		"Exec=" + desktopExecQuote(exe) + " %u\n" +
		"Icon=usbridge-client\nTerminal=false\nNoDisplay=true\n" +
		"MimeType=x-scheme-handler/" + URLScheme + ";\n"
}

func defaultHandler() string {
	out, err := exec.Command("xdg-mime", "query", "default", "x-scheme-handler/"+URLScheme).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// IsURLSchemeRegistered reports whether usbridge:// links open this app. A
// handler this app wrote earlier counts only while it points at the file that
// is running now (an AppImage that was moved needs registering again).
func IsURLSchemeRegistered() bool {
	h := defaultHandler()
	if h == "" || !strings.Contains(h, "usbridge") {
		return false
	}
	if h != handlerDesktopName {
		return true // installed from a package: its .desktop file declares the scheme
	}
	exe, err := launcherPath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(userApplicationsDir(), handlerDesktopName))
	return err == nil && strings.Contains(string(data), "Exec="+desktopExecQuote(exe)+" %u")
}

// RegisterURLScheme makes usbridge:// links open this app for the current user.
func RegisterURLScheme() error {
	exe, err := launcherPath()
	if err != nil {
		return err
	}
	dir := userApplicationsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, handlerDesktopName), []byte(handlerDesktopFile(exe)), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("xdg-mime", "default", handlerDesktopName, "x-scheme-handler/"+URLScheme).CombinedOutput(); err != nil {
		return fmt.Errorf("xdg-mime: %v: %s", err, strings.TrimSpace(string(out)))
	}
	_ = exec.Command("update-desktop-database", dir).Run() // best effort: not on every system
	return nil
}
