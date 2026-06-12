//go:build !android

package service

import "fmt"

func initMoonlightConfigDir() {}

// startMoonlightProxy is a no-op on non-Android: system Tailscale provides kernel
// routing so C-level sockets can reach Tailscale IPs directly.
func startMoonlightProxy(_ *TailscaleService, _ string, _ int) (stop func(), localRTSPPort int, err error) {
	return func() {}, 0, fmt.Errorf("not needed on this platform")
}
