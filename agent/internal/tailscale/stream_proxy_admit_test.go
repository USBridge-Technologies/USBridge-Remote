package tailscale

import (
	"net"
	"testing"
)

func TestProxyAdmitsOnlyTheNodesItIsTold(t *testing.T) {
	p := &StreamProxy{}
	if !p.allowed(&net.TCPAddr{IP: net.ParseIP("100.64.0.9"), Port: 1}) {
		t.Fatal("a proxy without a check must let everyone through (the owner's node)")
	}
	calls := 0
	p.SetAdmit(func(r net.Addr) bool {
		calls++
		return r.(*net.TCPAddr).IP.String() == "100.64.0.2"
	})
	good := &net.TCPAddr{IP: net.ParseIP("100.64.0.2"), Port: 5000}
	bad := &net.TCPAddr{IP: net.ParseIP("100.64.0.3"), Port: 5000}
	if !p.allowed(good) || p.allowed(bad) {
		t.Fatal("wrong verdicts")
	}
	// Same IP, other port: answered from the cache.
	p.allowed(&net.TCPAddr{IP: good.IP, Port: 6000})
	p.allowed(&net.TCPAddr{IP: bad.IP, Port: 6000})
	if calls != 2 {
		t.Fatalf("the check ran %d times, want 2", calls)
	}
}
