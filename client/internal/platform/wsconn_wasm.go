//go:build js && wasm

package platform

// DialWebSocket wraps a browser WebSocket as a net.Conn.
//
// Why this exists: the wasm build has no working net.Dial (Go's net package
// compiles under GOOS=js but every Dial always fails at runtime -- see
// client/internal/api/usb_client_direct_wasm.go's doc comment, which hit
// this exact wall for a different feature). client/internal/usbpass's AES
// Hello/Attach control-plane code (usbaes_attach.go, usbaes_transport.go's
// aeadStream) is written directly against net.Conn and is otherwise
// perfectly portable pure Go -- rather than forking that logic for wasm,
// this gives the wasm build a net.Conn it CAN produce, backed by an
// ordinary browser WebSocket talking to the agent's own relay endpoint
// (agent/internal/api/usb_passthrough_browser.go), so usbaes_attach.go's
// wasm variant can reuse it unmodified.
import (
	"errors"
	"io"
	"net"
	"sync"
	"syscall/js"
	"time"
)

type wsAddr string

func (a wsAddr) Network() string { return "websocket" }
func (a wsAddr) String() string  { return string(a) }

type wsConn struct {
	ws  js.Value
	url string

	recvCh chan []byte

	closeOnce sync.Once
	done      chan struct{}
	doneErr   error

	readBuf []byte

	onOpen, onMsg, onErr, onClose js.Func
}

// wsDialTimeout bounds how long DialWebSocket waits for the WebSocket's
// open/error event before giving up -- matching usbaes_attach.go's own
// 5-second net.Dialer.Timeout in spirit (kept a little longer here since a
// WebSocket upgrade is one extra HTTP round-trip on top of the TCP handshake
// a raw dial would need).
const wsDialTimeout = 10 * time.Second

// DialWebSocket opens url and blocks until the connection is open (or it
// fails), returning it as a net.Conn.
func DialWebSocket(url string) (net.Conn, error) {
	c := &wsConn{
		url:    url,
		recvCh: make(chan []byte, 64),
		done:   make(chan struct{}),
	}

	openCh := make(chan struct{}, 1)
	c.ws = js.Global().Get("WebSocket").New(url)
	c.ws.Set("binaryType", "arraybuffer")

	c.onOpen = js.FuncOf(func(this js.Value, args []js.Value) any {
		select {
		case openCh <- struct{}{}:
		default:
		}
		return nil
	})
	c.onMsg = js.FuncOf(func(this js.Value, args []js.Value) any {
		data := args[0].Get("data")
		buf := make([]byte, data.Get("byteLength").Int())
		js.CopyBytesToGo(buf, js.Global().Get("Uint8Array").New(data))
		select {
		case c.recvCh <- buf:
		case <-c.done:
		default:
			// Consumer isn't keeping up and the channel's 64-message buffer
			// is full -- drop rather than block the JS event loop forever,
			// same "no infinite backpressure" tradeoff a real TCP socket's
			// bounded receive buffer would eventually force anyway.
		}
		return nil
	})
	c.onErr = js.FuncOf(func(this js.Value, args []js.Value) any {
		c.fail(errors.New("websocket: connection error"))
		select {
		case openCh <- struct{}{}: // unblock a Dial still waiting
		default:
		}
		return nil
	})
	c.onClose = js.FuncOf(func(this js.Value, args []js.Value) any {
		c.fail(io.EOF)
		select {
		case openCh <- struct{}{}:
		default:
		}
		return nil
	})

	c.ws.Set("onopen", c.onOpen)
	c.ws.Set("onmessage", c.onMsg)
	c.ws.Set("onerror", c.onErr)
	c.ws.Set("onclose", c.onClose)

	select {
	case <-openCh:
		select {
		case <-c.done:
			c.release()
			return nil, c.doneErr
		default:
			return c, nil
		}
	case <-time.After(wsDialTimeout):
		c.fail(errors.New("websocket: connect timeout"))
		c.release()
		return nil, errors.New("websocket: connect timeout to " + url)
	}
}

func (c *wsConn) fail(err error) {
	c.closeOnce.Do(func() {
		c.doneErr = err
		close(c.done)
	})
}

func (c *wsConn) release() {
	c.onOpen.Release()
	c.onMsg.Release()
	c.onErr.Release()
	c.onClose.Release()
}

func (c *wsConn) Read(p []byte) (int, error) {
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

func (c *wsConn) Write(p []byte) (int, error) {
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
	c.ws.Call("send", arr)
	return len(p), nil
}

func (c *wsConn) Close() error {
	c.fail(net.ErrClosed)
	c.ws.Call("close")
	c.release()
	return nil
}

func (c *wsConn) LocalAddr() net.Addr  { return wsAddr("wasm-local") }
func (c *wsConn) RemoteAddr() net.Addr { return wsAddr(c.url) }

// Deadlines are best-effort no-ops: nothing in usbaes_attach.go's Hello/
// Attach flow sets one directly on the conn (the 5s connect timeout is
// wsDialTimeout's job above, and the AEAD stream's own framing has no
// per-frame deadline of its own either) -- this only exists to satisfy
// net.Conn's interface.
func (c *wsConn) SetDeadline(time.Time) error      { return nil }
func (c *wsConn) SetReadDeadline(time.Time) error  { return nil }
func (c *wsConn) SetWriteDeadline(time.Time) error { return nil }
