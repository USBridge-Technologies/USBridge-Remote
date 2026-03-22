//go:build !windows

package service

func ensureWireGuardRuntimeAvailable() error {
	return nil
}
