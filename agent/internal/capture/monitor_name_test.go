package capture

import (
	"testing"

	"usbridge_agent/internal/displaypower"
)

func TestMonitorName(t *testing.T) {
	for _, c := range []struct {
		m    displaypower.Monitor
		want string
	}{
		{displaypower.Monitor{Name: "DELL U2720Q", GDIName: `\\.\DISPLAY2`}, "DELL U2720Q"},
		{displaypower.Monitor{Name: `\\.\DISPLAY1`, GDIName: `\\.\DISPLAY1`}, "Display 1"},
		{displaypower.Monitor{GDIName: `\\.\DISPLAY12`}, "Display 12"},
		{displaypower.Monitor{Name: `\\.\DISPLAY3`}, `\\.\DISPLAY3`},
		{displaypower.Monitor{}, "Display"},
	} {
		if got := monitorName(c.m); got != c.want {
			t.Errorf("monitorName(%+v) = %q, want %q", c.m, got, c.want)
		}
	}
}
