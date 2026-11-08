package vdisplay

import (
	"strings"
	"testing"
)

const oldVDDSettings = `<?xml version='1.0' encoding='utf-8'?>
<!-- Written by rust-shine scripts/install-mttvdd.ps1. The driver only reads this when the device starts. -->
<vdd_settings>
    <global>
        <g_refresh_rate>90</g_refresh_rate>
        <g_refresh_rate>144</g_refresh_rate>
    </global>
    <resolutions>
        <resolution><width>1280</width><height>720</height><refresh_rate>60</refresh_rate></resolution>
    </resolutions>
    <logging><logging>false</logging></logging>
</vdd_settings>
`

func TestUpgradeAddsTheRatesAndKeepsTheRest(t *testing.T) {
	got, ok := upgradedVDDSettings(oldVDDSettings)
	if !ok {
		t.Fatal("not upgraded")
	}
	for _, want := range []string{"<g_refresh_rate>240<", "<g_refresh_rate>480<", "<g_refresh_rate>1000<", "<width>3840</width>", "<logging><logging>false</logging></logging>", "Written by rust-shine"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if _, again := upgradedVDDSettings(got); again {
		t.Error("upgraded twice")
	}
}

func TestUpgradeLeavesAHandEditedList(t *testing.T) {
	if _, ok := upgradedVDDSettings(strings.Replace(oldVDDSettings, "Written by rust-shine", "mine", 1)); ok {
		t.Error("upgraded a list the installer didn't write")
	}
}

// Over 100 modes and the monitor never appears (see install-mttvdd.ps1).
func TestModeListStaysUnderTheDriversLimit(t *testing.T) {
	if n := len(vddSizes) * (1 + len(vddRates)); n > 100 {
		t.Fatalf("%d modes", n)
	}
}
