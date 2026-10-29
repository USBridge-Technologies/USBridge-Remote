//go:build js && wasm

package platform

import "syscall/js"

// PageServedOverUSB: this is the web client the KVM itself serves over its
// USB cable (http://10.55.0.1:8080/, usbridge's setupnet.KVMAddr), where
// that KVM hands this page its key.
func PageServedOverUSB() bool {
	loc := js.Global().Get("location")
	if loc.IsUndefined() || loc.IsNull() {
		return false
	}
	return loc.Get("protocol").String() == "http:" && loc.Get("hostname").String() == "10.55.0.1"
}
