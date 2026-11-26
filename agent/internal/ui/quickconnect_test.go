package ui

import (
	"net/url"
	"testing"
)

func TestBuildQuickConnectLinkIncludesHwID(t *testing.T) {
	link := buildQuickConnectLink("192.168.1.50", "", "master-secret", "direct", "hw-123")
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
	link := buildQuickConnectLink("192.168.1.50", "", "master-secret", "direct", "")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if _, present := u.Query()["hw_id"]; present {
		t.Errorf("expected no hw_id param in the link, got one: %s", link)
	}
}

func TestBuildQuickConnectLinkLANAndTailnetName(t *testing.T) {
	link := buildQuickConnectLink("192.168.1.50:8081", "pc.tail1234.ts.net:8081", "master-secret", "tailscale", "")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	q := u.Query()
	if got := q.Get("internal_host"); got != "192.168.1.50:8081" {
		t.Errorf("internal_host = %q, want the LAN address", got)
	}
	if got := q.Get("tailscale_host"); got != "pc.tail1234.ts.net:8081" {
		t.Errorf("tailscale_host = %q, want the tailnet name", got)
	}
	if _, present := q["device_host"]; present {
		t.Errorf("device_host still in the link: %s", link)
	}
}

func TestTailnetLinkHostPrefersName(t *testing.T) {
	if got := tailnetLinkHost("pc.tail1234.ts.net.", "100.64.0.1"); got != "pc.tail1234.ts.net" {
		t.Errorf("got %q, want the MagicDNS name", got)
	}
	if got := tailnetLinkHost("", "100.64.0.1"); got != "100.64.0.1" {
		t.Errorf("got %q, want the IP without a name", got)
	}
}
