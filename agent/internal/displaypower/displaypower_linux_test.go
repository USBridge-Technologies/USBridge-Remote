//go:build linux

package displaypower

import "testing"

func vkmsOutput(modes ...[3]float64) kscreenOutput {
	o := kscreenOutput{ID: 7, Name: "Virtual-1"}
	for i, m := range modes {
		o.Modes = append(o.Modes, struct {
			ID          string  `json:"id"`
			RefreshRate float64 `json:"refreshRate"`
			Size        struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"size"`
		}{ID: string(rune('1' + i)), RefreshRate: m[2]})
		o.Modes[i].Size.Width, o.Modes[i].Size.Height = int(m[0]), int(m[1])
	}
	return o
}

func TestHasModeTakesReducedBlankingRates(t *testing.T) {
	o := vkmsOutput([3]float64{1920, 1080, 60}, [3]float64{1920, 1080, 239.79}, [3]float64{1920, 1080, 999.5})
	for _, hz := range []int{60, 240, 1000} {
		if !hasMode(o, Mode{Width: 1920, Height: 1080, Hz: hz}) {
			t.Errorf("%d Hz should be there", hz)
		}
	}
	for _, m := range []Mode{{1920, 1080, 480}, {1920, 1080, 120}, {2560, 1440, 240}} {
		if hasMode(o, m) {
			t.Errorf("%v should be missing", m)
		}
	}
}

func TestCustomModeArgUsesTheIDAndMilliHertz(t *testing.T) {
	if got := customModeArg(7, Mode{Width: 1920, Height: 1080, Hz: 480}); got != "output.7.addCustomMode.1920.1080.480000.reduced" {
		t.Fatal(got)
	}
}

func TestClosestModePicksTheCustomRate(t *testing.T) {
	o := vkmsOutput([3]float64{1920, 1080, 60}, [3]float64{1920, 1080, 479.64})
	id, got := closestModeID(o, Mode{Width: 1920, Height: 1080, Hz: 480})
	if id != "2" || got.Hz != 480 {
		t.Fatalf("%s %v", id, got)
	}
}
