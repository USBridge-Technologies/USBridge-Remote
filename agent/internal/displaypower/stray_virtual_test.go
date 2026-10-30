package displaypower

import "testing"

func TestStrayVirtual(t *testing.T) {
	physical := Monitor{ID: "phys", Active: true}
	virtual := Monitor{ID: "vdd", Active: true, Virtual: true}
	for _, c := range []struct {
		name     string
		monitors []Monitor
		want     bool
	}{
		{"virtual next to a physical one", []Monitor{physical, virtual}, true},
		{"virtual alone (headless)", []Monitor{virtual}, false},
		{"virtual detached", []Monitor{physical, {ID: "vdd", Virtual: true}}, false},
		{"virtual next to a switched-off physical one", []Monitor{{ID: "phys"}, virtual}, false},
		{"no virtual", []Monitor{physical}, false},
	} {
		if got := StrayVirtual(c.monitors); got != c.want {
			t.Errorf("%s: StrayVirtual = %v, want %v", c.name, got, c.want)
		}
	}
}
