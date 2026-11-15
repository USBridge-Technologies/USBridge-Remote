//go:build !ios || !cgo

package service

func openIOSSignIn(url string) {}

func closeIOSSignIn() {}
