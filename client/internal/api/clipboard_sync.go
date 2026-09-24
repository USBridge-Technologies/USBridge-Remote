package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

// clipboardWSConn is the minimal shape runOnce needs from its transport --
// satisfied directly by *websocket.Conn (desktop-native, and the wasm build
// whenever no WebRTC DataChannel is available) and by dcJSONConn (the wasm
// build's WebRTC-DataChannel path, see dial/dcJSONConn below). Mirrors the
// agent's own clipboardJSONConn (clipboard.go's runClipboardDuplex) for the
// exact same reason: hide two very different transports' framing from the
// shared duplex loop.
type clipboardWSConn interface {
	WriteJSON(v interface{}) error
	ReadJSON(v interface{}) error
	Close() error
}

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
// (or, on the wasm build with WebRTC available, its "clipboard-sync"
// DataChannel counterpart -- see SetOpenDataChannel) and keeps the local
// system clipboard in sync with it: local changes are pushed out, incoming
// changes are applied locally.
type ClipboardSync struct {
	client   *USBClient
	manager  *clipboard.Manager
	maxBytes int64

	// OpenDataChannel, when set, routes the wasm build's connection over the
	// already-established RustShine WebRTC PeerConnection instead of a
	// direct ws://+wss:// dial -- see dial's doc comment for why a direct
	// dial can never work from an https-loaded page at all. Wired from
	// gui.mainWindow the same optional-interface-probe pattern already used
	// for browser USB/gamepad/pen passthrough (see
	// internal/usbpass/usbaes_attach_wasm.go's identical field); nil on
	// every non-wasm platform and whenever the active backend has no
	// WebRTC (e.g. plain Sunshine).
	OpenDataChannel func(label string) (net.Conn, error)

	mu     sync.Mutex
	conn   clipboardWSConn
	cancel context.CancelFunc
	// push sends local content over the live connection and send writes a
	// raw event over it; both nil while disconnected. Set by runOnce, used
	// by PushNow/PullNow.
	push func(clipboard.Content) error
	send func(ClipboardEvent) error

	// autoSync applies incoming remote changes as they arrive (the local
	// side is gated by manager.SetEnabled). Off is manual mode: remote
	// changes are only remembered in lastRemote until PullNow.
	autoSync atomic.Bool

	pullMu     sync.Mutex
	lastRemote *ClipboardEvent // newest non-pending remote event
	pullWaiter chan struct{}   // non-nil while a PullNow waits for a reply
}

// clipboardRequestKind asks the agent to push its current clipboard back
// (runClipboardDuplex answers with its Snapshot). Older agents ignore
// unknown kinds, so PullNow falls back to lastRemote.
const clipboardRequestKind = "request"

// pullReplyTimeout bounds how long PullNow waits for the agent's reply
// before falling back to the last remote change it already received. A var
// so tests can shorten it.
var pullReplyTimeout = 2 * time.Second

var (
	// ErrClipboardNotConnected is returned by PushNow/PullNow with no live
	// connection to the agent.
	ErrClipboardNotConnected = errors.New("clipboard-sync: not connected")
	// ErrClipboardEmpty is returned by PushNow/PullNow when there is
	// nothing to transfer.
	ErrClipboardEmpty = errors.New("clipboard-sync: clipboard is empty")
)

// NewClipboardSync wraps manager with a connection to client's paired agent.
// Call Start to begin syncing and Stop to tear it down.
func NewClipboardSync(client *USBClient, manager *clipboard.Manager, maxBytes int64) *ClipboardSync {
	cs := &ClipboardSync{client: client, manager: manager, maxBytes: maxBytes}
	cs.autoSync.Store(true)
	return cs
}

// SetEnabled switches automatic two-way sync on or off without tearing down
// the connection. Off is manual mode: nothing moves in either direction
// until PushNow or PullNow.
func (cs *ClipboardSync) SetEnabled(enabled bool) {
	cs.autoSync.Store(enabled)
	if cs.manager != nil {
		cs.manager.SetEnabled(enabled)
	}
}

// PushNow sends whatever is on the local clipboard to the agent right away,
// regardless of automatic sync. Blocks for the upload of an image or files.
func (cs *ClipboardSync) PushNow() error {
	cs.mu.Lock()
	push := cs.push
	cs.mu.Unlock()
	if push == nil {
		return ErrClipboardNotConnected
	}
	content, ok := cs.manager.Snapshot()
	if !ok {
		return ErrClipboardEmpty
	}
	return push(content)
}

// PullNow replaces the local clipboard with the agent's current one,
// regardless of automatic sync. It asks the agent for a fresh copy and waits
// up to pullReplyTimeout; if the agent does not answer (older agents ignore
// the request), the newest change it already announced on this connection
// is applied instead.
func (cs *ClipboardSync) PullNow() error {
	cs.mu.Lock()
	send := cs.send
	cs.mu.Unlock()
	if send == nil {
		return ErrClipboardNotConnected
	}

	waiter := make(chan struct{})
	cs.pullMu.Lock()
	cs.pullWaiter = waiter
	cs.pullMu.Unlock()

	if err := send(ClipboardEvent{Kind: clipboardRequestKind}); err != nil {
		cs.pullMu.Lock()
		if cs.pullWaiter == waiter {
			cs.pullWaiter = nil
		}
		cs.pullMu.Unlock()
		return err
	}
	select {
	case <-waiter:
		return nil
	case <-time.After(pullReplyTimeout):
	}

	cs.pullMu.Lock()
	if cs.pullWaiter != waiter {
		// The reply raced the timeout; the read loop is applying it.
		cs.pullMu.Unlock()
		return nil
	}
	cs.pullWaiter = nil
	last := cs.lastRemote
	cs.pullMu.Unlock()
	if last == nil {
		return ErrClipboardEmpty
	}
	logrus.Infof("[clipboard-sync] no reply to pull request, applying last remote %s change", last.Kind)
	return cs.applyIncomingEvent(context.Background(), *last)
}

// takeIncoming records event as the newest remote clipboard and reports
// whether to apply it now: always in automatic mode, and in manual mode only
// as the answer to a pending PullNow. done releases that PullNow; call it
// once the event is applied.
func (cs *ClipboardSync) takeIncoming(event ClipboardEvent) (apply bool, done func()) {
	cs.pullMu.Lock()
	defer cs.pullMu.Unlock()
	ev := event
	cs.lastRemote = &ev
	if waiter := cs.pullWaiter; waiter != nil {
		cs.pullWaiter = nil
		return true, func() { close(waiter) }
	}
	return cs.autoSync.Load(), func() {}
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

// SetOpenDataChannel wires cs.OpenDataChannel after construction -- see that
// field's doc comment. Safe to call before Start.
func (cs *ClipboardSync) SetOpenDataChannel(fn func(label string) (net.Conn, error)) {
	cs.OpenDataChannel = fn
}

// clipboardDataChannelLabel is the WebRTC DataChannel label rustshine
// recognizes for this (see rust-shine's crates/webrtc-video/src/
// signaling.rs's on_data_channel and usbpass_bridge::attach_clipboard_channel).
const clipboardDataChannelLabel = "clipboard-sync"

// dial opens the transport runOnce will speak clipboardWSConn over.
// Prefers cs.OpenDataChannel when set (the wasm build with an active
// RustShine WebRTC PeerConnection): a direct ws://+wss:// dial from an
// https-loaded page (e.g. https://web.usbridge.io) either gets blocked
// outright as mixed content (ws://) or, for wss://, can only ever present
// the agent's self-signed cert for a bare-IP target (TLS SNI is never sent
// for an IP literal, so the agent's cert manager can't select its
// browser-trusted device-wildcard cert there) -- and a browser silently
// rejects an untrusted cert for a background WebSocket upgrade, with no
// click-through the way a top-level navigation warning has. The WebRTC
// DataChannel rides the already-DTLS-encrypted PeerConnection instead and
// is exempt from both problems entirely. Falls back to the direct dial
// otherwise (desktop-native always; wasm too, whenever the active backend
// has no WebRTC, e.g. plain Sunshine -- in which case this whole feature is
// simply unavailable from an https page, same limitation as before this
// existed).
func (cs *ClipboardSync) dial(ctx context.Context, header http.Header) (clipboardWSConn, *http.Response, error) {
	if cs.OpenDataChannel != nil {
		conn, err := cs.OpenDataChannel(clipboardDataChannelLabel)
		if err != nil {
			return nil, nil, fmt.Errorf("open clipboard-sync data channel: %w", err)
		}
		return newDCJSONConn(conn), nil, nil
	}
	return cs.dialer().DialContext(ctx, cs.wsURL(), header)
}

// dcJSONConn adapts a message-oriented net.Conn (webrtcweb.WebRTCClient.
// OpenDataChannel's return value -- one Write() call is one complete
// DataChannel message, one logical unit off Read() is likewise one
// complete received message, see dcconn_wasm.go's doc comment) to
// clipboardWSConn: one JSON value per message in both directions, exactly
// matching one WS message per WriteJSON/ReadJSON call on the direct-dial
// path this replaces.
type dcJSONConn struct {
	conn net.Conn
}

func newDCJSONConn(conn net.Conn) *dcJSONConn {
	return &dcJSONConn{conn: conn}
}

func (c *dcJSONConn) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = c.conn.Write(data)
	return err
}

func (c *dcJSONConn) ReadJSON(v interface{}) error {
	return json.NewDecoder(c.conn).Decode(v)
}

func (c *dcJSONConn) Close() error {
	return c.conn.Close()
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
	if cs.OpenDataChannel != nil {
		logrus.Infof("[clipboard-sync] dialing DataChannel %q", clipboardDataChannelLabel)
	} else {
		logrus.Infof("[clipboard-sync] dialing %s", cs.wsURL())
	}
	conn, resp, err := cs.dial(ctx, header)
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
	sendLocal := func(content clipboard.Content) error {
		if closed.Load() {
			return ErrClipboardNotConnected
		}
		event, err := cs.buildOutgoingEvent(ctx, content)
		if err != nil {
			logrus.Errorf("[clipboard-sync] failed to prepare outgoing event: %v", err)
			return err
		}
		if err := safeWriteJSON(event); err != nil {
			logrus.Errorf("[clipboard-sync] push failed: %v", err)
			return err
		}
		logrus.Infof("[clipboard-sync] sent local %s change (size=%d)", event.Kind, event.Size)
		return nil
	}
	pushLocal := func(content clipboard.Content) { _ = sendLocal(content) }
	cs.manager.SetOnLocalChange(pushLocal)

	cs.mu.Lock()
	cs.push = sendLocal
	cs.send = func(event ClipboardEvent) error {
		if closed.Load() {
			return ErrClipboardNotConnected
		}
		return safeWriteJSON(event)
	}
	cs.mu.Unlock()

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
		cs.mu.Lock()
		cs.push = nil
		cs.send = nil
		cs.mu.Unlock()
	}()

	// Run's poll loop only fires on the *edge* of a detected clipboard
	// change, so a local change that happened (or that failed to send) while
	// this connection was down would otherwise never be retried. Resync once
	// up front on every fresh connection so the peer always converges to
	// whatever is currently on the clipboard, not just future changes.
	// Manual mode sends nothing on its own.
	if cs.autoSync.Load() {
		if content, ok := cs.manager.Snapshot(); ok {
			pushLocal(content)
		}
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
		apply, done := cs.takeIncoming(event)
		if !apply {
			logrus.Infof("[clipboard-sync] received remote %s change (size=%d), kept for manual pull", event.Kind, event.Size)
			continue
		}
		logrus.Infof("[clipboard-sync] received remote %s change (size=%d)", event.Kind, event.Size)
		if err := cs.applyIncomingEvent(ctx, event); err != nil {
			logrus.Errorf("[clipboard-sync] apply failed kind=%s: %v", event.Kind, err)
		}
		done()
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
