//go:build linux || (darwin && !ios)

// Note: a plain "darwin" build tag also matches GOOS=ios (Go treats ios as a
// specialization of darwin for build-tag purposes) — "!ios" is required here
// or this collides with moonlight_ios.go's own initMoonlightConfigDir.

package service

func initMoonlightConfigDir() {}
