//go:build !windows

package main

func runMain(headless, tray bool, attach string) {
	doStart(headless, tray, attach)
}

func manageService(action string) error {
	return nil
}
