package benchrender

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"
)

// stampNow is the clock the time barcode is drawn from: this machine's wall
// clock, or with -sync another host's (a KVM's), so the client can compare a
// barcode against the clock it reads from that host.
var stampNow = time.Now

// remoteClock follows another host's GET /api/bench/time ({"us": ...}) on this
// machine's monotonic clock, so neither this machine's NTP slewing nor a step of
// its wall clock moves the barcode.
//
// Each reading is the quickest of a burst of requests (the least queueing, so its
// midpoint is the closest to when the host read its clock). The model is a line
// through the recent quick readings: host_us = a + b*mono_us. b carries the two
// crystals' drift (tens of ppm, i.e. tens of microseconds a second), which a bare
// offset would let pile up between readings.
type remoteClock struct {
	url    string
	client *http.Client
	start  time.Time // monotonic origin

	mu    sync.RWMutex
	ok    bool
	a, b  float64
	reads []clockReading
}

type clockReading struct {
	mono, host, rtt float64 // microseconds
}

const (
	syncEvery  = 400 * time.Millisecond
	syncBurst  = 6
	syncWindow = 20 * time.Second
	maxDrift   = 500e-6
)

func newRemoteClock(url string) *remoteClock {
	return &remoteClock{
		url: url,
		client: &http.Client{
			Timeout:   time.Second,
			Transport: &http.Transport{MaxIdleConnsPerHost: 1, DisableCompression: true},
		},
		start: time.Now(),
		b:     1,
	}
}

func (c *remoteClock) mono() float64 { return float64(time.Since(c.start).Nanoseconds()) / 1e3 }

// Now is the remote host's clock right now (this machine's until the first reading).
func (c *remoteClock) Now() time.Time {
	c.mu.RLock()
	ok, a, b := c.ok, c.a, c.b
	c.mu.RUnlock()
	if !ok {
		return time.Now()
	}
	us := a + b*c.mono()
	sec := math.Floor(us / 1e6)
	return time.Unix(int64(sec), int64((us-sec*1e6)*1e3))
}

func (c *remoteClock) readOnce() (clockReading, error) {
	t0 := c.mono()
	resp, err := c.client.Get(c.url)
	if err != nil {
		return clockReading{}, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	resp.Body.Close()
	t1 := c.mono()
	if err != nil {
		return clockReading{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return clockReading{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var v struct {
		Us int64 `json:"us"`
	}
	if err := json.Unmarshal(body, &v); err != nil || v.Us <= 0 {
		return clockReading{}, fmt.Errorf("no time in %q", body)
	}
	return clockReading{mono: (t0 + t1) / 2, host: float64(v.Us), rtt: t1 - t0}, nil
}

// sync takes one burst and refits the model.
func (c *remoteClock) sync() error {
	best := clockReading{rtt: math.MaxFloat64}
	var lastErr error
	for i := 0; i < syncBurst; i++ {
		r, err := c.readOnce()
		if err != nil {
			lastErr = err
			continue
		}
		if r.rtt < best.rtt {
			best = r
		}
	}
	if best.rtt == math.MaxFloat64 {
		return lastErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// The host's clock was stepped (ntpd), or this one's monotonic clock jumped
	// (suspend): the old readings describe a different line.
	if c.ok && best.rtt < 2000 && math.Abs(best.host-(c.a+c.b*best.mono)) > 1000+best.rtt/2 {
		log.Printf("[bench-render] clock %s jumped by %+.0f us, starting over", c.url, best.host-(c.a+c.b*best.mono))
		c.reads = c.reads[:0]
	}
	c.reads = append(c.reads, best)
	cut := best.mono - float64(syncWindow.Microseconds())
	for len(c.reads) > 0 && c.reads[0].mono < cut {
		c.reads = c.reads[1:]
	}
	c.a, c.b = fitClock(c.reads)
	c.ok = true
	return nil
}

// fitClock fits host = a + b*mono through the quickest readings (rtt within the
// window's lowest quarter): a slow reading's midpoint can be off by half its rtt.
func fitClock(reads []clockReading) (a, b float64) {
	rtts := make([]float64, len(reads))
	for i, r := range reads {
		rtts[i] = r.rtt
	}
	sort.Float64s(rtts)
	limit := rtts[len(rtts)/4] + 30 // us
	var use []clockReading
	for _, r := range reads {
		if r.rtt <= limit {
			use = append(use, r)
		}
	}
	// Fit around the first reading so the sums stay small and exact.
	m0, h0 := use[0].mono, use[0].host
	n := float64(len(use))
	var sx, sy, sxx, sxy float64
	for _, r := range use {
		x, y := r.mono-m0, r.host-h0-(r.mono-m0)
		sx, sy, sxx, sxy = sx+x, sy+y, sxx+x*x, sxy+x*y
	}
	drift := 0.0
	if den := n*sxx - sx*sx; len(use) >= 4 && use[len(use)-1].mono-m0 > 2e6 && den > 0 {
		drift = (n*sxy - sx*sy) / den
		drift = math.Max(-maxDrift, math.Min(maxDrift, drift))
	}
	off := (sy - drift*sx) / n // mean of (host - mono) after the drift
	b = 1 + drift
	a = h0 + off - b*m0 // host = h0 + off + b*(mono-m0)
	return a, b
}

// stats is the model's state for the log: residual of the quick readings, best rtt.
func (c *remoteClock) stats() (resid, bestRTT, ppm float64, n int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	bestRTT = math.MaxFloat64
	for _, r := range c.reads {
		bestRTT = math.Min(bestRTT, r.rtt)
	}
	for _, r := range c.reads {
		if e := math.Abs(r.host - (c.a + c.b*r.mono)); r.rtt <= bestRTT+30 && e > resid {
			resid = e
		}
	}
	return resid, bestRTT, (c.b - 1) * 1e6, len(c.reads)
}

// startClockSync points stampNow at url's clock, once the first reading is in (or
// gives up after a few seconds, and keeps trying in the background).
func startClockSync(url string) {
	c := newRemoteClock(url)
	first := make(chan struct{})
	go func() {
		var once sync.Once
		lastLog := time.Time{}
		for {
			if err := c.sync(); err != nil {
				log.Printf("[bench-render] clock %s: %v", url, err)
			} else {
				once.Do(func() { close(first) })
				if time.Since(lastLog) > 5*time.Second {
					lastLog = time.Now()
					resid, rtt, ppm, n := c.stats()
					off := c.Now().Sub(time.Now())
					log.Printf("[bench-render] clock %s: offset %+.1f us, drift %+.2f ppm, best rtt %.0f us, fit error %.0f us (%d readings)",
						url, float64(off.Nanoseconds())/1e3, ppm, rtt, resid, n)
				}
			}
			time.Sleep(syncEvery)
		}
	}()
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		log.Printf("[bench-render] clock %s: no reading yet, drawing this machine's clock until one comes in", url)
	}
	stampNow = c.Now
}
