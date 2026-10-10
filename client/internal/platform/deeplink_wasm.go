//go:build js && wasm

package platform

import (
	"net/url"
	"strings"
	"sync"
	"syscall/js"
)

// GetIntentDataURI in the browser: a usbridge:// link after the page's #
// (https://<kvm>:9443/#usbridge://connect?host=...&master_key=...), as the
// KVM's USB setup page opens the web client the KVM serves. The fragment
// never reaches a server; it's read once and then cleared from the address
// bar (and history), so the key doesn't linger there.
var (
	hashOnce sync.Once
	hashURI  string
)

func GetIntentDataURI() (string, error) {
	hashOnce.Do(func() {
		loc := js.Global().Get("location")
		if loc.IsUndefined() || loc.IsNull() {
			return
		}
		h := strings.TrimPrefix(loc.Get("hash").String(), "#")
		if h == "" {
			return
		}
		if dec, err := url.QueryUnescape(h); err == nil && strings.HasPrefix(dec, "usbridge://") {
			h = dec
		}
		if !strings.HasPrefix(h, "usbridge://") {
			return
		}
		hashURI = h
		hist := js.Global().Get("history")
		if !hist.IsUndefined() && !hist.IsNull() {
			hist.Call("replaceState", js.Null(), "", loc.Get("pathname").String()+loc.Get("search").String())
		}
	})
	return hashURI, nil
}
