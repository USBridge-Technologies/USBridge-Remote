package models

import (
	"testing"
)

func TestParseSizeHumanPair(t *testing.T) {
	tests := []struct {
		input    string
		wantUsed int64
		wantTot  int64
	}{
		{"0 B / 40.0 GB", 0, 40 * 1024 * 1024 * 1024},
		{"5.2 GB / 32 GB", 5*1024*1024*1024 + 214748364, 32 * 1024 * 1024 * 1024},
		{"invalid", -1, -1},
		{"10 MB / 100 MB", 10 * 1024 * 1024, 100 * 1024 * 1024},
	}
	for _, tt := range tests {
		used, tot := parseSizeHumanPair(tt.input)
		if tt.wantUsed >= 0 && (used != tt.wantUsed || tot != tt.wantTot) {
			t.Errorf("parseSizeHumanPair(%q) = (%d, %d), want (%d, %d)",
				tt.input, used, tot, tt.wantUsed, tt.wantTot)
		}
	}
}

func TestLocalDrive_StorageDisplay(t *testing.T) {
	ld := &LocalDrive{
		Size:      32 * 1024 * 1024 * 1024,
		FreeSpace: 12 * 1024 * 1024 * 1024,
	}
	total, free, pct := ld.StorageDisplay()
	if total != ld.Size || free != ld.FreeSpace {
		t.Errorf("StorageDisplay: got total=%d free=%d", total, free)
	}
	if pct < 62 || pct > 63 {
		t.Errorf("StorageDisplay: got pct=%.1f, want ~62.5", pct)
	}

	// SizeHuman fallback
	ld2 := &LocalDrive{
		Size:      32 * 1024 * 1024 * 1024,
		SizeHuman: "8.0 GB / 32 GB",
	}
	total2, free2, pct2 := ld2.StorageDisplay()
	if total2 != 32*1024*1024*1024 || free2 != 24*1024*1024*1024 {
		t.Errorf("StorageDisplay SizeHuman: got total=%d free=%d", total2, free2)
	}
	if pct2 < 24 || pct2 > 26 {
		t.Errorf("StorageDisplay SizeHuman: got pct=%.1f, want ~25", pct2)
	}
}

func TestFormatSizeShort(t *testing.T) {
	if s := FormatSizeShort(1024); s != "1.0 KB" {
		t.Errorf("FormatSizeShort(1024) = %q", s)
	}
	if s := FormatSizeShort(32 * 1024 * 1024 * 1024); s != "32.0 GB" {
		t.Errorf("FormatSizeShort(32GB) = %q", s)
	}
}

func TestFormatStorageCompact(t *testing.T) {
	// 43% занято: 66 GB свободно, 119 GB всего
	av := int64(66) * 1024 * 1024 * 1024
	tot := int64(119) * 1024 * 1024 * 1024
	s := FormatStorageCompact(av, tot, 43)
	if s != "43% 66/119 GB" {
		t.Errorf("FormatStorageCompact(66GB, 119GB, 43) = %q, want \"43%% 66/119 GB\"", s)
	}
	// MB для маленьких объёмов
	av2 := int64(50) * 1024 * 1024
	tot2 := int64(100) * 1024 * 1024
	s2 := FormatStorageCompact(av2, tot2, 50)
	if s2 != "50% 50/100 MB" {
		t.Errorf("FormatStorageCompact(50MB, 100MB, 50) = %q, want \"50%% 50/100 MB\"", s2)
	}
	// TB для больших объёмов
	av3 := int64(500) * 1024 * 1024 * 1024 * 1024
	tot3 := int64(1024) * 1024 * 1024 * 1024 * 1024
	s3 := FormatStorageCompact(av3, tot3, 51)
	if s3 != "51% 500/1024 TB" {
		t.Errorf("FormatStorageCompact(500TB, 1024TB, 51) = %q, want \"51%% 500/1024 TB\"", s3)
	}
}
