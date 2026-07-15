//go:build ignore

// This file has been split into platform-specific files:
//   moonlight_cgo_apple.go  — VideoToolbox + CoreAudio  (darwin, ios)
//   moonlight_cgo_linux.go  — libavcodec + ALSA         (linux)
//   moonlight_cgo_windows.go — libavcodec + WASAPI      (windows)
//   moonlight_cgo_android.go — AMediaCodec + AAudio     (android)
//   moonlight_cgo_wrapper.go — shared Go wrapper        (darwin/ios/linux)

package service
