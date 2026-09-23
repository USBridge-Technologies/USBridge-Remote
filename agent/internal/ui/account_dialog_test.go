package ui

import (
	"net/url"
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestLicenseManagerURLIsBillingHost(t *testing.T) {
	u, err := url.Parse(licenseManagerURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "billing.usbridge.io" {
		t.Fatalf("license manager must open https://billing.usbridge.io/, got %q", licenseManagerURL)
	}
}

func TestWrapBreakBlockHeightSplitsLongHardwareID(t *testing.T) {
	test.NewApp()
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	oneLine := wrapBreakBlockHeight(id, 10, 2000)
	wrapped := wrapBreakBlockHeight(id, 10, 120)
	if wrapped <= oneLine {
		t.Fatalf("64-char hardware id at 120px must wrap: wrapped=%v oneLine=%v", wrapped, oneLine)
	}
}

func TestWrapLicenseIDLinesEvenHex(t *testing.T) {
	test.NewApp()
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	lines := wrapLicenseIDLines(id, 11, 400)
	if len(lines) != 2 || lines[0] != id[:32] || lines[1] != id[32:] {
		t.Fatalf("64-char id should wrap as two 32-char rows, got %#v", lines)
	}
}
