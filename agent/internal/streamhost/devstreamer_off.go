//go:build !devstreamer

package streamhost

// devStreamerOverride is always "" in release builds: only the staged,
// signed usbridge-streamer ever runs. See devstreamer_on.go for the
// `-tags devstreamer` debugging override.
func devStreamerOverride() string { return "" }
