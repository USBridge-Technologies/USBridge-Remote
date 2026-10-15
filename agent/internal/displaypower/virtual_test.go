package displaypower

import (
	"path/filepath"
	"testing"
)

func TestParseMode(t *testing.T) {
	m, err := ParseMode("2560x1440@120")
	if err != nil || m != (Mode{2560, 1440, 120}) {
		t.Fatalf("got %v, %v", m, err)
	}
	for _, bad := range []string{"", "1920x1080", "1920@60", "axb@c", "0x0@60"} {
		if _, err := ParseMode(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestStateFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vd.json")
	if LoadState(path) != nil {
		t.Fatal("state without a file")
	}
	st := &VirtualState{GDIName: `\\.\DISPLAY3`, Mode: Mode{1920, 1080, 60}, Layout: []Placed{{Name: `\\.\DISPLAY1`}}}
	if err := saveState(path, st); err != nil {
		t.Fatal(err)
	}
	got := LoadState(path)
	if got == nil || got.GDIName != st.GDIName || got.Mode != st.Mode || len(got.Layout) != 1 {
		t.Fatalf("got %+v", got)
	}
	if err := saveState(path, nil); err != nil || LoadState(path) != nil {
		t.Fatalf("not removed: %v", err)
	}
}

func TestParseHelperOutput(t *testing.T) {
	out := []byte("2026/10/15 log line\r\n" + HelperResultPrefix + `{"state":{"gdi_name":"\\\\.\\DISPLAY9","friendly_name":"VDD by MTT","device_path":"","mode":{"width":1,"height":2,"hz":3}}}` + "\r\n")
	res, err := ParseHelperOutput(out)
	if err != nil || res.State == nil || res.State.GDIName != `\\.\DISPLAY9` || res.State.Mode.Hz != 3 {
		t.Fatalf("got %+v, %v", res, err)
	}
	if _, err := ParseHelperOutput([]byte("crash")); err == nil {
		t.Fatal("no error without a result line")
	}
}
