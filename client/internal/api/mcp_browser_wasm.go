//go:build js && wasm

package api

// MCPBrowserBridge is wasm's counterpart to MCPProxy (mcp_proxy.go):
// desktop runs a local HTTP listener Claude Desktop's MCP client dials
// directly; a browser tab can never accept an inbound connection at all
// (see client/web/mcp-bridge/bridge.mjs's doc comment for the full
// topology and rationale -- Claude Desktop instead spawns that small Node
// process, which runs the listener on this app's behalf). This type DIALS
// OUT to bridge.mjs's local WebSocket server -- the one direction a
// browser sandbox permits -- and answers whatever JSON-RPC arrives over
// that connection the exact same way MCPProxy.handle answers an HTTP
// POST: same tryLocalUIParse interception (now backed by onnxruntime-web
// instead of onnxruntime_go, see internal/localui/parser_wasm.go), same
// signed-forward-to-device fallback via PostRawWithTimeout. No Origin-
// header CSRF check here (contrast MCPProxy.handle) -- there's no HTTP
// listener any page could accidentally/maliciously POST to; bridge.mjs's
// own --token requirement is what gates who can open this connection in
// the first place.
import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"

	"usbridge-client/internal/platform"
)

type MCPBrowserBridge struct {
	mu     sync.Mutex
	conn   net.Conn
	client *USBClient
	gen    int // bumped on Stop/reconnect so a still-unwinding read loop knows to exit quietly
}

// Start dials wsURL (ws://127.0.0.1:<port>/?token=<token>, see
// scripts_tab_wasm's own instructions for how that URL is built) and, once
// connected, answers every JSON-RPC message that arrives over it.
// Reconnecting after a Stop is just calling Start again. A no-op if
// already connected -- same "Start while running is a no-op" contract
// MCPProxy.Start documents.
func (b *MCPBrowserBridge) Start(wsURL string, client *USBClient) error {
	b.mu.Lock()
	if b.conn != nil {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	conn, err := platform.DialWebSocket(wsURL)
	if err != nil {
		return fmt.Errorf("mcp browser bridge: dial %s: %w", wsURL, err)
	}

	b.mu.Lock()
	b.conn = conn
	b.client = client
	b.gen++
	gen := b.gen
	b.mu.Unlock()

	go b.readLoop(conn, gen)
	log.Printf("✅ MCP browser bridge connected")
	return nil
}

// UpdateClient rewires an already-connected bridge to forward through
// client instead of whatever *USBClient it currently holds -- same reason
// MCPProxy.UpdateClient exists (see that doc comment): a reconnect/new
// paired device shouldn't leave in-flight forwarding signing requests with
// a stale key.
func (b *MCPBrowserBridge) UpdateClient(client *USBClient) {
	b.mu.Lock()
	b.client = client
	b.mu.Unlock()
}

// Stop closes the WebSocket connection to bridge.mjs.
func (b *MCPBrowserBridge) Stop() {
	b.mu.Lock()
	conn := b.conn
	b.conn = nil
	b.gen++
	b.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// Running reports whether the bridge is currently connected.
func (b *MCPBrowserBridge) Running() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conn != nil
}

// readLoop mirrors MCPProxy.handle's body, one WebSocket message at a time
// instead of one HTTP request at a time -- bridge.mjs never coalesces
// multiple JSON-RPC lines into a single frame (its own doc comment), and
// each of wsConn's Read calls returns exactly one browser-side WebSocket
// message's bytes (internal/platform/wsconn_wasm.go), so each iteration
// here is exactly one JSON-RPC message. Each message is handled on its own
// goroutine (see handle) so a slow device-forwarded call doesn't stall
// reading the next one off the wire.
func (b *MCPBrowserBridge) readLoop(conn net.Conn, gen int) {
	buf := make([]byte, 4<<20)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			b.mu.Lock()
			stale := gen != b.gen
			if !stale {
				b.conn = nil
			}
			b.mu.Unlock()
			if !stale {
				log.Printf("MCP browser bridge: disconnected: %v", err)
			}
			return
		}
		body := append([]byte(nil), buf[:n]...)
		go b.handle(conn, body)
	}
}

func (b *MCPBrowserBridge) handle(conn net.Conn, body []byte) {
	body = bytes.TrimSpace(body)
	id := jsonRPCID(body)

	if isJSONRPCNotification(body) {
		// See isJSONRPCNotification's doc comment (mcp_proxy.go): never
		// reply to a JSON-RPC notification, or a strict client reading
		// stdout sees an unsolicited response and breaks.
		return
	}

	b.mu.Lock()
	client := b.client
	b.mu.Unlock()
	if client == nil {
		b.reply(conn, jsonRPCErrorBytes(id, -32000, "no device connected"))
		return
	}

	// Local ui.parse offload -- identical interception to MCPProxy.handle,
	// see that call site's own doc comment.
	if localResp, handled, localErr := tryLocalUIParse(client, body); handled {
		if localErr != nil {
			b.reply(conn, jsonRPCErrorBytes(id, -32000, fmt.Sprintf("local ui.parse error: %v", localErr)))
			return
		}
		b.reply(conn, localResp)
		return
	}

	resp, err := client.PostRawWithTimeout("/api/mcp", body, mcpProxyTimeout)
	if err != nil {
		b.reply(conn, jsonRPCErrorBytes(id, -32000, fmt.Sprintf("device error: %v", err)))
		return
	}
	b.reply(conn, resp)
}

func (b *MCPBrowserBridge) reply(conn net.Conn, data []byte) {
	if _, err := conn.Write(data); err != nil {
		log.Printf("MCP browser bridge: write failed: %v", err)
	}
}

// jsonRPCErrorBytes mirrors writeJSONRPCError's JSON shape (mcp_proxy.go)
// without an http.ResponseWriter to write into -- this bridge answers over
// a raw net.Conn instead.
func jsonRPCErrorBytes(id any, code int, message string) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
	return b
}
