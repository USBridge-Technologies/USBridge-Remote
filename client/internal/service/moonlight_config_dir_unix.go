//go:build (linux && !android) || (darwin && !ios)

// Note: plain "linux"/"darwin" build tags also match GOOS=android/ios
// respectively (Go treats android/ios as specializations of linux/darwin for
// build-tag purposes) — the "!android"/"!ios" exclusions are required here or
// this collides with moonlight_android.go's/moonlight_ios.go's own
// initMoonlightConfigDir.

package service

func initMoonlightConfigDir() {}
