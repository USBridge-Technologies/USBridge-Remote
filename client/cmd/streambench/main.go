// streambench streams from a paired GameStream host without the GUI and
// prints the same per-run metrics the GUI benchmark computes
// (service.AnalyzeBenchRun): frame cadence, stalls, host capture+encode
// time, packet transfer time, jitter, loss and FEC counters. One JSON line
// per run, so a sweep over streamers/codecs/bitrates/loss is a shell loop.
//
//	go run -tags usbpass_gousb ./cmd/streambench -host 192.168.200.15 \
//	    -codec pyrowave -bitrate 150000 -seconds 20 -label rustshine
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
)

func main() {
	host := flag.String("host", "127.0.0.1", "GameStream host")
	mode := flag.String("codec", models.VideoModePyroWave, "video mode (pyrowave, h264, h265, av1)")
	width := flag.Int("width", 1920, "stream width")
	height := flag.Int("height", 1080, "stream height")
	fps := flag.Int("fps", 60, "stream fps")
	bitrate := flag.Int("bitrate", 150000, "bitrate, kbps")
	seconds := flag.Int("seconds", 20, "measurement window")
	settle := flag.Int("settle", 3, "seconds to wait after the first frame before recording")
	label := flag.String("label", "", "free-form tag copied into the output (streamer, loss level, ...)")
	flag.Parse()
	logrus.SetLevel(logrus.WarnLevel)

	if !service.BenchSupported() {
		fmt.Fprintln(os.Stderr, "this build does not record per-frame data")
		os.Exit(3)
	}
	m := service.NewMoonlightService(models.DefaultConfig())
	m.UpdateHost(*host)
	m.SetVideoMode(*mode)
	m.SetExpectedVideoSize(*width, *height)
	m.SetFPS(*fps)
	m.SetBitrate(*bitrate)
	// An unpaired client asks the host for a PIN; print it so a script can submit it to the host
	// (the agent's POST /token/pin).
	m.SetOnPairingPINRequired(func(pin string) { fmt.Println("PIN", pin) })
	connectAt := time.Now()
	if err := m.ConnectToMoonlight(); err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	startup := time.Since(connectAt)
	time.Sleep(time.Duration(*settle) * time.Second)

	codec, _ := m.NegotiatedVideoCodecName()
	var rec service.BenchRecorder
	run := &service.BenchRun{Backend: *label, ExpectedFPS: *fps, Width: *width, Height: *height,
		StartupMs: float64(startup.Milliseconds()), Codec: codec}
	rec.Start(run)
	time.Sleep(time.Duration(*seconds) * time.Second)
	rec.Stop()
	_ = m.Disconnect()

	met := service.AnalyzeBenchRun(run)
	out := map[string]any{
		"label": *label, "requested": *mode, "negotiated": codec, "bitrate_kbps": *bitrate,
		"startup_ms": met.StartupMs, "frames": met.Frames, "fps": met.AvgFPS, "low1_fps": met.Low1FPS,
		"interval_p50_ms": met.IntervalP50, "interval_p99_ms": met.IntervalP99, "interval_max_ms": met.IntervalMax,
		"stalls": met.StallCount, "stall_total_ms": met.StallTotalMs, "hitches": met.Hitches,
		"host_ms_avg": met.HostLatencyAvg, "host_ms_p95": met.HostLatencyP95, "host_timing_ok": met.HostTimingOK,
		"transfer_ms_avg": met.TransferAvg, "transfer_ms_p95": met.TransferP95,
		"jitter_ms_avg": met.NetJitterAvg, "jitter_ms_p95": met.NetJitterP95,
		"rtt_ms_avg": met.RTTAvg, "playout_ms_avg": met.PlayoutDelayAvg,
		"bitrate_mbps": met.BitrateMbps, "loss_pct": met.PacketLossPct,
		"fec_recovered": met.FecRecovered, "fec_failed": met.FecFailed,
		"lost_frames": met.LostFrames, "recovery_avg_ms": met.RecoveryAvgMs, "idr": met.IDRFrames,
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
}
