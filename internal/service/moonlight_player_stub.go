//go:build !darwin && (!linux || android) && !windows

package service

import (
	"fmt"
	"image"
	"os"
)

func startMoonlightGStreamer(
	pipeRead *os.File,
	width, height int,
	stopCh <-chan struct{},
	onFrame func(image.Image),
	onStop func(error),
) error {
	return fmt.Errorf("Moonlight GStreamer player not supported on this platform")
}
