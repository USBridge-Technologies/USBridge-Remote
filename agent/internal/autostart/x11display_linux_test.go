//go:build linux

package autostart

import (
	"errors"
	"testing"
)

const sampleXrandrDark = `Screen 0: minimum 8 x 8, current 1920 x 1080, maximum 32767 x 32767
HDMI-0 disconnected (normal left inverted right x axis y axis)
HDMI-1 connected primary (normal left inverted right x axis y axis)
   1024x768      60.00 +
   1600x900      59.82
   1920x1080_60.00  59.96
USB-C-0 disconnected (normal left inverted right x axis y axis)
`

const sampleXrandrActive = `Screen 0: minimum 8 x 8, current 1600 x 900, maximum 32767 x 32767
HDMI-1 connected primary 1600x900+0+0 (normal left inverted right x axis y axis) 0mm x 0mm
   1024x768      60.00 +
   1600x900      59.82*
USB-C-0 disconnected (normal left inverted right x axis y axis)
`

func TestParseXrandrOutputs_DarkOutputHasNoActiveMode(t *testing.T) {
	outputs := parseXrandrOutputs([]byte(sampleXrandrDark))

	var hdmi *xrandrOutput
	for i := range outputs {
		if outputs[i].name == "HDMI-1" {
			hdmi = &outputs[i]
		}
	}
	if hdmi == nil {
		t.Fatal("HDMI-1 not found")
	}
	if !hdmi.connected {
		t.Error("HDMI-1 should be connected")
	}
	if hdmi.hasActiveMode {
		t.Error("HDMI-1 has no '*' marker in the sample output, hasActiveMode should be false")
	}
	if len(hdmi.modes) != 3 {
		t.Fatalf("expected 3 modes, got %d: %+v", len(hdmi.modes), hdmi.modes)
	}
}

func TestParseXrandrOutputs_ActiveOutputDetected(t *testing.T) {
	outputs := parseXrandrOutputs([]byte(sampleXrandrActive))
	var hdmi *xrandrOutput
	for i := range outputs {
		if outputs[i].name == "HDMI-1" {
			hdmi = &outputs[i]
		}
	}
	if hdmi == nil {
		t.Fatal("HDMI-1 not found")
	}
	if !hdmi.hasActiveMode {
		t.Error("HDMI-1 has a '*' marker on 1600x900, hasActiveMode should be true")
	}
}

func TestParseXrandrOutputs_DisconnectedOutputHasNoModes(t *testing.T) {
	outputs := parseXrandrOutputs([]byte(sampleXrandrDark))
	for _, o := range outputs {
		if o.name == "USB-C-0" {
			if o.connected {
				t.Error("USB-C-0 should be disconnected")
			}
			if len(o.modes) != 0 {
				t.Errorf("disconnected output should have no modes, got %+v", o.modes)
			}
			return
		}
	}
	t.Fatal("USB-C-0 not found")
}

func TestParseXrandrResolution(t *testing.T) {
	cases := []struct {
		in     string
		w, h   int
		wantOk bool
	}{
		{"1600x900", 1600, 900, true},
		{"1920x1080_60.00", 1920, 1080, true},
		{"garbage", 0, 0, false},
		{"1600xNaN", 0, 0, false},
	}
	for _, c := range cases {
		w, h, ok := parseXrandrResolution(c.in)
		if ok != c.wantOk || w != c.w || h != c.h {
			t.Errorf("parseXrandrResolution(%q) = (%d, %d, %v), want (%d, %d, %v)", c.in, w, h, ok, c.w, c.h, c.wantOk)
		}
	}
}

// TestFixDarkOutput_PrefersRealModeOverCustomEvenWhenSmaller reproduces the
// exact live scenario: a manually-added "1920x1080_60.00" mode is larger on
// paper than the real "1600x900" mode, but the driver has started rejecting
// it (RRAddOutputMode BadMatch) -- fixDarkOutput must try the real mode
// first regardless, succeeding without ever attempting the custom one.
func TestFixDarkOutput_PrefersRealModeOverCustomEvenWhenSmaller(t *testing.T) {
	o := xrandrOutput{
		name: "HDMI-1",
		modes: []xrandrMode{
			{name: "1024x768", area: 1024 * 768},
			{name: "1600x900", area: 1600 * 900},
			{name: "1920x1080_60.00", area: 1920 * 1080},
		},
	}
	var attempted []string
	run := func(args ...string) error {
		mode := args[len(args)-1]
		attempted = append(attempted, mode)
		return nil // every mode "succeeds" here -- the point is which one gets tried first
	}
	fixDarkOutput(o, run)
	if len(attempted) != 1 {
		t.Fatalf("expected exactly 1 attempt (the first real mode tried should succeed immediately), got %v", attempted)
	}
	if attempted[0] != "1600x900" {
		t.Errorf("expected 1600x900 (largest real/non-custom mode) to be tried first despite being smaller on paper than the custom mode, got %q", attempted[0])
	}
}

// TestFixDarkOutput_FallsThroughPastARejectedMode confirms the fallback
// chain itself (not just the sort order) skips a rejected mode and keeps
// going to the next one instead of stopping at the first failure -- here the
// larger of two *real* modes is rejected, isolating that behavior from the
// separate real-vs-custom priority already covered above.
func TestFixDarkOutput_FallsThroughPastARejectedMode(t *testing.T) {
	o := xrandrOutput{
		name: "HDMI-1",
		modes: []xrandrMode{
			{name: "1600x900", area: 1600 * 900},
			{name: "1024x768", area: 1024 * 768},
		},
	}
	var attempted []string
	run := func(args ...string) error {
		mode := args[len(args)-1]
		attempted = append(attempted, mode)
		if mode == "1600x900" {
			return errors.New("rejected")
		}
		return nil
	}
	fixDarkOutput(o, run)
	if len(attempted) != 2 {
		t.Fatalf("expected 2 attempts (1600x900 rejected, then falling through to 1024x768), got %v", attempted)
	}
	if attempted[0] != "1600x900" || attempted[1] != "1024x768" {
		t.Errorf("expected [1600x900 1024x768], got %v", attempted)
	}
}

func TestFixDarkOutput_FallsBackToAutoWhenEveryModeFails(t *testing.T) {
	o := xrandrOutput{
		name:  "HDMI-1",
		modes: []xrandrMode{{name: "1024x768", area: 1024 * 768}},
	}
	var attempted []string
	run := func(args ...string) error {
		attempted = append(attempted, args[len(args)-1])
		return errors.New("nope")
	}
	fixDarkOutput(o, run)
	if len(attempted) != 2 {
		t.Fatalf("expected the mode attempt plus a final --auto fallback, got %v", attempted)
	}
	if attempted[1] != "--auto" {
		t.Errorf("expected the last attempt to be --auto, got %q", attempted[1])
	}
}

func TestEnsureDisplayActive_SkipsOutputsThatAlreadyHaveAnActiveMode(t *testing.T) {
	ranAnything := false
	run := func(args ...string) error {
		ranAnything = true
		return nil
	}
	query := func() ([]byte, error) { return []byte(sampleXrandrActive), nil }
	ensureDisplayActive(query, run)
	if ranAnything {
		t.Error("no output needed fixing, ensureDisplayActive should not have run any xrandr mutation")
	}
}

func TestEnsureDisplayActive_FixesTheDarkOutput(t *testing.T) {
	var fixedOutput string
	run := func(args ...string) error {
		for i, a := range args {
			if a == "--output" && i+1 < len(args) {
				fixedOutput = args[i+1]
			}
		}
		return nil
	}
	query := func() ([]byte, error) { return []byte(sampleXrandrDark), nil }
	ensureDisplayActive(query, run)
	if fixedOutput != "HDMI-1" {
		t.Errorf("expected HDMI-1 (the dark output) to be fixed, got %q", fixedOutput)
	}
}

func TestEnsureDisplayActive_NoopsOnQueryError(t *testing.T) {
	ranAnything := false
	run := func(args ...string) error {
		ranAnything = true
		return nil
	}
	query := func() ([]byte, error) { return nil, errors.New("no X server") }
	ensureDisplayActive(query, run)
	if ranAnything {
		t.Error("a failed --query should not attempt any fix")
	}
}
