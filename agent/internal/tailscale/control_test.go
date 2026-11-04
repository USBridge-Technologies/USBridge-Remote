package tailscale

import "testing"

func TestNormalizeControlURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                               "",
		"  ":                             "",
		"headscale.example.com":          "https://headscale.example.com",
		"https://hs.example.com/":        "https://hs.example.com",
		"http://10.0.0.5:8080":           "http://10.0.0.5:8080",
		"https://example.com/headscale/": "https://example.com/headscale",
	} {
		got, err := NormalizeControlURL(in)
		if err != nil || got != want {
			t.Errorf("NormalizeControlURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"ftp://x", "https://", "://"} {
		if _, err := NormalizeControlURL(bad); err == nil {
			t.Errorf("NormalizeControlURL(%q): want an error", bad)
		}
	}
}

func TestSetControlURL_SeparateStateDirs(t *testing.T) {
	s := &Service{}
	if !s.SetControlURL("https://hs.example.com") {
		t.Fatal("first switch should report a change")
	}
	if s.subdir == "" || s.ControlURL() != "https://hs.example.com" {
		t.Errorf("subdir=%q url=%q", s.subdir, s.ControlURL())
	}
	if s.SetControlURL("https://hs.example.com") {
		t.Error("same URL again should be no change")
	}
	s.SetControlURL("")
	if s.subdir != "" {
		t.Errorf("back to Tailscale should use the default state dir, got %q", s.subdir)
	}
}
