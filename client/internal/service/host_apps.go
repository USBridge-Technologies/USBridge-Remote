package service

import (
	"context"
	"fmt"

	"usbridge-client/internal/api/moonlight"

	"github.com/sirupsen/logrus"
)

// The host's own app list (/applist): Desktop plus whatever the host has
// added -- Steam Big Picture, a game, a low/high resolution desktop -- the
// list stock Moonlight shows after a connect. The connect dialog offers it
// when there is more than one; the chosen app is what the next /launch
// starts. 0 = none chosen: "Desktop", else the first app (the old default).

// SetLaunchApp records the app the next ConnectToMoonlight launches.
func (m *MoonlightService) SetLaunchApp(appID int) {
	m.launchAppID.Store(int32(appID))
}

// ListHostApps fetches the host's app list without launching anything. A
// host this client isn't paired with yet answers with an error: pairing
// happens on the first ConnectToMoonlight, never from here.
func (m *MoonlightService) ListHostApps(ctx context.Context) ([]moonlight.App, error) {
	m.prepareGameStreamClient()
	type result struct {
		apps []moonlight.App
		err  error
	}
	done := make(chan result, 1)
	go func() {
		info, err := m.client.GetServerInfo()
		if err != nil {
			done <- result{err: err}
			return
		}
		if info.PairStatus == 0 {
			done <- result{err: fmt.Errorf("not paired")}
			return
		}
		apps, err := m.client.GetAppList()
		done <- result{apps: apps, err: err}
	}()
	select {
	case r := <-done:
		return r.apps, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// pickLaunchApp is the chosen app when the host still lists it, otherwise
// "Desktop" or the first app.
func (m *MoonlightService) pickLaunchApp(apps []moonlight.App) int {
	chosen := int(m.launchAppID.Load())
	appID := 0
	for _, app := range apps {
		logrus.Infof("🎮 Found Moonlight App: %s (ID: %d)", app.AppTitle, app.ID)
		if chosen != 0 && app.ID == chosen {
			return chosen
		}
		if appID == 0 || app.AppTitle == "Desktop" {
			appID = app.ID
		}
	}
	return appID
}
