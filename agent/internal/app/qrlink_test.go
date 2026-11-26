package app

import (
	"net/url"
	"testing"
)

func TestBuildQRLinkIncludesHwID(t *testing.T) {
	link := buildQRLink("192.168.1.50", "", "master-secret", "hw-123")
	if link == "" {
		t.Fatal("expected a non-empty link")
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	q := u.Query()
	if got := q.Get("hw_id"); got != "hw-123" {
		t.Errorf("hw_id = %q, want %q", got, "hw-123")
	}
	// master_key -- the real credential -- was already exposed here before
	// hw_id existed; confirm adding hw_id didn't disturb it.
	if got := q.Get("master_key"); got != "master-secret" {
		t.Errorf("master_key = %q, want %q", got, "master-secret")
	}
}

func TestBuildQRLinkOmitsHwIDWhenEmpty(t *testing.T) {
	link := buildQRLink("192.168.1.50", "", "master-secret", "")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if _, present := u.Query()["hw_id"]; present {
		t.Errorf("expected no hw_id param in the link, got one: %s", link)
	}
}
