//go:build linux && !android

package gui

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/sirupsen/logrus"
)

func nativeSaveAvailable() bool { return true }

// nativeSaveFile opens the desktop's own save panel: the XDG desktop
// portal first (KDE, GNOME and the others all ship one, and it is the only
// way out of a Flatpak/Snap sandbox), then zenity/kdialog for desktops
// without a portal. "" with a nil error means the operator cancelled; an
// error means no native panel could be shown and the caller falls back to
// Fyne's own dialog.
func nativeSaveFile(parent uintptr, title, defaultName string, filter saveFilter) (string, error) {
	path, err := portalSaveFile(parent, title, defaultName, filter)
	if err == nil {
		return path, nil
	}
	logrus.Infof("save dialog: desktop portal unavailable (%v), trying zenity/kdialog", err)
	return helperSaveFile(title, defaultName, filter)
}

const (
	portalBus       = "org.freedesktop.portal.Desktop"
	portalPath      = "/org/freedesktop/portal/desktop"
	portalChooser   = "org.freedesktop.portal.FileChooser"
	portalRequestIf = "org.freedesktop.portal.Request"
)

// portalFilter is the portal's a(sa(us)) filter entry: a name and its
// (kind, pattern) list, kind 0 being a glob.
type portalFilter struct {
	Name     string
	Patterns []portalPattern
}

type portalPattern struct {
	Kind    uint32
	Pattern string
}

func portalSaveFile(parent uintptr, title, defaultName string, filter saveFilter) (string, error) {
	// A private connection: the shared session bus is used elsewhere, and
	// the signal match below must not leak onto it.
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	var tok [8]byte
	_, _ = rand.Read(tok[:])
	token := "usbridge_" + hex.EncodeToString(tok[:])
	sender := strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
	reqPath := dbus.ObjectPath(portalPath + "/request/" + sender + "/" + token)

	// Subscribe before calling: a fast portal can answer before SaveFile
	// returns the request handle.
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(reqPath),
		dbus.WithMatchInterface(portalRequestIf),
		dbus.WithMatchMember("Response"),
	); err != nil {
		return "", err
	}
	signals := make(chan *dbus.Signal, 4)
	conn.Signal(signals)

	typed := portalFilter{Name: filter.Name, Patterns: []portalPattern{
		{0, "*." + strings.ToLower(filter.Ext)}, {0, "*." + strings.ToUpper(filter.Ext)},
	}}
	all := portalFilter{Name: "All files", Patterns: []portalPattern{{0, "*"}}}
	opts := map[string]dbus.Variant{
		"handle_token":   dbus.MakeVariant(token),
		"modal":          dbus.MakeVariant(true),
		"current_name":   dbus.MakeVariant(defaultName),
		"filters":        dbus.MakeVariant([]portalFilter{typed, all}),
		"current_filter": dbus.MakeVariant(typed),
	}
	parentID := ""
	if parent != 0 {
		parentID = fmt.Sprintf("x11:%x", parent)
	}

	var handle dbus.ObjectPath
	call := conn.Object(portalBus, portalPath).Call(portalChooser+".SaveFile", 0, parentID, title, opts)
	if err := call.Store(&handle); err != nil {
		return "", err
	}
	if handle != reqPath {
		// Pre-0.9 portals ignore handle_token; follow the path they gave.
		_ = conn.AddMatchSignal(
			dbus.WithMatchObjectPath(handle),
			dbus.WithMatchInterface(portalRequestIf),
			dbus.WithMatchMember("Response"),
		)
	}

	for sig := range signals {
		if sig.Name != portalRequestIf+".Response" || (sig.Path != handle && sig.Path != reqPath) {
			continue
		}
		return portalResponsePath(sig.Body)
	}
	return "", errors.New("portal connection closed")
}

// portalResponsePath reads Request.Response's (u response, a{sv} results):
// 0 is a pick, 1 a cancel, 2 the portal failing.
func portalResponsePath(body []any) (string, error) {
	if len(body) < 2 {
		return "", errors.New("malformed portal response")
	}
	code, _ := body[0].(uint32)
	switch code {
	case 0:
	case 1:
		return "", nil
	default:
		return "", fmt.Errorf("portal response %d", code)
	}
	results, _ := body[1].(map[string]dbus.Variant)
	uris, _ := results["uris"].Value().([]string)
	if len(uris) == 0 {
		return "", nil
	}
	u, err := url.Parse(uris[0])
	if err != nil || u.Scheme != "file" {
		return "", fmt.Errorf("portal returned %q", uris[0])
	}
	return u.Path, nil
}

func helperSaveFile(title, defaultName string, filter saveFilter) (string, error) {
	glob := "*." + filter.Ext
	cmd := exec.Command(
		"zenity",
		"--file-selection",
		"--save",
		"--confirm-overwrite",
		"--title="+title,
		"--filename="+defaultName,
		"--file-filter="+filter.Name+" | "+glob,
		"--file-filter=All files | *",
	)
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	if !execNotFound(err) {
		if _, ok := err.(*exec.ExitError); ok {
			return "", nil
		}
		return "", err
	}

	cmd = exec.Command("kdialog", "--getsavefilename", defaultName, glob+" | "+filter.Name, "--title", title)
	out, err = cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	if execNotFound(err) {
		return "", err
	}
	if _, ok := err.(*exec.ExitError); ok {
		return "", nil
	}
	return "", err
}

func execNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var ee *exec.Error
	return errors.As(err, &ee) && errors.Is(ee.Err, exec.ErrNotFound)
}
