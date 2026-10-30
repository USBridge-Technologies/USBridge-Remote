package app

import (
	"testing"

	"usbridge_agent/internal/displaypower"
)

func TestStrayVirtualMonitor(t *testing.T) {
	physical := displaypower.Monitor{ID: "phys", Active: true}
	virtual := displaypower.Monitor{ID: "vdd", Active: true, Virtual: true}
	for _, c := range []struct {
		name     string
		monitors []displaypower.Monitor
		want     bool
	}{
		{"virtual next to a physical one", []displaypower.Monitor{physical, virtual}, true},
		{"virtual alone (headless)", []displaypower.Monitor{virtual}, false},
		{"virtual detached", []displaypower.Monitor{physical, {ID: "vdd", Virtual: true}}, false},
		{"virtual next to a switched-off physical one", []displaypower.Monitor{{ID: "phys"}, virtual}, false},
		{"no virtual", []displaypower.Monitor{physical}, false},
	} {
		if got := strayVirtualMonitor(c.monitors); got != c.want {
			t.Errorf("%s: strayVirtualMonitor = %v, want %v", c.name, got, c.want)
		}
	}
}
