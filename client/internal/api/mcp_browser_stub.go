//go:build !(js && wasm)

package api

import "fmt"

// MCPBrowserBridge is wasm-only real functionality -- see
// mcp_browser_wasm.go's doc comment. Desktop/mobile already have a working
// local MCP listener (MCPProxy) an MCP client can dial directly, so there's
// nothing for this type to do there; it exists on every platform only so
// scripts_tab_widget.go (shared, no build tag) can hold and drive one
// unconditionally -- the same reason MCPProxy itself compiles (and simply
// never succeeds, net.Listen always failing under GOOS=js) on wasm too.
type MCPBrowserBridge struct{}

func (b *MCPBrowserBridge) Start(wsURL string, client *USBClient) error {
	return fmt.Errorf("MCP browser bridge is only available in the web build")
}

func (b *MCPBrowserBridge) UpdateClient(client *USBClient) {}
func (b *MCPBrowserBridge) Stop()                          {}
func (b *MCPBrowserBridge) Running() bool                  { return false }
