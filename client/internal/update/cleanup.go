package update

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

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
// archive behind.
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
