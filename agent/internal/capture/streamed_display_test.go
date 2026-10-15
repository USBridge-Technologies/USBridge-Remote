package capture

import (
	"testing"

	"usbridge_agent/internal/streamhost"
)

func TestPickStreamedDisplay(t *testing.T) {
	gdi := []string{`\\.\DISPLAY1`, `\\.\DISPLAY2`, `\\.\DISPLAY3`}
	devs := []streamhost.CaptureDevice{
		{OutputName: "{aaa}", GDIName: `\\.\DISPLAY1`},
		{OutputName: "{bbb}", GDIName: `\\.\DISPLAY3`, Primary: true},
		{OutputName: "{ccc}", GDIName: `\\.\DISPLAY2`},
	}
	if got := pickStreamedDisplay(gdi, devs, "{ccc}"); got != 1 {
		t.Errorf("pinned output = %d, want 1", got)
	}
	if got := pickStreamedDisplay(gdi, devs, ""); got != 2 {
		t.Errorf("unpinned falls back to primary = %d, want 2", got)
	}
	if got := pickStreamedDisplay(gdi, devs, "{zzz}"); got != 2 {
		t.Errorf("unknown output falls back to primary = %d, want 2", got)
	}
	if got := pickStreamedDisplay(gdi, nil, "{ccc}"); got != 0 {
		t.Errorf("no devices = %d, want 0", got)
	}
	if got := pickStreamedDisplay(nil, devs, "{ccc}"); got != 0 {
		t.Errorf("no monitors = %d, want 0", got)
	}
}
