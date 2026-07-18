package clipboard

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// pollInterval balances responsiveness against cost: desktop backends read
// a cheap native sequence counter every tick (negligible cost), while the
// Linux backend shells out to check available clipboard targets — frequent
// enough to feel instant, not so frequent it spawns a process needlessly.
const pollInterval = 800 * time.Millisecond

var errPayloadTooLarge = errors.New("clipboard: payload exceeds configured size limit")

// Manager polls a native Backend for local clipboard changes and applies
// remote changes back to it, while preventing the two directions from
// echoing each other into an infinite loop.
type Manager struct {
	backend  Backend
	maxBytes int64
	enabled  atomic.Bool

	// backendMu serializes all access to backend. Run's poll goroutine reads
	// it on a timer while Apply (driven by the transport's WS goroutine) can
	// write to it at any moment; without this lock the two race on the native
	// clipboard. On Windows in particular a racing OpenClipboard call loses
	// and gives up after a short retry budget, so a concurrent Read() could
	// silently fail an incoming Apply() and leave stale content in place.
	backendMu sync.Mutex

	mu                   sync.Mutex
	lastAppliedHash      string
	onLocalChange        func(Content)
	onLocalChangePending func(PendingInfo)
}

// PendingInfo is a cheap, best-effort summary of an in-progress local
// clipboard change — sent via SetOnLocalChangePending before the (possibly
// slow) full read has finished, purely so a peer can react instantly instead
// of appearing to hang until the real transfer completes.
type PendingInfo struct {
	Kind       Kind
	Count      int
	ApproxSize int64
}

// NewManager wraps backend with change polling and loop prevention. Sync
// starts enabled; call SetEnabled(false) to pause it.
func NewManager(backend Backend, maxBytes int64) *Manager {
	m := &Manager{backend: backend, maxBytes: maxBytes}
	m.enabled.Store(true)
	return m
}

func (m *Manager) SetEnabled(enabled bool) { m.enabled.Store(enabled) }
func (m *Manager) Enabled() bool           { return m.enabled.Load() }

// SetOnLocalChange registers the callback invoked with newly observed local
// clipboard content that didn't originate from a recent Apply call. Safe to
// call from a different goroutine than Run (e.g. a transport's connect/
// disconnect handler); pass nil to stop pushing (no active peer to send to).
func (m *Manager) SetOnLocalChange(fn func(Content)) {
	m.mu.Lock()
	m.onLocalChange = fn
	m.mu.Unlock()
}

// SetOnLocalChangePending registers the callback invoked as soon as a local
// file/directory clipboard change is detected, before Read() has actually
// loaded its content — only backends implementing FileEnumerator support
// this (see backend.go); on others it's simply never called. Same calling
// convention as SetOnLocalChange: safe from another goroutine, nil to stop.
func (m *Manager) SetOnLocalChangePending(fn func(PendingInfo)) {
	m.mu.Lock()
	m.onLocalChangePending = fn
	m.mu.Unlock()
}

// Run polls until ctx is cancelled. Call it in its own goroutine.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var lastStamp string
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !m.enabled.Load() {
				continue
			}
			m.backendMu.Lock()
			stamp, err := m.backend.ChangeStamp()
			if err != nil || stamp == lastStamp {
				m.backendMu.Unlock()
				continue
			}

			m.firePendingLocked()

			content, ok, err := m.backend.Read()
			m.backendMu.Unlock()
			if err != nil {
				// Don't advance lastStamp: this stamp might not have been
				// genuinely unreadable, just transiently so (e.g. Android
				// blocks clipboard reads from backgrounded apps — see
				// backend_android.go's "blocked" case). Retrying the same
				// stamp next tick means a change that happened while
				// unreadable eventually gets picked up instead of being
				// silently lost until some later, unrelated change bumps
				// the stamp again.
				log.Printf("[clipboard] read failed: %v", err)
				continue
			}
			lastStamp = stamp
			if !ok || content.Empty() || content.Size() > m.maxBytes {
				continue
			}

			hash := content.Hash()
			m.mu.Lock()
			isEcho := hash == m.lastAppliedHash
			cb := m.onLocalChange
			m.mu.Unlock()
			if isEcho {
				continue
			}
			if cb != nil {
				cb(content)
			}
		}
	}
}

// firePendingLocked calls the backend's cheap file enumeration (if it
// supports one) and, if there's anything there, fires onLocalChangePending
// with a summary. Called with backendMu already held, right after detecting
// a changed stamp but before the (possibly slow) full Read().
func (m *Manager) firePendingLocked() {
	fe, ok := m.backend.(FileEnumerator)
	if !ok {
		return
	}
	files, ok := fe.EnumerateFiles()
	if !ok || len(files) == 0 {
		return
	}
	var total int64
	for _, f := range files {
		total += f.Size
	}
	m.mu.Lock()
	cb := m.onLocalChangePending
	m.mu.Unlock()
	if cb != nil {
		cb(PendingInfo{Kind: KindFile, Count: len(files), ApproxSize: total})
	}
}

// Snapshot reads whatever is currently on the local clipboard right now,
// bypassing the change-stamp tracking Run uses. Callers use this to resync a
// peer immediately after a connection is (re)established: Run only pushes on
// the *edge* of a detected change, so a local change that occurred (or that
// failed to send) while no connection was up would otherwise never be
// retried, since the poll loop already considers that change stamp "seen".
// Returns ok=false if there's nothing worth sending (empty, unreadable, or
// over the size cap).
func (m *Manager) Snapshot() (Content, bool) {
	m.backendMu.Lock()
	content, ok, err := m.backend.Read()
	m.backendMu.Unlock()
	if err != nil || !ok || content.Empty() || content.Size() > m.maxBytes {
		return Content{}, false
	}
	return content, true
}

// Apply writes remote content to the local clipboard, remembering its hash
// so the poll loop above recognizes the resulting local change as its own
// echo instead of bouncing it straight back to the sender.
func (m *Manager) Apply(content Content) error {
	if content.Size() > m.maxBytes {
		return errPayloadTooLarge
	}
	hash := content.Hash()
	m.mu.Lock()
	m.lastAppliedHash = hash
	m.mu.Unlock()
	m.backendMu.Lock()
	defer m.backendMu.Unlock()
	return m.backend.Write(content)
}
