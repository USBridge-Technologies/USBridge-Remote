//go:build windows

package platform

import (
	"os"
	"strconv"
	"testing"
)

// Live dump of a pad's HID collections:
//
//	USBRIDGE_HID_DUMP=1532:1007 go test -v -run TestLiveHIDCollections ./internal/platform/
func TestLiveHIDCollections(t *testing.T) {
	spec := os.Getenv("USBRIDGE_HID_DUMP")
	if spec == "" {
		t.Skip("set USBRIDGE_HID_DUMP=vvvv:pppp (hex) to list a pad's HID collections")
	}
	var vid, pid uint64
	if _, err := fmtSscanHex(spec, &vid, &pid); err != nil {
		t.Fatal(err)
	}
	cols, err := hidCollections(uint16(vid), uint16(pid))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cols {
		t.Logf("usagePage=%#04x usage=%#04x in=%d out=%d feature=%d %s", c.UsagePage, c.Usage, c.InputLen, c.OutputLen, c.FeatureLen, c.Path)
	}
	if len(cols) == 0 {
		t.Log("no HID collections found (is the pad bound to the inbox HID driver, not WinUSB?)")
	}
}

func fmtSscanHex(s string, vid, pid *uint64) (int, error) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			v, err := strconv.ParseUint(s[:i], 16, 16)
			if err != nil {
				return 0, err
			}
			p, err := strconv.ParseUint(s[i+1:], 16, 16)
			if err != nil {
				return 0, err
			}
			*vid, *pid = v, p
			return 2, nil
		}
	}
	return 0, strconv.ErrSyntax
}
