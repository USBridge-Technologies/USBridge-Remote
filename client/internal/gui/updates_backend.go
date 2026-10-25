package gui

import (
	"context"
	"sync"

	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/update"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// InstallUpdatesBackend wires the footer version -> What's new -> Updates
// dialog (Release/Test channel switch, check and install on demand) to
// internal/update. Does nothing in builds with self-update compiled out,
// which hides the dialog's entry point.
func (mw *MainWindow) InstallUpdatesBackend(version string) {
	if !update.Enabled {
		return
	}
	var (
		mu      sync.Mutex
		pending *update.Manifest
	)
	view.Updates = &view.UpdatesBackend{
		TestChannel: func() bool { return update.GetChannel() == update.ChannelTest },
		SetTestChannel: func(test bool) {
			if test {
				update.SetChannel(update.ChannelTest)
			} else {
				update.SetChannel(update.ChannelStable)
			}
		},
		Check: func() (string, error) {
			manifest, err := update.CheckNow(context.Background(), version)
			mu.Lock()
			pending = manifest
			mu.Unlock()
			if manifest == nil {
				return "", err
			}
			return manifest.Version, nil
		},
		Install: func() {
			mu.Lock()
			manifest := pending
			mu.Unlock()
			if manifest == nil {
				return
			}
			fyne.Do(func() {
				progress := mw.ShowUpdateProgressDialog(manifest.Version)
				go func() {
					err := update.DownloadAndApply(context.Background(), manifest, progress.Update)
					progress.Close()
					if err != nil {
						logrus.Errorf("failed to apply update: %v", err)
						mw.ShowUpdateFailedDialog(err)
					}
				}()
			})
		},
	}
}
