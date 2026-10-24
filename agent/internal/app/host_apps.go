package app

import (
	"log"
	"time"

	"usbridge_agent/internal/streamhost"
)

// HostApps is the app list Moonlight clients see after connecting
// (streamhost/apps.go), the same for every stream host.
func (a *App) HostApps() ([]streamhost.HostApp, error) {
	return streamhost.LoadHostApps(a.cfg.StateDir)
}

// SetHostApps saves the app list and hands it to the running stream host.
// A host that only reads it on start (Sunshine and its forks; RustShine
// the first time) is restarted -- right away when nothing streams, else
// once the stream ends: pending reports that wait.
func (a *App) SetHostApps(apps []streamhost.HostApp) (pending bool, err error) {
	if err := streamhost.SaveHostApps(a.cfg.StateDir, apps); err != nil {
		return false, err
	}
	a.streamMu.Lock()
	stream := a.stream
	a.streamMu.Unlock()
	applier, ok := stream.(streamhost.HostAppsApplier)
	if !ok || stream == nil {
		return false, nil
	}
	restart, err := applier.ApplyHostApps()
	if err != nil {
		return false, err
	}
	if !restart || !stream.Running() {
		return false, nil
	}
	if !stream.SessionActive() {
		log.Printf("[apps] app list changed -- restarting %s to load it", stream.DisplayName())
		return false, a.RestartSunshine()
	}
	if a.hostAppsRestartPending.CompareAndSwap(false, true) {
		log.Printf("[apps] app list changed during a stream -- %s restarts when it ends", stream.DisplayName())
		go a.restartForHostAppsWhenIdle(stream)
	}
	return true, nil
}

// restartForHostAppsWhenIdle restarts stream once its session ends, unless
// the agent switched to another backend meanwhile (which reads the list on
// its own start).
func (a *App) restartForHostAppsWhenIdle(stream streamhost.Backend) {
	defer a.hostAppsRestartPending.Store(false)
	for {
		time.Sleep(5 * time.Second)
		a.streamMu.Lock()
		current := a.stream
		a.streamMu.Unlock()
		if current != stream || !stream.Running() {
			return
		}
		if !stream.SessionActive() {
			log.Printf("[apps] stream ended -- restarting %s to load the new app list", stream.DisplayName())
			if err := a.RestartSunshine(); err != nil {
				log.Printf("[apps] restart failed: %v", err)
			}
			return
		}
	}
}
