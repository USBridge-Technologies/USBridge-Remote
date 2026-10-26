//go:build !(js && wasm)

package gui

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// tailscaleSignInTimeout is how long a connect waits for the user to finish
// signing in to Tailscale in the browser.
const tailscaleSignInTimeout = 5 * time.Minute

// ensureTailscaleSignedIn signs the embedded Tailscale node in when it is
// signed out: starts the interactive login (the connection manager's
// AuthURL handler opens the sign-in page, Android opens it itself), then
// waits for the user to finish. Returns at once when already signed in.
// waitCtx is the connect's cancelable context, so the toast's X stops it.
func (mw *MainWindow) ensureTailscaleSignedIn(waitCtx context.Context, gen uint64) error {
	ts := mw.tailscaleService
	if ts == nil {
		return nil
	}
	loggedIn := func() bool {
		ctx, cancel := context.WithTimeout(waitCtx, 5*time.Second)
		defer cancel()
		st, err := ts.Status(ctx)
		return err == nil && st != nil && st.LoggedIn
	}
	if loggedIn() {
		return nil
	}

	logrus.Info("🛰️ [CONNECT] Tailscale is signed out — starting sign-in automatically")
	loginCtx, cancel := context.WithTimeout(waitCtx, 40*time.Second)
	_, err := ts.StartLogin(loginCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("could not start the Tailscale sign-in: %w", err)
	}

	deadline := time.Now().Add(tailscaleSignInTimeout)
	for time.Now().Before(deadline) {
		if mw.connectAborted(waitCtx, gen) {
			return errConnectAborted
		}
		if loggedIn() {
			logrus.Info("🛰️ [CONNECT] Tailscale sign-in completed")
			return nil
		}
		select {
		case <-time.After(time.Second):
		case <-waitCtx.Done():
			return errConnectAborted
		}
	}
	return fmt.Errorf("Tailscale sign-in was not completed in time — try again")
}
