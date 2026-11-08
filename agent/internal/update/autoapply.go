package update

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// autoRetryAfter is how long an unattended install waits before trying the
// same version again. If an attempt didn't take (the swap failed, the new
// version didn't come up), the agent that starts afterwards is still the old
// one and finds the same update at once; without this it would download and
// install it over and over.
const autoRetryAfter = 6 * time.Hour

func attemptFilePath() string {
	p := channelFilePath()
	if p == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), appName+"-update-attempt")
}

func lastAttempt() (version string, at time.Time, ok bool) {
	p := attemptFilePath()
	if p == "" {
		return "", time.Time{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", time.Time{}, false
	}
	v, ts, found := strings.Cut(strings.TrimSpace(string(b)), "\n")
	sec, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64)
	if !found || err != nil {
		return "", time.Time{}, false
	}
	return strings.TrimSpace(v), time.Unix(sec, 0), true
}

func recordAttempt(version string, now time.Time) {
	p := attemptFilePath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
		_ = os.WriteFile(p, []byte(version+"\n"+strconv.FormatInt(now.Unix(), 10)+"\n"), 0o644)
	}
}

// shouldAutoApply reports whether an unattended install of version may run
// now, and records the attempt when it may.
func shouldAutoApply(version string, now time.Time) bool {
	if v, at, ok := lastAttempt(); ok && v == version && now.Sub(at) < autoRetryAfter && !at.After(now) {
		logrus.WithFields(logrus.Fields{
			"component": "update",
			"version":   version,
			"retry_at":  at.Add(autoRetryAfter).Format(time.RFC3339),
		}).Warn("the last install of this version did not take; not retrying yet")
		return false
	}
	recordAttempt(version, now)
	return true
}

// AutoApply installs manifest without asking, for the headless agent, unless
// the same version was already tried within autoRetryAfter. A user-started
// install (DownloadAndApply from the window) is never held back.
func AutoApply(ctx context.Context, manifest *Manifest) error {
	if !shouldAutoApply(manifest.Version, time.Now()) {
		return nil
	}
	return DownloadAndApply(ctx, manifest, nil)
}

// staleTempAge keeps CleanStaleTemp away from a download that may still be
// running (downloadTimeout is 5 minutes).
const staleTempAge = 10 * time.Minute

// staleTempRecheck is when CleanStaleTemp looks again after launch: an
// update installed by an older version leaves its archives right before
// this process starts, too fresh for the first pass, and a service may then
// run for weeks.
const staleTempRecheck = 15 * time.Minute

// CleanStaleTemp deletes update archives and staging folders that earlier
// updates left in the temp folder: each successful update used to leave its
// archive behind, and a Windows service caught in a failing update loop left
// hundreds of gigabytes of them.
func CleanStaleTemp() {
	cleanStaleTemp(os.TempDir(), time.Now())
	time.AfterFunc(staleTempRecheck, func() { cleanStaleTemp(os.TempDir(), time.Now()) })
}

func cleanStaleTemp(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := "usbridge-" + appName + "-update-"
	removed := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < staleTempAge {
			continue
		}
		if os.RemoveAll(filepath.Join(dir, e.Name())) == nil {
			removed++
		}
	}
	if removed > 0 {
		logrus.WithFields(logrus.Fields{"component": "update", "dir": dir, "removed": removed}).Info("removed leftovers of earlier updates")
	}
}
