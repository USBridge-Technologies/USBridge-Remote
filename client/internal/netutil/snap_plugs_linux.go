//go:build linux && !android

package netutil

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// snapManualPlugs are the plugs of client/snap/snapcraft.yaml that snapd doesn't
// connect at install time. A store auto-connection grant empties SnapMissingPlugs
// for that plug, so this list doesn't have to change when one comes through.
var snapManualPlugs = []string{
	"audio-record",
	"alsa",
	"joystick",
	"camera",
	"raw-usb",
	"network-manager",
	"network-observe",
	"hardware-observe",
	"removable-media",
}

// InSnap reports whether the client runs from the snap.
func InSnap() bool { return inSnap() }

// SnapMissingPlugs asks snapd which of the manual plugs aren't connected. Outside
// a snap it is empty. A plug whose state can't be read isn't reported: only a
// plain "not connected" (exit 1, no error text) counts. It runs one snapctl per
// plug, so call it off the UI thread.
func SnapMissingPlugs() []string {
	if !inSnap() {
		return nil
	}
	var missing []string
	for _, plug := range snapManualPlugs {
		if snapPlugMissing(plug) {
			missing = append(missing, plug)
		}
	}
	return missing
}

func snapPlugMissing(plug string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "snapctl", "is-connected", plug).CombinedOutput()
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1 && strings.TrimSpace(string(out)) == ""
}

// SnapConnectCommand is one terminal line connecting the plugs. It stays on one
// line because sudo's password prompt would swallow the rest of a multi-line paste.
func SnapConnectCommand(plugs []string) string {
	cmds := make([]string, len(plugs))
	for i, plug := range plugs {
		cmds[i] = "sudo snap connect usbridge-client:" + plug
	}
	return strings.Join(cmds, " && ")
}
