//go:build !darwin && !ios && !linux && !windows && !android

package service

import (
	"fmt"
	"image"
	"os"
)

func usesVideoToolbox() bool { return false }

func startMoonlightGStreamer(
	pipeRead *os.File,
	width, height int,
	stopCh <-chan struct{},
	onFrame func(image.Image),
	onStop func(error),
) error {
	return fmt.Errorf("Moonlight video not supported on this platform")
}

func startMoonlightAudio(pipeRead *os.File, stopCh <-chan struct{}, onStop func(error)) error {
	return fmt.Errorf("Moonlight audio not supported on this platform")
}
