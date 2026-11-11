// usbridge_benchrender is the streamer benchmark's test window on its own, for a
// target PC that has no agent: one behind a KVM. Its time barcode is drawn in the
// KVM's clock (-sync), the clock the client measures the stream's latency against.
//
//	usbridge_benchrender -sync http://10.55.0.1/api/bench/time [-fps 120] [-rect x,y,w,h]
//
// Esc or closing the window quits.
package main

import (
	"log"
	"os"

	"usbridge_agent/internal/benchrender"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	os.Exit(benchrender.Run(os.Args[1:]))
}
