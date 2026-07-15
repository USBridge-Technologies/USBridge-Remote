package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

const DefaultMCPProxyPort = 8765

// MCPProxy runs a local HTTP server on 127.0.0.1 that forwards /api/mcp
// requests to the device with HMAC signatures, letting local AI tools reach
// MCP without needing to sign requests themselves.
type MCPProxy struct {
	mu     sync.Mutex
	srv    *http.Server
	client *USBClient
	port   int
}

// Start launches the proxy on 127.0.0.1:port. Calling Start while already
// running is a no-op.
func (p *MCPProxy) Start(port int, client *USBClient) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv != nil {
		return nil
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("mcp proxy: listen %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp", p.handle)
	// Also accept /mcp for convenience (some AI clients use shorter path)
	mux.HandleFunc("/mcp", p.handle)

	p.client = client
	p.port = port
	p.srv = &http.Server{
		Handler:           mux,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := p.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("mcp proxy: %v", err)
		}
	}()
	log.Printf("✅ MCP proxy started on %s", addr)
	return nil
}

// Stop shuts down the proxy.
func (p *MCPProxy) Stop() {
	p.mu.Lock()
	srv := p.srv
	p.srv = nil
	p.mu.Unlock()
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Println("🛑 MCP proxy stopped")
}

// Running reports whether the proxy is active.
func (p *MCPProxy) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.srv != nil
}

// Port returns the port the proxy is listening on (0 if stopped).
func (p *MCPProxy) Port() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv == nil {
		return 0
	}
	return p.port
}

func (p *MCPProxy) handle(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	if client == nil {
		http.Error(w, "no device connected", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	// PostRaw signs the request with HMAC and forwards to device /api/mcp.
	resp, err := client.PostRaw("/api/mcp", bytes.TrimSpace(body))
	if err != nil {
		http.Error(w, fmt.Sprintf("device error: %v", err), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(resp)
}
