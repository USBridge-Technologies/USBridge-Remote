//go:build js && wasm

package webrtcweb

// OpenDataChannel + dcConn: lets other packages (client/internal/usbpass'
// browser-sourced USB/IP passthrough) multiplex additional labeled
// RTCDataChannels onto the SAME already-connected RTCPeerConnection this
// client uses for video/control, instead of opening a separate ws://
// WebSocket straight to the agent's HTTP server. Structurally a copy of
// client/internal/platform/wsconn_wasm.go's wsConn -- same "buffered
// recvCh + leftover readBuf reassembly" shape, wrapping RTCDataChannel's
// send()/"message"/"close"/"error" events instead of WebSocket's -- so
// usbaes_attach_wasm.go's AEAD Hello/Attach code (written against net.Conn)
// runs unmodified over either transport.
//
// Why this matters: a plain ws:// connection from an https-loaded page is
// blocked by the browser's mixed-content policy, and this agent has no TLS
// support anywhere to offer a wss:// alternative -- but WebRTC's
// DataChannel traffic is DTLS-encrypted end to end and isn't subject to
// that mixed-content check at all, so riding the already-established
// PeerConnection sidesteps the problem entirely rather than working around
// it.

import (
	"errors"
	"io"
	"net"
	"sync"
	"syscall/js"
	"time"
)

type dcAddr string

func (a dcAddr) Network() string { return "webrtc-datachannel" }
func (a dcAddr) String() string  { return string(a) }

type dcConn struct {
	dc    js.Value
	label string

	recvCh chan []byte

	closeOnce sync.Once
	done      chan struct{}
	doneErr   error

	readBuf []byte

	onOpen, onMsg, onErr, onClose js.Func
}

// dcOpenTimeout bounds how long OpenDataChannel waits for the channel's
// open/error event -- mirrors wsconn_wasm.go's wsDialTimeout in spirit,
// though opening an additional DataChannel on an already-established SCTP
// association should be near-instant (no new ICE/DTLS handshake, just a
// DCEP control message) compared to a fresh WebSocket's full TCP+HTTP
// upgrade round-trip.
const dcOpenTimeout = 10 * time.Second

// OpenDataChannel creates a new DataChannel labeled label on this client's
// already-connected RTCPeerConnection and blocks until it opens (or fails),
// returning it as a net.Conn. Safe to call any time after Connect has
// succeeded: creating a DataChannel on an established SCTP association
// needs no SDP renegotiation (DCEP negotiates new stream ids in-band) --
// this is the same assumption the "input" channel created inside Connect
// already relies on implicitly (it's just created before the first offer
// instead of after).
func (c *WebRTCClient) OpenDataChannel(label string) (net.Conn, error) {
	if c.pc == nil {
		return nil, errors.New("webrtc: peer connection not established")
	}
	pc := *c.pc
	dc := pc.Call("createDataChannel", label)
	dc.Set("binaryType", "arraybuffer")

	conn := &dcConn{
		dc:     dc,
		label:  label,
		recvCh: make(chan []byte, 64),
		done:   make(chan struct{}),
	}

	openCh := make(chan struct{}, 1)
	signalOpen := func() {
		select {
		case openCh <- struct{}{}:
		default:
		}
	}

	conn.onOpen = js.FuncOf(func(this js.Value, args []js.Value) any {
		signalOpen()
		return nil
	})
	conn.onMsg = js.FuncOf(func(this js.Value, args []js.Value) any {
		event := args[0]
		data := event.Get("data")
		var buf []byte
		if data.Type() == js.TypeString {
			buf = []byte(data.String())
		} else {
			buf = make([]byte, data.Get("byteLength").Int())
			js.CopyBytesToGo(buf, js.Global().Get("Uint8Array").New(data))
		}
		select {
		case conn.recvCh <- buf:
		case <-conn.done:
		default:
			// Consumer isn't keeping up and the channel's 64-message buffer
			// is full -- drop rather than block the JS event loop forever,
			// same tradeoff wsConn's onMsg makes.
		}
		return nil
	})
	conn.onErr = js.FuncOf(func(this js.Value, args []js.Value) any {
		conn.fail(errors.New("datachannel: error on " + label))
		signalOpen() // unblock a pending OpenDataChannel call
		return nil
	})
	conn.onClose = js.FuncOf(func(this js.Value, args []js.Value) any {
		conn.fail(io.EOF)
		signalOpen()
		return nil
	})

	dc.Call("addEventListener", "open", conn.onOpen)
	dc.Call("addEventListener", "message", conn.onMsg)
	dc.Call("addEventListener", "error", conn.onErr)
	dc.Call("addEventListener", "close", conn.onClose)

	// createDataChannel can hand back a channel already in "open" state in
	// some engines/timings (no "open" event will fire again for it) --
	// check readyState directly rather than relying solely on the listener.
	if dc.Get("readyState").String() == "open" {
		signalOpen()
	}

	select {
	case <-openCh:
		select {
		case <-conn.done:
			conn.release()
			return nil, conn.doneErr
		default:
			return conn, nil
		}
	case <-time.After(dcOpenTimeout):
		conn.fail(errors.New("datachannel: open timeout"))
		conn.release()
		return nil, errors.New("datachannel: open timeout for label " + label)
	}
}

func (c *dcConn) fail(err error) {
	c.closeOnce.Do(func() {
		c.doneErr = err
		close(c.done)
	})
}

func (c *dcConn) release() {
	c.onOpen.Release()
	c.onMsg.Release()
	c.onErr.Release()
	c.onClose.Release()
}

func (c *dcConn) Read(p []byte) (int, error) {
	if len(c.readBuf) == 0 {
		select {
		case b := <-c.recvCh:
			c.readBuf = b
		default:
			select {
			case b := <-c.recvCh:
				c.readBuf = b
			case <-c.done:
				if c.doneErr != nil {
					return 0, c.doneErr
				}
				return 0, io.EOF
			}
		}
	}
	n := copy(p, c.readBuf)
	c.readBuf = c.readBuf[n:]
	return n, nil
}

func (c *dcConn) Write(p []byte) (int, error) {
	select {
	case <-c.done:
		if c.doneErr != nil {
			return 0, c.doneErr
		}
		return 0, net.ErrClosed
	default:
	}
	arr := js.Global().Get("Uint8Array").New(len(p))
	js.CopyBytesToJS(arr, p)
	c.dc.Call("send", arr)
	return len(p), nil
}

func (c *dcConn) Close() error {
	c.fail(net.ErrClosed)
	c.dc.Call("close")
	c.release()
	return nil
}

func (c *dcConn) LocalAddr() net.Addr  { return dcAddr("wasm-local") }
func (c *dcConn) RemoteAddr() net.Addr { return dcAddr(c.label) }

// Deadlines are best-effort no-ops, same as wsConn's -- nothing in
// usbaes_attach_wasm.go's Hello/Attach flow sets one directly on the conn.
func (c *dcConn) SetDeadline(time.Time) error      { return nil }
func (c *dcConn) SetReadDeadline(time.Time) error  { return nil }
func (c *dcConn) SetWriteDeadline(time.Time) error { return nil }
