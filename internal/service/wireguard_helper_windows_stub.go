//go:build !windows

package service

func RunWindowsWireGuardHelper(_ []string) error {
	return nil
}
