package ui

import (
	"net/url"
	"testing"
)

func TestBuildQuickConnectLinkIncludesHwID(t *testing.T) {
	link := buildQuickConnectLink("192.168.1.50", "", "", "master-secret", "direct", "hw-123")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	q := u.Query()
	if got := q.Get("hw_id"); got != "hw-123" {
		t.Errorf("hw_id = %q, want %q", got, "hw-123")
	}
	if got := q.Get("protocol"); got != "direct" {
		t.Errorf("protocol = %q, want %q", got, "direct")
	}
}

func TestBuildQuickConnectLinkOmitsHwIDWhenEmpty(t *testing.T) {
	link := buildQuickConnectLink("192.168.1.50", "", "", "master-secret", "direct", "")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if _, present := u.Query()["hw_id"]; present {
		t.Errorf("expected no hw_id param in the link, got one: %s", link)
	}
}

func TestBuildQuickConnectLinkCarriesDeviceHost(t *testing.T) {
	link := buildQuickConnectLink("192.168.1.50", "100.64.0.1", "pc.tail1234.ts.net", "master-secret", "tailscale", "")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := u.Query().Get("device_host"); got != "pc.tail1234.ts.net" {
		t.Errorf("device_host = %q, want pc.tail1234.ts.net", got)
	}
}
