package localui

import "fmt"

// Ported verbatim from usbridge/modules/ui_parser/marks.go -- assigns each
// icon/text region a "00".."FF" Set-of-Mark hex tag, icons first then text,
// in one continuous sequence, matching the device's tagging exactly so an
// agent sees identical IDs regardless of which backend answered ui.parse.
func assignMarkIDs(icons []Icon, text []TextRegion) {
	n := 0
	next := func() string {
		id := fmt.Sprintf("%02X", n)
		n++
		return id
	}
	for i := range icons {
		icons[i].ID = next()
	}
	for i := range text {
		text[i].ID = next()
	}
}
