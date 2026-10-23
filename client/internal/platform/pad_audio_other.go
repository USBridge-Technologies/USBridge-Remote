//go:build !(windows && cgo) && !(linux && !android && cgo)

package platform

// Playing a pad's sound on the pad is implemented on Windows and Linux.

func padAudioChannels(deviceID string) (int, error) { return 0, ErrNoPadAudio }

func openPadSink(deviceID string, channels int) (padSink, error) { return nil, ErrNoPadAudio }
