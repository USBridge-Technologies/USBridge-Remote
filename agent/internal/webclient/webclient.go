// Package webclient serves the browser client (USBridge-Remote's web build:
// index.html, lite.html, app.wasm, wasm_exec.js) from the agent itself, the
// way the hardware KVM does (usbridge/web/webclient.go): http://<pc>:8080/
// and https://<pc>:8443/ open the client with nothing to install, and its
// API calls, /webrtc/offer and /webtransport/info go to the very origin the
// page came from -- no CORS, no second certificate to accept, and the lite
// client (which only talks to its own origin) works at all.
//
// The files are embedded at build time: scripts/stage_webclient.sh builds
// client/web and copies it into static/ before `go build` (the release
// build_*.sh scripts call it). A build without it still compiles -- static/
// then holds only its README -- and answers "/" with a note saying so.
// Static files only, no secrets: the page asks for the master key like any
// client (or gets it after the # of a usbridge://connect link, which the
// browser never sends to a server).
package webclient

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed all:static
var embedded embed.FS

var types = map[string]string{
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".mjs":  "text/javascript; charset=utf-8",
	".wasm": "application/wasm",
	".json": "application/json",
	".css":  "text/css; charset=utf-8",
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".ico":  "image/x-icon",
}

func contentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if t, ok := types[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// file is one embedded file, read once; etag is its content hash, so a
// browser revalidates a cached app.wasm cheaply and still picks up the next
// release's (embed gives every file the same zero modification time).
type file struct {
	data []byte
	etag string
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*file{}
)

func load(fsys fs.FS, name string) *file {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if f, ok := cache[name]; ok {
		return f
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		cache[name] = nil
		return nil
	}
	sum := sha256.Sum256(data)
	f := &file{data: data, etag: `"` + hex.EncodeToString(sum[:12]) + `"`}
	cache[name] = f
	return f
}

// Available reports whether this build carries the web client.
func Available() bool {
	return load(files(), "index.html") != nil || load(files(), "lite.html") != nil
}

func files() fs.FS {
	sub, err := fs.Sub(embedded, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

// Handler serves the embedded web client. Only GET/HEAD; /api/ paths and
// anything not embedded are a 404, as before this handler existed.
func Handler() http.Handler {
	return handler(files())
}

func handler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		p := path.Clean("/" + r.URL.Path)
		if p == "/api" || strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/webrtc/") {
			http.NotFound(w, r)
			return
		}
		switch p {
		case "/app":
			// The fragment (#usbridge://connect?...) survives the redirect.
			http.Redirect(w, r, "/", http.StatusFound)
			return
		case "/lite":
			p = "/lite.html"
		case "/":
			p = "/index.html"
		}
		name := strings.TrimPrefix(p, "/")
		if name == "README.md" {
			http.NotFound(w, r)
			return
		}

		f, encoding := (*file)(nil), ""
		accept := r.Header.Get("Accept-Encoding")
		if strings.Contains(accept, "br") {
			if f = load(fsys, name+".br"); f != nil {
				encoding = "br"
			}
		}
		if f == nil && strings.Contains(accept, "gzip") {
			if f = load(fsys, name+".gz"); f != nil {
				encoding = "gzip"
			}
		}
		if f == nil {
			f = load(fsys, name)
		}
		if f == nil {
			if load(fsys, name+".gz") != nil || load(fsys, name+".br") != nil {
				// Only the compressed copy is embedded.
				http.Error(w, "gzip required", http.StatusNotAcceptable)
				return
			}
			if name == "index.html" || name == "lite.html" {
				http.Error(w, "The web client isn't built into this agent. Open https://web.usbridge.io instead.", http.StatusNotFound)
				return
			}
			http.NotFound(w, r)
			return
		}

		h := w.Header()
		h.Set("Content-Type", contentType(name))
		if encoding != "" {
			h.Set("Content-Encoding", encoding)
		}
		h.Add("Vary", "Accept-Encoding")
		h.Set("ETag", f.etag)
		// Revalidate every time: the ETag answers 304 for an unchanged file,
		// and an agent update's new app.wasm is picked up on the next load.
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.data))
	})
}
