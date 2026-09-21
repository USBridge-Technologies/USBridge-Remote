package usbpass

import (
	"bytes"
	"testing"
)

// hidDescriptorWalk checks a report descriptor is well formed: short items are
// sized correctly, collections balance, and it never runs off the end.
func hidDescriptorWalk(t *testing.T, d []byte) (collections int) {
	t.Helper()
	depth := 0
	for i := 0; i < len(d); {
		prefix := d[i]
		size := int(prefix & 3)
		if size == 3 {
			size = 4
		}
		if prefix == 0xFE { // long item, not produced by the reconstruction
			t.Fatalf("unexpected long item at %d", i)
		}
		if i+1+size > len(d) {
			t.Fatalf("item at %d overruns descriptor (%d bytes)", i, len(d))
		}
		switch prefix & 0xFC {
		case rdItemCollection:
			depth++
			collections++
		case rdItemEndColl:
			depth--
			if depth < 0 {
				t.Fatalf("unbalanced End Collection at %d", i)
			}
		}
		i += 1 + size
	}
	if depth != 0 {
		t.Fatalf("unbalanced descriptor: %d collections left open", depth)
	}
	return collections
}

// A three-button, two-axis relative mouse described the way Windows parses it.
func syntheticMousePP() *ppData {
	return &ppData{
		Usage: 0x02, UsagePage: 0x01,
		Info: [ppReportTypes]ppCapsInfo{{FirstCap: 0, NumberOfCaps: 3, LastCap: 3, ReportByteLength: 4}},
		Links: []ppLink{
			{LinkUsage: 0x02, LinkUsagePage: 0x01, CollectionType: 1},
		},
		Caps: []ppCap{
			{UsagePage: 0x09, BytePosition: 1, ReportSize: 1, ReportCount: 3, BitField: 0x02, IsButtonCap: true, IsRange: true,
				UsageMin: 1, UsageMax: 3, DataIndexMin: 0, DataIndexMax: 2},
			{UsagePage: 0x01, BytePosition: 2, ReportSize: 8, ReportCount: 1, BitField: 0x06,
				UsageMin: 0x30, DataIndexMin: 3, DataIndexMax: 3, LogicalMin: -127, LogicalMax: 127},
			{UsagePage: 0x01, BytePosition: 3, ReportSize: 8, ReportCount: 1, BitField: 0x06,
				UsageMin: 0x31, DataIndexMin: 4, DataIndexMax: 4, LogicalMin: -127, LogicalMax: 127},
		},
	}
}

func TestReconstructSyntheticMouse(t *testing.T) {
	got, err := reconstructReportDescriptor(syntheticMousePP())
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x05, 0x01, 0x09, 0x02, 0xA1, 0x01, // Usage Page (Generic Desktop), Usage (Mouse), Collection (Application)
		0x05, 0x09, 0x19, 0x01, 0x29, 0x03, // Usage Page (Button), Usage Min 1, Usage Max 3
		0x15, 0x00, 0x25, 0x01, 0x75, 0x01, 0x95, 0x03, 0x81, 0x02, // 3 x 1-bit variable inputs
		0x75, 0x05, 0x95, 0x01, 0x81, 0x03, // 5 bits of constant padding
		0x05, 0x01, 0x09, 0x30, 0x09, 0x31, // Generic Desktop: X, Y (folded into one item)
		0x15, 0x81, 0x25, 0x7F, 0x75, 0x08, 0x95, 0x02, 0x81, 0x06, // 2 x 8-bit relative
		0xC0,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("descriptor mismatch\n got: % X\nwant: % X", got, want)
	}
	if n := hidDescriptorWalk(t, got); n != 1 {
		t.Fatalf("collections = %d, want 1", n)
	}
}

func TestReconstructRejectsAliases(t *testing.T) {
	pp := syntheticMousePP()
	pp.Caps[0].IsAlias = true
	if _, err := reconstructReportDescriptor(pp); err == nil {
		t.Fatal("expected an error for aliased usages")
	}
	pp = syntheticMousePP()
	pp.Links[0].IsAlias = true
	if _, err := reconstructReportDescriptor(pp); err == nil {
		t.Fatal("expected an error for aliased collections")
	}
}

func TestReconstructMalformedDoesNotPanic(t *testing.T) {
	pp := syntheticMousePP()
	pp.Caps[1].LinkCollection = 9 // out of range
	if _, err := reconstructReportDescriptor(pp); err == nil {
		t.Fatal("expected an error for a bad link collection index")
	}
	pp = syntheticMousePP()
	pp.Info[0].LastCap = 40 // beyond the caps array
	if _, err := reconstructReportDescriptor(pp); err == nil {
		t.Fatal("expected an error for caps index out of range")
	}
}

func TestParsePreparsedRoundTrip(t *testing.T) {
	// Build the raw blob for the synthetic mouse and parse it back.
	blob := make([]byte, ppHeaderSize+3*ppCapSize+ppLinkSize)
	copy(blob, "HidP KDR")
	blob[8], blob[10] = 0x02, 0x01
	blob[16+0] = 0                                                        // FirstCap
	blob[16+2], blob[16+4], blob[16+6] = 3, 3, 4                          // NumberOfCaps, LastCap, ReportByteLength
	blob[40], blob[41] = byte((3*ppCapSize)&0xFF), byte((3*ppCapSize)>>8) // FirstByteOfLinkCollectionArray
	blob[42] = 1                                                          // NumberLinkCollectionNodes
	put := func(cap int, off int, v ...byte) { copy(blob[ppHeaderSize+cap*ppCapSize+off:], v) }
	put(0, 0, 0x09, 0x00)     // UsagePage
	put(0, 8, 1, 0)           // BytePosition
	put(0, 4, 1, 0)           // ReportSize
	put(0, 6, 3, 0)           // ReportCount
	put(0, 12, 0x02, 0, 0, 0) // BitField
	put(0, 24, 1<<2|1<<4)     // IsButtonCap | IsRange
	put(0, 60, 1, 0, 3, 0)    // UsageMin/Max
	put(0, 72, 0, 0, 2, 0)    // DataIndexMin/Max
	for i := 1; i <= 2; i++ {
		put(i, 0, 0x01, 0x00)
		put(i, 8, byte(1+i), 0)
		put(i, 4, 8, 0)
		put(i, 6, 1, 0)
		put(i, 12, 0x06, 0, 0, 0)
		put(i, 60, byte(0x2F+i), 0)
		put(i, 72, byte(2+i), 0, byte(2+i), 0)
		put(i, 80, 0x81, 0xFF, 0xFF, 0xFF, 0x7F, 0, 0, 0) // LogicalMin -127, LogicalMax 127
	}
	lo := ppHeaderSize + 3*ppCapSize
	blob[lo], blob[lo+2], blob[lo+12] = 0x02, 0x01, 1 // link: usage 2, page 1, type Application

	if n, err := preparsedBlobSize(blob); err != nil || n != len(blob) {
		t.Fatalf("preparsedBlobSize = %d, %v; want %d", n, err, len(blob))
	}
	pp, err := parsePreparsed(blob)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reconstructReportDescriptor(pp)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := reconstructReportDescriptor(syntheticMousePP())
	if !bytes.Equal(got, want) {
		t.Fatalf("parsed blob reconstructs differently\n got: % X\nwant: % X", got, want)
	}
	if ids := pp.reportIDs(); !ids[0][0] {
		t.Fatalf("input report ID 0 not reported: %v", ids)
	}
}

func TestParsePreparsedRejectsGarbage(t *testing.T) {
	if _, err := parsePreparsed(make([]byte, 10)); err == nil {
		t.Fatal("short blob accepted")
	}
	bad := make([]byte, ppHeaderSize)
	copy(bad, "NotHidP!")
	if _, err := parsePreparsed(bad); err == nil {
		t.Fatal("wrong magic accepted")
	}
}
