package webclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func serve(t *testing.T, h http.Handler, method, target, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, nil)
	if acceptEncoding != "" {
		r.Header.Set("Accept-Encoding", acceptEncoding)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHandler(t *testing.T) {
	cacheMu.Lock()
	cache = map[string]*file{}
	cacheMu.Unlock()
	h := handler(fstest.MapFS{
		"index.html":  {Data: []byte("<html>index")},
		"lite.html":   {Data: []byte("<html>lite")},
		"app.wasm.gz": {Data: []byte("gz")},
		"README.md":   {Data: []byte("readme")},
	})

	if w := serve(t, h, "GET", "/", ""); w.Code != 200 || w.Body.String() != "<html>index" {
		t.Fatalf("/: %d %q", w.Code, w.Body.String())
	}
	w := serve(t, h, "GET", "/lite", "")
	if w.Code != 200 || w.Body.String() != "<html>lite" || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("/lite: %d %q %q", w.Code, w.Body.String(), w.Header().Get("Content-Type"))
	}
	w = serve(t, h, "GET", "/app.wasm", "gzip, deflate, br")
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Content-Type") != "application/wasm" {
		t.Fatalf("/app.wasm: %d enc=%q type=%q", w.Code, w.Header().Get("Content-Encoding"), w.Header().Get("Content-Type"))
	}
	etag := w.Header().Get("ETag")
	r := httptest.NewRequest("GET", "/app.wasm", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	r.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotModified {
		t.Fatalf("revalidate: %d, want 304", w.Code)
	}
	if w := serve(t, h, "GET", "/app.wasm", ""); w.Code != http.StatusNotAcceptable {
		t.Fatalf("/app.wasm without gzip: %d", w.Code)
	}
	if w := serve(t, h, "GET", "/app", ""); w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("/app: %d %q", w.Code, w.Header().Get("Location"))
	}
	for _, p := range []string{"/api/unknown", "/README.md", "/nope.js", "/../../etc/passwd"} {
		if w := serve(t, h, "GET", p, ""); w.Code != http.StatusNotFound {
			t.Fatalf("%s: %d, want 404", p, w.Code)
		}
	}
	if w := serve(t, h, "POST", "/", ""); w.Code != http.StatusNotFound {
		t.Fatalf("POST /: %d", w.Code)
	}
}

func TestHandlerNotBuiltIn(t *testing.T) {
	cacheMu.Lock()
	cache = map[string]*file{}
	cacheMu.Unlock()
	h := handler(fstest.MapFS{"README.md": {Data: []byte("readme")}})
	w := serve(t, h, "GET", "/", "")
	if w.Code != http.StatusNotFound || len(w.Body.String()) < 20 {
		t.Fatalf("/: %d %q", w.Code, w.Body.String())
	}
}
