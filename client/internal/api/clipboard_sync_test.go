package api

import (
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"usbridge-client/internal/clipboard"
)

// memBackend is an in-memory clipboard.Backend so these tests never touch the
// real OS clipboard.
type memBackend struct {
	mu      sync.Mutex
	content clipboard.Content
	stamp   int
}

func (b *memBackend) ChangeStamp() (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strconv.Itoa(b.stamp), nil
}

func (b *memBackend) Read() (clipboard.Content, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.content, !b.content.Empty(), nil
}

func (b *memBackend) Write(c clipboard.Content) error {
	b.set(c)
	return nil
}

func (b *memBackend) set(c clipboard.Content) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.content = c
	b.stamp++
}

func (b *memBackend) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.content.Text
}

// fakeAgent is the agent end of the clipboard DataChannel: one JSON event
// per message over a net.Pipe, exactly what dcJSONConn speaks.
type fakeAgent struct {
	t      *testing.T
	conn   net.Conn
	events chan ClipboardEvent
}

func (a *fakeAgent) send(ev ClipboardEvent) {
	a.t.Helper()
	data, err := json.Marshal(ev)
	if err != nil {
		a.t.Fatal(err)
	}
	if _, err := a.conn.Write(data); err != nil {
		a.t.Fatalf("agent write: %v", err)
	}
}

func (a *fakeAgent) recv(timeout time.Duration) (ClipboardEvent, bool) {
	select {
	case ev := <-a.events:
		return ev, true
	case <-time.After(timeout):
		return ClipboardEvent{}, false
	}
}

// startTestSync runs a ClipboardSync against a fakeAgent and waits until the
// connection is live.
func startTestSync(t *testing.T, auto bool, local clipboard.Content) (*ClipboardSync, *memBackend, *fakeAgent) {
	t.Helper()
	backend := &memBackend{content: local}
	cs := NewClipboardSync(&USBClient{}, clipboard.NewManager(backend, 1<<20), 1<<20)
	cs.SetEnabled(auto)

	agentEnd, clientEnd := net.Pipe()
	agent := &fakeAgent{t: t, conn: agentEnd, events: make(chan ClipboardEvent, 16)}
	go func() {
		dec := json.NewDecoder(agentEnd)
		for {
			var ev ClipboardEvent
			if err := dec.Decode(&ev); err != nil {
				return
			}
			agent.events <- ev
		}
	}()
	var once sync.Once
	cs.SetOpenDataChannel(func(string) (net.Conn, error) {
		var conn net.Conn
		once.Do(func() { conn = clientEnd })
		if conn == nil {
			return nil, errors.New("test: single connection only")
		}
		return conn, nil
	})
	cs.Start()
	t.Cleanup(func() {
		cs.Stop()
		agentEnd.Close()
	})

	deadline := time.Now().Add(3 * time.Second)
	for {
		cs.mu.Lock()
		live := cs.send != nil
		cs.mu.Unlock()
		if live {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("clipboard sync never connected")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cs, backend, agent
}

func waitText(t *testing.T, b *memBackend, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for b.text() != want {
		if time.Now().After(deadline) {
			t.Fatalf("local clipboard = %q, want %q", b.text(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func textContent(s string) clipboard.Content {
	return clipboard.Content{Kind: clipboard.KindText, Text: s}
}

func TestClipboardSync_NotConnected(t *testing.T) {
	cs := NewClipboardSync(&USBClient{}, clipboard.NewManager(&memBackend{}, 1<<20), 1<<20)
	if err := cs.PushNow(); !errors.Is(err, ErrClipboardNotConnected) {
		t.Fatalf("PushNow = %v, want ErrClipboardNotConnected", err)
	}
	if err := cs.PullNow(); !errors.Is(err, ErrClipboardNotConnected) {
		t.Fatalf("PullNow = %v, want ErrClipboardNotConnected", err)
	}
}

func TestClipboardSync_AutoResyncsAndAppliesIncoming(t *testing.T) {
	_, backend, agent := startTestSync(t, true, textContent("local"))

	ev, ok := agent.recv(2 * time.Second)
	if !ok || ev.Kind != string(clipboard.KindText) || ev.Text != "local" {
		t.Fatalf("expected connect-time resync of local text, got %+v ok=%v", ev, ok)
	}
	agent.send(ClipboardEvent{Kind: string(clipboard.KindText), Text: "remote"})
	waitText(t, backend, "remote")
}

func TestClipboardSync_ManualSendsNothingAndKeepsIncoming(t *testing.T) {
	_, backend, agent := startTestSync(t, false, textContent("local"))

	agent.send(ClipboardEvent{Kind: string(clipboard.KindText), Text: "remote"})
	backend.set(textContent("local-changed"))
	// Longer than the manager's poll interval: a local change must not leak.
	if ev, ok := agent.recv(1200 * time.Millisecond); ok {
		t.Fatalf("manual mode sent %+v on its own", ev)
	}
	if got := backend.text(); got != "local-changed" {
		t.Fatalf("manual mode applied a remote change: local = %q", got)
	}
}

func TestClipboardSync_PushNowSendsLocalClipboard(t *testing.T) {
	cs, _, agent := startTestSync(t, false, textContent("send me"))

	if err := cs.PushNow(); err != nil {
		t.Fatalf("PushNow: %v", err)
	}
	ev, ok := agent.recv(2 * time.Second)
	if !ok || ev.Kind != string(clipboard.KindText) || ev.Text != "send me" || ev.Hash == "" {
		t.Fatalf("unexpected pushed event %+v ok=%v", ev, ok)
	}
}

func TestClipboardSync_PushNowEmptyClipboard(t *testing.T) {
	cs, _, _ := startTestSync(t, false, clipboard.Content{})
	if err := cs.PushNow(); !errors.Is(err, ErrClipboardEmpty) {
		t.Fatalf("PushNow = %v, want ErrClipboardEmpty", err)
	}
}

func TestClipboardSync_PullNowRequestsAndApplies(t *testing.T) {
	cs, backend, agent := startTestSync(t, false, textContent("local"))

	done := make(chan error, 1)
	go func() { done <- cs.PullNow() }()

	ev, ok := agent.recv(2 * time.Second)
	if !ok || ev.Kind != clipboardRequestKind {
		t.Fatalf("expected a request event, got %+v ok=%v", ev, ok)
	}
	agent.send(ClipboardEvent{Kind: string(clipboard.KindText), Text: "fresh"})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PullNow: %v", err)
		}
	case <-time.After(pullReplyTimeout + time.Second):
		t.Fatal("PullNow did not return")
	}
	// PullNow returns only after the reply was applied.
	if got := backend.text(); got != "fresh" {
		t.Fatalf("local clipboard = %q, want %q", got, "fresh")
	}

	// Manual mode again afterwards: the next remote change stays remote.
	agent.send(ClipboardEvent{Kind: string(clipboard.KindText), Text: "later"})
	time.Sleep(200 * time.Millisecond)
	if got := backend.text(); got != "fresh" {
		t.Fatalf("remote change applied outside a pull: local = %q", got)
	}
}

func TestClipboardSync_PullNowFallsBackToLastRemote(t *testing.T) {
	old := pullReplyTimeout
	pullReplyTimeout = 150 * time.Millisecond
	t.Cleanup(func() { pullReplyTimeout = old })

	cs, backend, agent := startTestSync(t, false, textContent("local"))
	agent.send(ClipboardEvent{Kind: string(clipboard.KindText), Text: "announced"})
	time.Sleep(100 * time.Millisecond)

	// An agent without request support never answers.
	if err := cs.PullNow(); err != nil {
		t.Fatalf("PullNow: %v", err)
	}
	if got := backend.text(); got != "announced" {
		t.Fatalf("local clipboard = %q, want %q", got, "announced")
	}
}

func TestClipboardSync_PullNowNothingKnown(t *testing.T) {
	old := pullReplyTimeout
	pullReplyTimeout = 100 * time.Millisecond
	t.Cleanup(func() { pullReplyTimeout = old })

	cs, _, _ := startTestSync(t, false, textContent("local"))
	if err := cs.PullNow(); !errors.Is(err, ErrClipboardEmpty) {
		t.Fatalf("PullNow = %v, want ErrClipboardEmpty", err)
	}
}

func TestClipboardSync_SwitchingToAutoDoesNotEchoPulledContent(t *testing.T) {
	cs, _, agent := startTestSync(t, false, textContent("local"))

	go func() { _ = cs.PullNow() }()
	if ev, ok := agent.recv(2 * time.Second); !ok || ev.Kind != clipboardRequestKind {
		t.Fatalf("expected a request event, got %+v ok=%v", ev, ok)
	}
	agent.send(ClipboardEvent{Kind: string(clipboard.KindText), Text: "pulled"})
	time.Sleep(200 * time.Millisecond)

	cs.SetEnabled(true)
	if ev, ok := agent.recv(1200 * time.Millisecond); ok {
		t.Fatalf("auto mode echoed pulled content back: %+v", ev)
	}
}
