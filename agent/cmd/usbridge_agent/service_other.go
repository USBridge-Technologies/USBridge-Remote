//go:build !windows

package main

func runMain(headless bool) {
	doStart(headless)
}

func manageService(action string) error {
	return nil
}
