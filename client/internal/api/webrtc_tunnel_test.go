package api

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
)

type mockConn struct {
	net.Conn
	reader io.Reader
	writer io.Writer
	closed bool
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return m.reader.Read(b)
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return m.writer.Write(b)
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func TestWebRTCAPITransport(t *testing.T) {
	// Create a mock agent API response
	respStr := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 13\r\n\r\n{\"ok\": true}\n"
	
	reqBuf := new(bytes.Buffer)
	conn := &mockConn{
		reader: bytes.NewBufferString(respStr),
		writer: reqBuf,
	}

	transport := &webrtcAPITransport{
		openDataChannel: func(label string) (net.Conn, error) {
			if label != "api-tunnel" {
				return nil, fmt.Errorf("unexpected label")
			}
			return conn, nil
		},
		fallback: http.DefaultTransport,
	}

	client := &http.Client{Transport: transport}
	
	req, _ := http.NewRequest("GET", "http://agent:1234/api/status", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	
	if string(body) != "{\"ok\": true}\n" {
		t.Errorf("unexpected body: %q", string(body))
	}
	
	// Close body and ensure connection is closed
	resp.Body.Close()
	if !conn.closed {
		t.Errorf("expected connection to be closed after body Close")
	}

	// Ensure the request was actually written to our connection buffer
	if !bytes.Contains(reqBuf.Bytes(), []byte("GET /api/status HTTP/1.1")) {
		t.Errorf("request was not written to connection correctly, buffer: %s", reqBuf.String())
	}
}

func TestWebRTCAPITransport_Fallback(t *testing.T) {
	// Test fallback when openDataChannel returns error
	fallbackTransport := &testFallbackTransport{
		resp: &http.Response{StatusCode: 201},
	}
	transport := &webrtcAPITransport{
		openDataChannel: func(label string) (net.Conn, error) {
			return nil, fmt.Errorf("channel error")
		},
		fallback: fallbackTransport,
	}

	client := &http.Client{Transport: transport}
	req, _ := http.NewRequest("GET", "http://agent:1234/api/status", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.StatusCode != 201 {
		t.Errorf("expected fallback 201, got %d", resp.StatusCode)
	}
}

type testFallbackTransport struct {
	resp *http.Response
}

func (t *testFallbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.resp, nil
}
