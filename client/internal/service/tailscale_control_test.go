package service

import "testing"

func TestNormalizeControlURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                        "",
		"hs.example.com":          "https://hs.example.com",
		"https://hs.example.com/": "https://hs.example.com",
		"http://10.0.0.5:8080":    "http://10.0.0.5:8080",
	} {
		if got, err := NormalizeControlURL(in); err != nil || got != want {
			t.Errorf("NormalizeControlURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeControlURL("ftp://x"); err == nil {
		t.Error("ftp:// should be refused")
	}
	if ownControlStateSubdir("") != "tailscale" || ownControlStateSubdir("https://a") == "tailscale" {
		t.Error("own control server must get its own state dir")
	}
}
