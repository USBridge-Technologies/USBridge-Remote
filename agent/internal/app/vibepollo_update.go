package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"usbridge_agent/internal/forkrelease"
)

// Vibepollo from Streamers-Forks (forkrelease.PrepareVibepollo): never shipped with the
// agent; downloaded the first time it is picked as the streamer and updated from then on,
// like Punktfunk and Sunshine.

// vibepolloUpdateInterval: as punktfunkUpdateInterval.
const vibepolloUpdateInterval = 6 * time.Hour

// vibepolloBusyRetry: an update found while a client is streaming is tried
// again this much later instead of cutting the stream.
const vibepolloBusyRetry = 15 * time.Minute

var errVibepolloStreaming = errors.New("a client is streaming")

// vibepolloDownloadable: Streamers-Forks publishes a Vibepollo for this platform.
func vibepolloDownloadable() bool { return forkrelease.VibepolloAssetName() != "" }

// DownloadVibepollo downloads Vibepollo (signature and SHA-256 checked) and
// puts it in place; SetStreamBackend("vibepollo") calls it when there is
// none. Progress goes through entStatus like DownloadPunktfunk's.
func (a *App) DownloadVibepollo(onProgress forkrelease.ProgressFunc) error {
	a.entMu.Lock()
	if a.entStatus.VibepolloUpdateInProgress {
		a.entMu.Unlock()
		return fmt.Errorf("vibepollo download already in progress")
	}
	a.entStatus.VibepolloUpdateInProgress = true
	a.entMu.Unlock()
	defer a.endVibepolloUpdate()
	return a.downloadVibepollo(onProgress)
}

func (a *App) downloadVibepollo(onProgress forkrelease.ProgressFunc) error {
	a.entMu.Lock()
	a.entStatus.DownloadInProgress = true
	a.entStatus.DownloadName = "Vibepollo"
	a.entStatus.Progress = -1
	a.entStatus.LastError = ""
	a.entMu.Unlock()
	defer func() {
		a.entMu.Lock()
		a.entStatus.DownloadInProgress = false
		a.entStatus.DownloadName = ""
		a.entMu.Unlock()
	}()
	combined := func(downloaded, total int64) {
		frac := -1.0
		if total > 0 {
			frac = float64(downloaded) / float64(total)
		}
		a.entMu.Lock()
		a.entStatus.Progress = frac
		a.entMu.Unlock()
		if onProgress != nil {
			onProgress(downloaded, total)
		}
	}
	log.Printf("[app] downloading Vibepollo (Streamers-Forks latest release)")
	prep, err := forkrelease.PrepareVibepollo(context.Background(), a.cfg.StateDir, combined)
	if err == nil {
		if err = prep.Commit(); err != nil {
			prep.Discard()
		}
	}
	if err != nil {
		a.setEntError(fmt.Sprintf("vibepollo download failed: %v", err))
		log.Printf("[app] vibepollo download failed: %v", err)
		return fmt.Errorf("vibepollo download failed: %w", err)
	}
	log.Printf("[app] Vibepollo %s downloaded and verified", prep.Version)
	return nil
}

func (a *App) endVibepolloUpdate() {
	a.entMu.Lock()
	a.entStatus.VibepolloUpdateInProgress = false
	a.entMu.Unlock()
}

// checkVibepolloUpdate installs a newer Vibepollo from Streamers-Forks every
// vibepolloUpdateInterval -- only one the agent downloaded itself; a bundled
// one from an older install moves over through the card's button. Never cuts
// a stream: with a client connected it waits for vibepolloBusyRetry.
func (a *App) checkVibepolloUpdate(ctx context.Context) {
	if !vibepolloDownloadable() || !forkrelease.VibepolloStaged(a.cfg.StateDir) {
		return
	}
	a.entMu.Lock()
	due := time.Since(a.lastVibepolloCheck) >= vibepolloUpdateInterval && !a.entStatus.VibepolloUpdateInProgress
	if due {
		a.lastVibepolloCheck = time.Now()
		a.entStatus.VibepolloUpdateInProgress = true
	}
	a.entMu.Unlock()
	if !due {
		return
	}
	defer a.endVibepolloUpdate()
	err := a.updateVibepollo(ctx, true)
	if errors.Is(err, errVibepolloStreaming) {
		a.entMu.Lock()
		a.lastVibepolloCheck = time.Now().Add(vibepolloBusyRetry - vibepolloUpdateInterval)
		a.entMu.Unlock()
		log.Printf("[app] vibepollo update postponed: %v", err)
	} else if err != nil {
		log.Printf("[app] vibepollo update failed (will retry): %v", err)
	}
}

// CheckVibepolloUpdateNow is the Vibepollo card's "check for updates": the
// latest Streamers-Forks release, right now, applied even mid-stream (the
// user asked). It also replaces a Vibepollo bundled with an older agent.
func (a *App) CheckVibepolloUpdateNow() error {
	if !vibepolloDownloadable() {
		return fmt.Errorf("vibepollo is updated together with the agent on this platform")
	}
	a.entMu.Lock()
	if a.entStatus.VibepolloUpdateInProgress {
		a.entMu.Unlock()
		return fmt.Errorf("vibepollo update already in progress")
	}
	a.entStatus.VibepolloUpdateInProgress = true
	a.entStatus.LastError = ""
	a.lastVibepolloCheck = time.Now()
	a.entMu.Unlock()
	defer a.endVibepolloUpdate()
	if err := a.updateVibepollo(context.Background(), false); err != nil {
		a.setEntError(fmt.Sprintf("vibepollo update failed: %v", err))
		return err
	}
	return nil
}

// updateVibepollo installs the latest release's Vibepollo when it differs from
// the downloaded one (or there is no downloaded one yet). A Vibepollo that is
// the active streamer is stopped for the swap -- Windows can't replace a
// running .exe -- and started again from the new tree. On Linux with KMS
// capture the launcher keeps running the root-owned copy of the previous
// tree until screen capture is granted again (permissions.installVibepolloKMS).
func (a *App) updateVibepollo(ctx context.Context, spareStream bool) error {
	staged := forkrelease.VibepolloStaged(a.cfg.StateDir)
	latest, err := forkrelease.LatestVibepolloVersion(ctx)
	if err != nil {
		return err
	}
	if latest == "" || (staged && latest == forkrelease.VibepolloStagedVersion(a.cfg.StateDir)) {
		return nil
	}
	if spareStream && a.currentStreamKind() == "vibepollo" && a.SessionActive() {
		return errVibepolloStreaming
	}
	log.Printf("[app] Vibepollo %s available -- downloading", latest)
	prep, err := forkrelease.PrepareVibepollo(ctx, a.cfg.StateDir, nil)
	if err != nil {
		return err
	}
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	active := a.streamKind == "vibepollo" && a.stream != nil
	if active && spareStream && a.stream.SessionActive() {
		prep.Discard()
		return errVibepolloStreaming
	}
	if active {
		a.stopStreamAndWait(a.stream)
	}
	commitErr := prep.Commit()
	if commitErr != nil {
		prep.Discard()
		commitErr = fmt.Errorf("vibepollo update not applied: %w", commitErr)
	} else {
		log.Printf("[app] Vibepollo updated to %s", prep.Version)
	}
	if active {
		if err := a.RestartSunshineStartOnly(); err != nil {
			log.Printf("[app] vibepollo restart after update failed: %v", err)
		}
	}
	return commitErr
}
