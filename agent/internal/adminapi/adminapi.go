// Package adminapi lets a GUI process attach to an already-running headless
// agent instance instead of spinning up a second copy of the whole engine
// (HTTP server, Sunshine, tsnet). It's a small JSON-over-unix-socket RPC
// layer, local to the machine only — never exposed on any network interface
// — covering exactly the operations the GUI needs (internal/ui.Window's
// token/perms/ts calls) plus a stream of Tailscale AuthURL events.
package adminapi

import "path/filepath"

// SocketPath returns the path of the admin unix socket for a given agent
// state directory. Both the headless engine and any GUI attaching to it
// must agree on this path, which is why it lives here rather than being
// passed around ad hoc.
func SocketPath(stateDir string) string {
	return filepath.Join(stateDir, "admin.sock")
}

// errorBody is the JSON shape written on any handler failure.
type errorBody struct {
	Error string `json:"error"`
}

type boolBody struct {
	Value bool `json:"value"`
}

type stringBody struct {
	Value string `json:"value"`
}

type captureModeBody struct {
	Mode string `json:"mode"`
}

type unpairBody struct {
	UniqueID string `json:"unique_id"`
}

type pinBody struct {
	PIN string `json:"pin"`
}

type listenAddrBody struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type sunshinePortBody struct {
	Port int `json:"port"`
}

type sunshineStreamAddrBody struct {
	Host       string `json:"host"`
	StreamPort int    `json:"stream_port"`
}

type loginBody struct {
	URL string `json:"url"`
}

type adminCredentialsBody struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}
