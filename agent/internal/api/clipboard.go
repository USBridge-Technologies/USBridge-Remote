package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"usbridge_agent/internal/clipboard"
)

const (
	blobIDLength = 32 // hex-encoded 16 random bytes
	blobTTL      = 10 * time.Minute
)

// clipboardBlobStore holds clipboard image/file bytes on disk between
// "announced over /api/clipboard/ws" and "fetched/consumed by the peer",
// entirely off the WS signaling channel so a large transfer never blocks a
// subsequent small clipboard-changed notification behind it. The agent is
// the only HTTP server in this architecture, so it's the single place both
// directions land: content the agent itself produced (served via GET to the
// client) and content the client pushed (received via PUT, then applied
// locally).
type clipboardBlobStore struct {
	dir string

	mu    sync.Mutex
	files map[string]time.Time // id -> stored-at, for TTL cleanup
}

func newClipboardBlobStore() *clipboardBlobStore {
	dir, err := os.MkdirTemp("", "usbridge-clipboard-blobs-*")
	if err != nil {
		dir = os.TempDir()
	}
	s := &clipboardBlobStore{dir: dir, files: make(map[string]time.Time)}
	go s.reapLoop()
	return s
}

func (s *clipboardBlobStore) reapLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for id, at := range s.files {
			if time.Since(at) > blobTTL {
				os.Remove(filepath.Join(s.dir, id))
				delete(s.files, id)
			}
		}
		s.mu.Unlock()
	}
}

func newBlobID() string {
	b := make([]byte, blobIDLength/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func validBlobID(id string) bool {
	if len(id) != blobIDLength {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// SaveReader streams src (capped at maxBytes) into the blob named id.
func (s *clipboardBlobStore) SaveReader(id string, src io.Reader, maxBytes int64) (int64, error) {
	path := filepath.Join(s.dir, id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(src, maxBytes+1))
	if err != nil {
		os.Remove(path)
		return 0, err
	}
	if n > maxBytes {
		os.Remove(path)
		return 0, errors.New("clipboard: blob exceeds configured size limit")
	}
	s.mu.Lock()
	s.files[id] = time.Now()
	s.mu.Unlock()
	return n, nil
}

// SaveBytes stores an in-memory blob (agent-originated content that's
// already fully in memory from a Backend.Read() call).
func (s *clipboardBlobStore) SaveBytes(id string, data []byte) error {
	path := filepath.Join(s.dir, id)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	s.mu.Lock()
	s.files[id] = time.Now()
	s.mu.Unlock()
	return nil
}

func (s *clipboardBlobStore) Open(id string) (*os.File, int64, error) {
	f, err := os.Open(filepath.Join(s.dir, id))
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

func (s *clipboardBlobStore) Delete(id string) {
	s.mu.Lock()
	delete(s.files, id)
	s.mu.Unlock()
	os.Remove(filepath.Join(s.dir, id))
}

func (s *Server) clipboardMaxBytesOrDefault() int64 {
	if n := s.app.ClipboardMaxBytes(); n > 0 {
		return n
	}
	return 200 * 1024 * 1024
}

func (s *Server) clipboardBlobPut(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validBlobID(id) {
		http.Error(w, "invalid blob id", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	if _, err := s.clipboardBlobs.SaveReader(id, r.Body, s.clipboardMaxBytesOrDefault()); err != nil {
		log.Printf("[api] clipboard blob upload failed id=%s: %v", id, err)
		http.Error(w, "upload failed", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clipboardBlobGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validBlobID(id) {
		http.Error(w, "invalid blob id", http.StatusBadRequest)
		return
	}
	f, size, err := s.clipboardBlobs.Open(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	_, _ = io.Copy(w, f)
}

// clipboardWS is the persistent duplex JSON signaling channel: local
// clipboard changes are pushed out as they happen, and incoming events are
// applied to the local clipboard. It carries no payload bytes itself — see
// clipboardBlobPut/Get for the actual transfer.
func (s *Server) clipboardWS(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] clipboard_ws incoming request from %s", r.RemoteAddr)
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[api] clipboard_ws upgrade failed: %v", err)
		return
	}
	log.Printf("[api] clipboard_ws upgraded successfully for %s", r.RemoteAddr)
	defer conn.Close()

	mgr := s.app.Clipboard()
	if mgr == nil {
		return
	}

	conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		return nil
	})

	var writeMu sync.Mutex
	safeWriteJSON := func(v interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}

	stopPush := make(chan struct{})
	defer close(stopPush)
	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopPush:
				return
			case <-ticker.C:
				writeMu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, []byte("ping"))
				writeMu.Unlock()
				if err != nil {
					log.Printf("[api] clipboard_ws ping failed, closing: %v", err)
					conn.Close()
					return
				}
			}
		}
	}()

	// Push local clipboard changes to this connection for as long as it's
	// open; clear the callback on disconnect so a later local change with no
	// connected peer doesn't try to write to a closed socket.
	mgr.SetOnLocalChange(func(content clipboard.Content) {
		event := ClipboardEvent{Kind: string(content.Kind), Hash: content.Hash()}
		switch content.Kind {
		case clipboard.KindText:
			event.Text = content.Text
			event.Size = int64(len(content.Text))
		case clipboard.KindImage:
			event.MimeType = "image/png"
			event.Size = int64(len(content.Image))
			id := newBlobID()
			if err := s.clipboardBlobs.SaveBytes(id, content.Image); err != nil {
				log.Printf("[api] clipboard blob store failed: %v", err)
				return
			}
			event.BlobID = id
		case clipboard.KindFile:
			data := clipboard.EncodeFiles(content.Files)
			event.Size = int64(len(data))
			if len(content.Files) > 0 {
				event.FileName = content.Files[0].Name
			}
			id := newBlobID()
			if err := s.clipboardBlobs.SaveBytes(id, data); err != nil {
				log.Printf("[api] clipboard blob store failed: %v", err)
				return
			}
			event.BlobID = id
		default:
			return
		}
		if err := safeWriteJSON(event); err != nil {
			log.Printf("[api] clipboard_ws push failed: %v", err)
		}
	})
	defer mgr.SetOnLocalChange(nil)

	for {
		var event ClipboardEvent
		if err := conn.ReadJSON(&event); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("[api] clipboard_ws read error: %v", err)
			}
			return
		}
		if err := s.applyClipboardEvent(mgr, event); err != nil {
			log.Printf("[api] clipboard_ws apply failed kind=%s: %v", event.Kind, err)
		}
	}
}

// applyClipboardEvent turns an incoming event into clipboard.Content and
// applies it. For image/file events, the client already PUT the blob to
// clipboardBlobPut before sending this event, so it's read locally here —
// no network round-trip needed on the agent's side.
func (s *Server) applyClipboardEvent(mgr *clipboard.Manager, event ClipboardEvent) error {
	switch event.Kind {
	case string(clipboard.KindText):
		return mgr.Apply(clipboard.Content{Kind: clipboard.KindText, Text: event.Text})
	case string(clipboard.KindImage):
		data, err := s.readClipboardBlob(event.BlobID)
		if err != nil {
			return err
		}
		return mgr.Apply(clipboard.Content{Kind: clipboard.KindImage, Image: data})
	case string(clipboard.KindFile):
		data, err := s.readClipboardBlob(event.BlobID)
		if err != nil {
			return err
		}
		files, err := clipboard.DecodeFiles(data)
		if err != nil {
			return err
		}
		return mgr.Apply(clipboard.Content{Kind: clipboard.KindFile, Files: files})
	default:
		return nil
	}
}

func (s *Server) readClipboardBlob(id string) ([]byte, error) {
	if !validBlobID(id) {
		return nil, errors.New("clipboard: invalid blob id")
	}
	f, _, err := s.clipboardBlobs.Open(id)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	defer s.clipboardBlobs.Delete(id)
	return io.ReadAll(f)
}
