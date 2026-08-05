package tailscale

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeErrStop is the sentinel read() returns once the fake source is
// exhausted, telling pumpBurstTolerant to stop.
var fakeErrStop = errors.New("fake source exhausted")

// TestPumpBurstTolerantDeliversUnderNormalLoad reproduces the keyframe-burst
// shape that triggered client-side FEC failures ("N received < M needed"):
// a large batch of same-sized RTP-like packets arriving back-to-back, with a
// write side fast enough to keep up. Every packet must be delivered, in
// order, with no drops -- this is the case a synchronous read-then-write
// relay already handled correctly; pumpBurstTolerant must not regress it.
func TestPumpBurstTolerantDeliversUnderNormalLoad(t *testing.T) {
	const burstSize = 2000
	packets := make([][]byte, burstSize)
	for i := range packets {
		packets[i] = bytes.Repeat([]byte{byte(i % 251)}, 1200) // ~MTU-sized
	}

	var mu sync.Mutex
	var received [][]byte
	idx := 0
	read := func(buf []byte) (int, error) {
		if idx >= len(packets) {
			return 0, fakeErrStop
		}
		n := copy(buf, packets[idx])
		idx++
		return n, nil
	}
	write := func(packet []byte) {
		cp := append([]byte(nil), packet...)
		mu.Lock()
		received = append(received, cp)
		mu.Unlock()
	}

	pumpBurstTolerant(read, write, 4096, nil)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != burstSize {
		t.Fatalf("delivered %d/%d packets under normal load (queue depth exceeds burst size, so this must be zero-drop)", len(received), burstSize)
	}
	for i, pkt := range received {
		if !bytes.Equal(pkt, packets[i]) {
			t.Fatalf("packet %d corrupted or reordered", i)
		}
	}
}

// TestPumpBurstTolerantDropsInsteadOfBlockingWhenQueueFull is the actual
// regression this pump exists to fix: when the write side (analogous to
// tsnet's userspace WireGuard write, which can be slow relative to a raw
// kernel socket read) can't keep up with a burst larger than the queue, the
// read loop must keep draining and report drops via onDrop instead of
// blocking -- a blocking read loop is what let the OS receive buffer
// upstream overflow and silently drop the tail of every Sunshine keyframe.
func TestPumpBurstTolerantDropsInsteadOfBlockingWhenQueueFull(t *testing.T) {
	const burstSize = 5000
	const queueDepth = 8

	var mu sync.Mutex
	idx := 0
	read := func(buf []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		if idx >= burstSize {
			return 0, fakeErrStop
		}
		idx++
		return copy(buf, []byte{1, 2, 3}), nil
	}
	readIdx := func() int {
		mu.Lock()
		defer mu.Unlock()
		return idx
	}

	blockWrite := make(chan struct{})
	writeStarted := make(chan struct{}, 1)
	write := func(packet []byte) {
		select {
		case writeStarted <- struct{}{}:
		default:
		}
		<-blockWrite // held closed until the read side has finished the burst
	}

	var droppedMu sync.Mutex
	dropped := 0
	onDrop := func(n int) {
		droppedMu.Lock()
		dropped = n
		droppedMu.Unlock()
	}
	readDropped := func() int {
		droppedMu.Lock()
		defer droppedMu.Unlock()
		return dropped
	}

	done := make(chan struct{})
	go func() {
		pumpBurstTolerant(read, write, queueDepth, onDrop)
		close(done)
	}()

	select {
	case <-writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("write side never started")
	}

	// The read loop must finish the whole (queue-exceeding) burst without
	// blocking on the stuck writer -- this is the property under test.
	select {
	case <-time.After(2 * time.Second):
		t.Fatal("read loop blocked instead of draining past a full queue -- this is the exact bug (kernel recv buffer overflow) this pump exists to prevent")
	default:
	}
	for i := 0; i < 50 && readIdx() < burstSize; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if got := readIdx(); got != burstSize {
		t.Fatalf("read loop stalled at packet %d/%d instead of draining the burst", got, burstSize)
	}
	if readDropped() == 0 {
		t.Fatal("expected drops once queue capacity was exceeded by a stalled writer, got none")
	}

	close(blockWrite)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pump never returned after read exhausted and writer unblocked")
	}
}
