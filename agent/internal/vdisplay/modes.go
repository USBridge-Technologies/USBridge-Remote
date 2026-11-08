package vdisplay

import (
	"fmt"
	"regexp"
	"strings"
)

// The MttVDD mode list install-mttvdd.ps1 writes: every size in vddSizes at 60 Hz and
// at each of vddRates. At over 100 modes in all the monitor never appears, so the
// sizes are as few as they are because the rates are as many.
var (
	vddSizes = [][2]int{
		{1280, 720}, {1440, 900}, {1600, 900}, {1920, 1080}, {1920, 1200}, {2400, 1080}, {2532, 1170},
		{2560, 1080}, {2560, 1440}, {2560, 1600}, {2880, 1800}, {3440, 1440}, {3840, 2160}, {5120, 1440},
	}
	vddRates = []int{90, 120, 144, 240, 480, 1000}

	vddGlobalBlock      = regexp.MustCompile(`(?s)<global>.*?</global>`)
	vddResolutionsBlock = regexp.MustCompile(`(?s)<resolutions>.*?</resolutions>`)
)

// upgradedVDDSettings is an older install's vdd_settings.xml with the current mode list,
// the rest of the file as it was. False when it has the list already, or isn't one
// the installer wrote: a list edited by hand stays.
func upgradedVDDSettings(xml string) (string, bool) {
	if !strings.Contains(xml, "Written by rust-shine") || strings.Contains(xml, "<g_refresh_rate>1000</g_refresh_rate>") {
		return "", false
	}
	if !vddGlobalBlock.MatchString(xml) || !vddResolutionsBlock.MatchString(xml) {
		return "", false
	}
	var global, res strings.Builder
	global.WriteString("<global>\n")
	for _, r := range vddRates {
		fmt.Fprintf(&global, "        <g_refresh_rate>%d</g_refresh_rate>\n", r)
	}
	global.WriteString("    </global>")
	res.WriteString("<resolutions>\n")
	for _, s := range vddSizes {
		fmt.Fprintf(&res, "        <resolution><width>%d</width><height>%d</height><refresh_rate>60</refresh_rate></resolution>\n", s[0], s[1])
	}
	res.WriteString("    </resolutions>")
	xml = vddGlobalBlock.ReplaceAllLiteralString(xml, global.String())
	xml = vddResolutionsBlock.ReplaceAllLiteralString(xml, res.String())
	return xml, true
}
