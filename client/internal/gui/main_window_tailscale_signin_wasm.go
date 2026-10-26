//go:build js && wasm

package gui

import "context"

// The browser build has no embedded Tailscale to sign in.
func (mw *MainWindow) ensureTailscaleSignedIn(context.Context, uint64) error { return nil }
