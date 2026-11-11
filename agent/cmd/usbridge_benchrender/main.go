// usbridge_benchrender is the streamer benchmark's test window on its own, for a
// target PC that has no agent: one behind a KVM. Its time barcode is drawn in the
// KVM's clock (-sync), the clock the client measures the stream's latency against.
//
//	usbridge_benchrender -sync http://10.55.0.1:8080/api/bench/time [-fps 480] [-rect x,y,w,h]
//
// Esc or closing the window quits.
package main

import (
	"log"
	"os"
	"strconv"
	"strings"

	"usbridge_agent/internal/benchrender"
)

// defaultFPS: far above the monitor's rate, so the frame the compositor picks up
// for each refresh was drawn just before it, not up to a whole frame interval
// earlier. Measured behind a NanoKVM (KWin, 60 Hz): the picture was 28 ms old at
// capture drawn at 60-120 fps, 23 ms at 480. A capture card sees only what the
// compositor sends, so that staleness was all latency the KVM never added.
const defaultFPS = 480

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	args := os.Args[1:]
	if !hasFlag(args, "fps") {
		args = append([]string{"-fps", strconv.Itoa(defaultFPS)}, args...)
	}
	os.Exit(benchrender.Run(args))
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		a = strings.TrimLeft(a, "-")
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}
