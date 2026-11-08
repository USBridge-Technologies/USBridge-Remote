package mobile

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDoBeforeRunQueuesOffMain(t *testing.T) {
	d := NewGoMobileDriver().(*driver)
	var ran atomic.Bool
	done := make(chan struct{})
	go func() {
		d.DoFromGoroutine(func() { ran.Store(true) }, false)
		close(done)
	}()
	<-done
	time.Sleep(20 * time.Millisecond)
	if ran.Load() {
		t.Fatal("ran on the calling goroutine before the event loop started")
	}
	select {
	case fn := <-d.queuedFuncs.Out():
		fn()
	case <-time.After(time.Second):
		t.Fatal("not queued")
	}
	if !ran.Load() {
		t.Fatal("queued function did not run")
	}
}

func TestDoBeforeRunStaysInlineOnMain(t *testing.T) {
	d := NewGoMobileDriver().(*driver)
	ran := false
	d.DoFromGoroutine(func() { ran = true }, false)
	if !ran {
		t.Fatal("main must run directly while it builds the window")
	}
}

func TestDoAndWaitBeforeRunWaitsForLoop(t *testing.T) {
	d := NewGoMobileDriver().(*driver)
	done := make(chan struct{})
	go func() {
		d.DoFromGoroutine(func() {}, true)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("returned before the event loop ran it")
	case fn := <-d.queuedFuncs.Out():
		fn()
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waiter was not released")
	}
}
