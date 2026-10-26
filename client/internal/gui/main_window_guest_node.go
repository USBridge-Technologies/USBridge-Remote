//go:build !(js && wasm)

package gui

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// sharedNodeWait is how long a guest waits for the computer to show up on the
// guest network after their own node is up (its agent brings the node up when
// the invitation is made, and again whenever a share is live).
const sharedNodeWait = 40 * time.Second

// waitForSharedNode returns once host (the shared computer's address on the
// guest network) is a peer of this app's guest node.
func (mw *MainWindow) waitForSharedNode(waitCtx context.Context, gen uint64, host string) error {
	ts := mw.tailscaleService
	if ts == nil || mw.guestSession == nil {
		return nil
	}
	visible := func() bool {
		ctx, cancel := context.WithTimeout(waitCtx, 5*time.Second)
		defer cancel()
		st, err := ts.Status(ctx)
		if err != nil || st == nil || !st.Running {
			return false
		}
		for _, p := range st.Peers {
			if p.IP4 == host {
				return true
			}
		}
		return false
	}
	deadline := time.Now().Add(sharedNodeWait)
	for time.Now().Before(deadline) {
		if mw.connectAborted(waitCtx, gen) {
			return errConnectAborted
		}
		if visible() {
			logrus.Info("🛰️ [CONNECT] The shared computer is visible on the guest network")
			return nil
		}
		select {
		case <-time.After(time.Second):
		case <-waitCtx.Done():
			return errConnectAborted
		}
	}
	if mw.guestSession.Network == "tailscale" {
		return fmt.Errorf("this computer is not visible in your Tailscale yet — the owner has to share it with your Tailscale account")
	}
	return fmt.Errorf("the computer is not reachable right now — its agent may be offline; ask the owner and try again")
}

// guestConsentWait is how long a guest waits for the person at the computer.
const guestConsentWait = 100 * time.Second

// waitForHostConsent starts the guest's session on the agent and waits until it
// is allowed (at once when the owner did not ask to be consulted).
func (mw *MainWindow) waitForHostConsent(ctx context.Context, gen uint64) error {
	client := mw.usbClient
	if client == nil {
		return nil
	}
	deadline := time.Now().Add(guestConsentWait)
	for {
		state, err := client.GuestHello()
		if err != nil {
			return fmt.Errorf("guest access: %w", err)
		}
		switch state {
		case "allowed":
			return nil
		case "denied":
			return fmt.Errorf("the person at the computer declined the connection")
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the person at the computer did not answer")
		}
		if mw.connectAborted(ctx, gen) {
			return errConnectAborted
		}
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return errConnectAborted
		}
	}
}
