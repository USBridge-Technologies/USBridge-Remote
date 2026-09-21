//go:build windows

package platform

import (
	"strings"
	"testing"
)

func TestEmbeddedDatabaseKnowsTheRaijuTE(t *testing.T) {
	m := sdlMappingFor(0x1532, 0x1007)
	if m == nil || m.name != "Razer Raiju TE" {
		t.Fatalf("Raiju TE not found in the embedded database: %+v", m)
	}
}

func TestEmbeddedDatabaseIsWellFormed(t *testing.T) {
	entries, usable := 0, 0
	for _, line := range strings.Split(sdlDB, "\n") {
		guid, m, ok := parseSDLMapping(strings.TrimRight(line, "\r"))
		if !ok {
			continue
		}
		entries++
		if !strings.HasPrefix(guid, "03000000") {
			t.Errorf("non-DirectInput entry slipped in: %s", guid)
		}
		if m.usable() {
			usable++
		}
		m.capture(joyInput{pov: -1}) // no source may crash the conversion
	}
	if entries < 500 || usable < 400 {
		t.Fatalf("database looks truncated: %d entries, %d usable", entries, usable)
	}
}

func TestWinmmUnitHandlesReversedRanges(t *testing.T) {
	// A reversed range (minimum above maximum): the ends are ordered, not the value flipped.
	if got := winmmUnit(0, 65535, 0); got != -1 {
		t.Errorf("low end: %v", got)
	}
	if got := winmmUnit(65535, 65535, 0); got != 1 {
		t.Errorf("high end: %v", got)
	}
	if got := winmmUnit(5, 7, 7); got != 0 {
		t.Errorf("empty range: %v", got)
	}
	if got := winmmScaleAxis(0, 65535, 0); got != -32767 {
		t.Errorf("legacy scale on a reversed range must not read as dead: %d", got)
	}
}

func TestFriendlyPadNameReplacesOnlyTheGenericDriverName(t *testing.T) {
	if got := friendlyPadName("Microsoft PC-joystick driver", "0x1532", "0x1007"); got != "Razer Raiju TE" {
		t.Errorf("known pad: %q", got)
	}
	if got := friendlyPadName("Microsoft PC-joystick driver", "0x1234", "0x5678"); got != "Microsoft PC-joystick driver" {
		t.Errorf("unknown pad keeps the generic name: %q", got)
	}
	if got := friendlyPadName("My Custom Pad", "0x1532", "0x1007"); got != "My Custom Pad" {
		t.Errorf("a specific name must be kept: %q", got)
	}
	if got := friendlyPadName("Microsoft PC-joystick driver", "", ""); got != "Microsoft PC-joystick driver" {
		t.Errorf("no ids: %q", got)
	}
}
