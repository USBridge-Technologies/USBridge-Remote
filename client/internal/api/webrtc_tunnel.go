package api

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

// webrtcAPITransport implements http.RoundTripper by tunneling HTTP/1.1 requests
// over a WebRTC DataChannel (label "api-tunnel"). This allows the browser web
// client to reach the agent's REST API without triggering mixed-content blocks
// when loaded over HTTPS.
type webrtcAPITransport struct {
	openDataChannel func(label string) (net.Conn, error)
	fallback        http.RoundTripper
}

func (t *webrtcAPITransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.openDataChannel == nil {
		return t.fallback.RoundTrip(req)
	}

	conn, err := t.openDataChannel("api-tunnel")
	if err != nil {
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
