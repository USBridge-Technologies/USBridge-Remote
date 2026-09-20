//go:build usbpass_gousb

package usbpass

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// newTicketTestBackend builds a gousbBackend with just enough state
// initialized to exercise beginCycle/endCycle's ticket turnstile and
// bulkSem — no real libusb device involved, since none of that logic
// touches b.intf/b.dev.
func newTicketTestBackend() *gousbBackend {
	b := &gousbBackend{
		bulkSem: make(chan struct{}, 1),
	}
	b.ticketCond = sync.NewCond(&b.ticketMu)
	return b
}

// TestBeginCycleAdmitsInTicketOrder is the regression test for the bug this
// session actually found live: server.go dispatches each CMD_SUBMIT on its
// own goroutine, so nothing guarantees those goroutines reach beginCycle in
// the order their frames were parsed off the wire — only bulkTicketKey does.
// This starts N "new CBW" callers with tickets 0..N-1 handed out in order,
// but deliberately calls beginCycle on them from goroutines released in
// reverse/random order (simulating adverse Go scheduling), and asserts they
// are still admitted into their cycle in ticket order — i.e. strictly 0, 1,
// 2, ... — matching the order Windows actually sent the CBWs in.
func TestBeginCycleAdmitsInTicketOrder(t *testing.T) {
	b := newTicketTestBackend()
	const n = 50

	var mu sync.Mutex
	var admitted []uint64
	var startWG sync.WaitGroup
	var doneWG sync.WaitGroup
	release := make(chan struct{})

	startWG.Add(n)
	doneWG.Add(n)
	for i := n - 1; i >= 0; i-- { // spawn in reverse order on purpose
		ticket := uint64(i)
		go func() {
			defer doneWG.Done()
			ctx := context.WithValue(context.Background(), bulkTicketKey{}, ticket)
			startWG.Done()
			<-release // all goroutines fight to enter beginCycle at once
			// Jitter which goroutine's select/channel-send actually runs
			// first, on top of whatever order the Go scheduler already
			// picked independent of spawn order.
			time.Sleep(time.Duration(rand.Intn(200)) * time.Microsecond)
			if !b.beginCycle(ctx, true) {
				t.Errorf("ticket %d: beginCycle returned false", ticket)
				return
			}
			mu.Lock()
			admitted = append(admitted, ticket)
			mu.Unlock()
			b.endCycle()
		}()
	}
	startWG.Wait()
	close(release)
	doneWG.Wait()

	if len(admitted) != n {
		t.Fatalf("got %d admissions, want %d", len(admitted), n)
	}
	for i, ticket := range admitted {
		if ticket != uint64(i) {
			t.Fatalf("admitted out of ticket order: position %d got ticket %d, want %d (full order: %v)",
				i, ticket, i, admitted)
		}
	}
}

// TestBeginCycleContinuationPassesThroughInOrder checks the fast path
// (newCBW=false, cycle already held) also participates in ticket ordering
// instead of being skipped, which would leave nowServingTicket stuck and
// deadlock any later new-CBW ticket — see beginCycle's own doc comment on
// why every ticket must advance the turnstile.
func TestBeginCycleContinuationPassesThroughInOrder(t *testing.T) {
	b := newTicketTestBackend()

	ctx0 := context.WithValue(context.Background(), bulkTicketKey{}, uint64(0))
	if !b.beginCycle(ctx0, true) {
		t.Fatal("ticket 0 (new CBW) should be admitted immediately")
	}

	// Continuation URBs of the same cycle, tickets 1 and 2 — must not
	// block even though they never touch bulkSem.
	ctx1 := context.WithValue(context.Background(), bulkTicketKey{}, uint64(1))
	done1 := make(chan bool, 1)
	go func() { done1 <- b.beginCycle(ctx1, false) }()
	select {
	case ok := <-done1:
		if !ok {
			t.Fatal("ticket 1 (continuation) beginCycle returned false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ticket 1 (continuation) beginCycle blocked — turnstile stuck")
	}

	ctx2 := context.WithValue(context.Background(), bulkTicketKey{}, uint64(2))
	done2 := make(chan bool, 1)
	go func() { done2 <- b.beginCycle(ctx2, false) }()
	select {
	case ok := <-done2:
		if !ok {
			t.Fatal("ticket 2 (continuation) beginCycle returned false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ticket 2 (continuation) beginCycle blocked — turnstile stuck")
	}
	b.endCycle() // closes ticket 0's cycle

	// A pipelined next new-CBW, ticket 3, must now be admitted (not stuck
	// behind the gap left by the fast-pathed tickets 1 and 2).
	ctx3 := context.WithValue(context.Background(), bulkTicketKey{}, uint64(3))
	done3 := make(chan bool, 1)
	go func() { done3 <- b.beginCycle(ctx3, true) }()
	select {
	case ok := <-done3:
		if !ok {
			t.Fatal("ticket 3 (new CBW) beginCycle returned false")
		}
		b.endCycle()
	case <-time.After(2 * time.Second):
		t.Fatal("ticket 3 (new CBW) beginCycle blocked — nowServingTicket stuck on a gap")
	}
}

// TestBeginCycleLaterTicketWaitsForEarlierPipelinedCycle is the direct
// analogue of the live symptom: a later-ticketed new CBW arrives (goroutine
// scheduled) *before* an earlier-ticketed one has managed to call
// beginCycle at all. The later one must still block until the earlier one
// has been admitted and released its cycle — proving the turnstile is
// actually held across the bulkSem acquisition, not just at entry.
func TestBeginCycleLaterTicketWaitsForEarlierPipelinedCycle(t *testing.T) {
	b := newTicketTestBackend()

	var order []uint64
	var mu sync.Mutex

	laterDone := make(chan struct{})
	go func() {
		ctx := context.WithValue(context.Background(), bulkTicketKey{}, uint64(1))
		if !b.beginCycle(ctx, true) {
			t.Errorf("ticket 1: beginCycle returned false")
			return
		}
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
		b.endCycle()
		close(laterDone)
	}()

	// Give the later ticket's goroutine every chance to run first.
	time.Sleep(50 * time.Millisecond)

	select {
	case <-laterDone:
		t.Fatal("ticket 1 was admitted before ticket 0 ever ran — turnstile did not hold it back")
	default:
	}

	ctx0 := context.WithValue(context.Background(), bulkTicketKey{}, uint64(0))
	if !b.beginCycle(ctx0, true) {
		t.Fatal("ticket 0: beginCycle returned false")
	}
	mu.Lock()
	order = append(order, 0)
	mu.Unlock()
	b.endCycle()

	select {
	case <-laterDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ticket 1 never got admitted after ticket 0 released")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != 0 || order[1] != 1 {
		t.Fatalf("admission order = %v, want [0 1]", order)
	}
}
