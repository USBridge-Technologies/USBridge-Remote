package api

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// apiTunnelDownUntil (unix nanos): after an "api-tunnel" channel failed to
// open, requests go straight to the fallback until then instead of each
// paying a failed channel open first (a NanoKVM without an agent bridge
// refuses every one). Package-level since one-off transports share it.
var apiTunnelDownUntil atomic.Int64

const apiTunnelRetryAfter = 30 * time.Second

// webrtcAPITransport implements http.RoundTripper by tunneling HTTP/1.1 requests
// over a WebRTC DataChannel (label "api-tunnel") when one can be opened,
// falling back to the client's original transport otherwise -- see
// USBClient.SetOpenDataChannel's doc comment for why that fallback is
// always kept (on every platform, wasm included): by the time this
// transport is ever installed, the connect flow has already proven the
// fallback transport works. fallback is only ever nil in a test that
// constructs this struct directly.
type webrtcAPITransport struct {
	openDataChannel func(label string) (net.Conn, error)
	fallback        http.RoundTripper
}

func (t *webrtcAPITransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.openDataChannel == nil {
		if t.fallback == nil {
			return nil, fmt.Errorf("webrtc api transport: no data channel opener configured")
		}
		return t.fallback.RoundTrip(req)
	}

	if t.fallback != nil && time.Now().UnixNano() < apiTunnelDownUntil.Load() {
		return t.fallback.RoundTrip(req)
	}
	conn, err := t.openDataChannel("api-tunnel")
	if err != nil {
		apiTunnelDownUntil.Store(time.Now().Add(apiTunnelRetryAfter).UnixNano())
		if t.fallback == nil {
			return nil, fmt.Errorf("webrtc api transport: open data channel %q: %w", "api-tunnel", err)
		}
		return t.fallback.RoundTrip(req)
	}

	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		conn.Close()
		return nil, err
	}

	// We wrap the response body to ensure the underlying DataChannel is closed
	// when the caller finishes reading the body.
	resp.Body = &connCloser{
		ReadCloser: resp.Body,
		conn:       conn,
	}

	return resp, nil
}

type connCloser struct {
	io.ReadCloser
	conn net.Conn
}

func (c *connCloser) Close() error {
	bodyErr := c.ReadCloser.Close()
	connErr := c.conn.Close()
	if bodyErr != nil {
		return bodyErr
	}
	return connErr
}
