//go:build js

package controller

// usbSetupSupported in the browser too: it works in the web client the KVM
// itself serves over its USB cable (http://10.55.0.1:8080/), whose origin
// the KVM's setup server allows. From an https page (web.usbridge.io, or
// https://<kvm>:9443/) the browser blocks the plain-http request: the
// button then says where to open the client instead.
const usbSetupSupported = true
