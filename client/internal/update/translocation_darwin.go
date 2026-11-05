//go:build darwin && !ios

package update

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

const quarantineAttr = "com.apple.quarantine"

// LeaveTranslocation relaunches the app from its real bundle when
// Gatekeeper started it from an App Translocation mount, and reports
// whether the caller should exit. It happens to a correctly installed app
// too: a bundle dragged from the .dmg into /Applications keeps the
// browser's quarantine flag, and macOS may still run it from a read-only
// copy, where self-update and anything that looks next to the bundle
// fail. Dropping the flag from our own bundle (allowed, as it carries
// our team's signature) ends that for this and every later launch.
//
// It relaunches only when a flag was actually removed, so a bundle that
// keeps getting translocated for some other reason can't loop.
func LeaveTranslocation() bool {
	exePath, err := os.Executable()
	if err != nil || !strings.Contains(exePath, "/AppTranslocation/") {
		return false
	}
	log := logrus.WithField("component", "update")
	bundle, err := currentAppBundlePath()
	if err != nil || strings.Contains(bundle, "/AppTranslocation/") {
		log.WithError(err).Warn("running translocated, real bundle not found")
		return false
	}
	removed, err := removeQuarantine(bundle)
	if err != nil {
		log.WithError(err).Warn("running translocated, could not clear the quarantine flag")
		return false
	}
	if !removed {
		return false
	}
	args := append([]string{"-n", bundle}, launchArgs()...)
	if err := exec.Command("open", args...).Start(); err != nil {
		log.WithError(err).Warn("relaunch outside translocation")
		return false
	}
	log.WithField("bundle", bundle).Info("cleared quarantine, relaunching from the real bundle")
	return true
}

func launchArgs() []string {
	if len(os.Args) < 2 {
		return nil
	}
	return append([]string{"--args"}, os.Args[1:]...)
}

// removeQuarantine drops the quarantine flag from every file in bundle and
// reports whether there was one. The bundle root's own error is the one
// returned; deeper files are best effort.
func removeQuarantine(bundle string) (bool, error) {
	removed := false
	var rootErr error
	filepath.WalkDir(bundle, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		switch e := unix.Lremovexattr(path, quarantineAttr); {
		case e == nil:
			removed = true
		case errors.Is(e, unix.ENOATTR):
		case path == bundle:
			rootErr = e
			return filepath.SkipAll
		}
		return nil
	})
	return removed, rootErr
}
