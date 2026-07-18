package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/clipboard"
)

// ClipboardEvent mirrors the agent's api.ClipboardEvent wire format exactly
// (same JSON field names) — independently defined since the two modules
// don't share Go types. For Kind=="text", Text carries the content inline;
// for "image"/"file" this carries no bytes at all, just a BlobID + Size —
// the payload moves separately via uploadBlob/downloadBlob so a large
// transfer can never block a subsequent small clipboard-changed
// notification behind it on this same connection.
//
// Pending==true marks a fast, best-effort pre-announcement sent the moment a
// file/directory clipboard change is detected — before the (possibly slow,
// for many/large files) local read+upload has even started. It carries no
// Hash/BlobID (there's nothing to fetch yet) and must not be applied to the
// local clipboard; the real event with a usable BlobID follows once the
// transfer is ready.
type ClipboardEvent struct {
	Kind      string `json:"kind"`
	Text      string `json:"text,omitempty"`
	Hash      string `json:"hash"`
	Size      int64  `json:"size,omitempty"`
	FileName  string `json:"file_name,omitempty"`
	MimeType  string `json:"mime_type,omitempty"`
	BlobID    string `json:"blob_id,omitempty"`
	Pending   bool   `json:"pending,omitempty"`
	FileCount int    `json:"file_count,omitempty"`
}

// ClipboardSync dials the paired agent's /api/clipboard/ws signaling channel
// and keeps the local system clipboard in sync with it: local changes are
// pushed out, incoming changes are applied locally.
type ClipboardSync struct {
	client   *USBClient
	manager  *clipboard.Manager
	maxBytes int64

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
}

// NewClipboardSync wraps manager with a connection to client's paired agent.
// Call Start to begin syncing and Stop to tear it down.
func NewClipboardSync(client *USBClient, manager *clipboard.Manager, maxBytes int64) *ClipboardSync {
	return &ClipboardSync{client: client, manager: manager, maxBytes: maxBytes}
}

// SetEnabled pauses/resumes sync without tearing down the connection.
func (cs *ClipboardSync) SetEnabled(enabled bool) {
	if cs.manager != nil {
		cs.manager.SetEnabled(enabled)
	}
}

// Start connects and begins the duplex sync loop in the background,
// reconnecting with backoff until Stop is called.
func (cs *ClipboardSync) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	cs.mu.Lock()
	cs.cancel = cancel
	cs.mu.Unlock()

	go cs.manager.Run(ctx)
	go cs.connectLoop(ctx)
}

// Stop cancels the poll loop and closes the active connection, if any.
func (cs *ClipboardSync) Stop() {
	cs.mu.Lock()
	cancel := cs.cancel
	conn := cs.conn
	cs.conn = nil
	cs.cancel = nil
	cs.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		conn.Close()
	}
	if cs.manager != nil {
		cs.manager.SetOnLocalChange(nil)
	}
}

func (cs *ClipboardSync) connectLoop(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for ctx.Err() == nil {
		if err := cs.runOnce(ctx); err != nil {
			logrus.Errorf("[clipboard-sync] connection error: %v", err)
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// dialer builds a websocket.Dialer that routes through the same transport
// c.client's regular HTTP calls use. This matters when the connection is
// over Tailscale in userspace (tsnet) mode: tailscale addresses (100.x.x.x)
// have no real OS-level route — only tsnet's in-process netstack knows how
// to reach them — so a plain websocket.DefaultDialer (a bare OS dial) would
// just hang. Reusing the *http.Transport's DialContext (already wired to
// tsnet's dialer by TailscaleService.HTTPClient) makes the WS upgrade dial
// exactly the way every other request to the agent already does.
func (cs *ClipboardSync) dialer() *websocket.Dialer {
	d := &websocket.Dialer{HandshakeTimeout: 45 * time.Second}
	if cs.client != nil && cs.client.httpClient != nil {
		if t, ok := cs.client.httpClient.Transport.(*http.Transport); ok && t.DialContext != nil {
			d.NetDialContext = t.DialContext
		}
	}
	return d
}

func (cs *ClipboardSync) wsURL() string {
	url := strings.Replace(cs.client.baseURL, "http://", "ws://", 1)
	url = strings.Replace(url, "https://", "wss://", 1)
	return url + "/api/clipboard/ws"
}

// signedHeader builds the X-Auth-Signature/X-Auth-Timestamp headers for a
// request whose body is not included in the signature — used for the WS
// upgrade (no body) and the blob PUT/GET endpoints (bodies too large to sign
// without buffering them, matching the agent's verifyHMACNoBody).
func (cs *ClipboardSync) signedHeader(method, path string) http.Header {
	header := http.Header{}
	if len(cs.client.apiSecret) == 0 {
		return header
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := CalculateHMACV2(method, path, ts, "", cs.client.apiSecret)
	header.Set("X-Auth-Signature", sig)
	header.Set("X-Auth-Timestamp", ts)
	return header
}

func (cs *ClipboardSync) runOnce(ctx context.Context) error {
	header := cs.signedHeader("GET", "/api/clipboard/ws")
	logrus.Infof("[clipboard-sync] dialing %s", cs.wsURL())
	conn, resp, err := cs.dialer().DialContext(ctx, cs.wsURL(), header)
	if err != nil {
		status := "n/a"
		if resp != nil {
			status = resp.Status
		}
		logrus.Errorf("[clipboard-sync] dial failed (http status=%s): %v", status, err)
		return err
	}
	logrus.Infof("[clipboard-sync] connected")
	defer conn.Close()

	cs.mu.Lock()
	cs.conn = conn
	cs.mu.Unlock()
	defer func() {
		cs.mu.Lock()
		if cs.conn == conn {
			cs.conn = nil
		}
		cs.mu.Unlock()
	}()

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	var writeMu sync.Mutex
	safeWriteJSON := func(v interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}

	var closed atomic.Bool
	pushLocal := func(content clipboard.Content) {
		if closed.Load() {
			return
		}
		event, err := cs.buildOutgoingEvent(ctx, content)
		if err != nil {
			logrus.Errorf("[clipboard-sync] failed to prepare outgoing event: %v", err)
			return
		}
		if err := safeWriteJSON(event); err != nil {
			logrus.Errorf("[clipboard-sync] push failed: %v", err)
			return
		}
		logrus.Infof("[clipboard-sync] sent local %s change (size=%d)", event.Kind, event.Size)
	}
	cs.manager.SetOnLocalChange(pushLocal)

	pushPending := func(info clipboard.PendingInfo) {
		if closed.Load() {
			return
		}
		event := ClipboardEvent{Kind: string(info.Kind), Pending: true, FileCount: info.Count, Size: info.ApproxSize}
		if err := safeWriteJSON(event); err != nil {
			logrus.Errorf("[clipboard-sync] pending push failed: %v", err)
			return
		}
		logrus.Infof("[clipboard-sync] sent local pending %s change (count=%d, approx_size=%d)", event.Kind, info.Count, info.ApproxSize)
	}
	cs.manager.SetOnLocalChangePending(pushPending)

	defer func() {
		closed.Store(true)
		cs.manager.SetOnLocalChange(nil)
		cs.manager.SetOnLocalChangePending(nil)
	}()

	// Run's poll loop only fires on the *edge* of a detected clipboard
	// change, so a local change that happened (or that failed to send) while
	// this connection was down would otherwise never be retried. Resync once
	// up front on every fresh connection so the peer always converges to
	// whatever is currently on the clipboard, not just future changes.
	if content, ok := cs.manager.Snapshot(); ok {
		pushLocal(content)
	}

	for {
		var event ClipboardEvent
		if err := conn.ReadJSON(&event); err != nil {
			logrus.Errorf("[clipboard-sync] read failed, reconnecting: %v", err)
			return err
		}
		if event.Pending {
			// No BlobID yet — the peer is still reading/uploading. Nothing to
			// apply; this exists purely so a UI hook can show "receiving N
			// files..." instead of appearing to hang until the real event.
			logrus.Infof("[clipboard-sync] remote is preparing %s change (count=%d, approx_size=%d)", event.Kind, event.FileCount, event.Size)
			continue
		}
		logrus.Infof("[clipboard-sync] received remote %s change (size=%d)", event.Kind, event.Size)
		if err := cs.applyIncomingEvent(ctx, event); err != nil {
			logrus.Errorf("[clipboard-sync] apply failed kind=%s: %v", event.Kind, err)
		}
	}
}

func (cs *ClipboardSync) buildOutgoingEvent(ctx context.Context, content clipboard.Content) (ClipboardEvent, error) {
	event := ClipboardEvent{Kind: string(content.Kind), Hash: content.Hash()}
	switch content.Kind {
	case clipboard.KindText:
		event.Text = content.Text
		event.Size = int64(len(content.Text))
	case clipboard.KindImage:
		event.MimeType = "image/png"
		event.Size = int64(len(content.Image))
		id, err := cs.uploadBlob(ctx, content.Image)
		if err != nil {
			return ClipboardEvent{}, err
		}
		event.BlobID = id
	case clipboard.KindFile:
		data := clipboard.EncodeFiles(content.Files)
		event.Size = int64(len(data))
		if len(content.Files) > 0 {
			event.FileName = content.Files[0].Name
		}
		id, err := cs.uploadBlob(ctx, data)
		if err != nil {
			return ClipboardEvent{}, err
		}
		event.BlobID = id
	default:
		return ClipboardEvent{}, fmt.Errorf("clipboard-sync: unsupported kind %q", content.Kind)
	}
	return event, nil
}

func (cs *ClipboardSync) applyIncomingEvent(ctx context.Context, event ClipboardEvent) error {
	switch event.Kind {
	case string(clipboard.KindText):
		return cs.manager.Apply(clipboard.Content{Kind: clipboard.KindText, Text: event.Text})
	case string(clipboard.KindImage):
		data, err := cs.downloadBlob(ctx, event.BlobID)
		if err != nil {
			return err
		}
		return cs.manager.Apply(clipboard.Content{Kind: clipboard.KindImage, Image: data})
	case string(clipboard.KindFile):
		data, err := cs.downloadBlob(ctx, event.BlobID)
		if err != nil {
			return err
		}
		files, err := clipboard.DecodeFiles(data)
		if err != nil {
			return err
		}
		return cs.manager.Apply(clipboard.Content{Kind: clipboard.KindFile, Files: files})
	default:
		return nil
	}
}

func newClipboardBlobID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (cs *ClipboardSync) uploadBlob(ctx context.Context, data []byte) (string, error) {
	id := newClipboardBlobID()
	path := "/api/clipboard/blob/" + id
	header := cs.signedHeader("PUT", path)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, cs.client.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(data))

	resp, err := cs.client.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("clipboard-sync: blob upload failed, status %d", resp.StatusCode)
	}
	return id, nil
}

func (cs *ClipboardSync) downloadBlob(ctx context.Context, id string) ([]byte, error) {
	path := "/api/clipboard/blob/" + id
	header := cs.signedHeader("GET", path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cs.client.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}

	resp, err := cs.client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clipboard-sync: blob download failed, status %d", resp.StatusCode)
	}

	maxBytes := cs.maxBytesOrDefault()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("clipboard-sync: blob exceeds configured size limit")
	}
	return data, nil
}

func (cs *ClipboardSync) maxBytesOrDefault() int64 {
	if cs.maxBytes > 0 {
		return cs.maxBytes
	}
	return 200 * 1024 * 1024
}
