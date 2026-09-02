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
	// 43% used: 66 GB free, 119 GB total
	av := int64(66) * 1024 * 1024 * 1024
	tot := int64(119) * 1024 * 1024 * 1024
	s := FormatStorageCompact(av, tot, 43)
	if s != "43% 66/119 GB" {
		t.Errorf("FormatStorageCompact(66GB, 119GB, 43) = %q, want \"43%% 66/119 GB\"", s)
	}
	// MB for small volumes
	av2 := int64(50) * 1024 * 1024
	tot2 := int64(100) * 1024 * 1024
	s2 := FormatStorageCompact(av2, tot2, 50)
	if s2 != "50% 50/100 MB" {
		t.Errorf("FormatStorageCompact(50MB, 100MB, 50) = %q, want \"50%% 50/100 MB\"", s2)
	}
	// TB for large volumes
	av3 := int64(500) * 1024 * 1024 * 1024 * 1024
	tot3 := int64(1024) * 1024 * 1024 * 1024 * 1024
	s3 := FormatStorageCompact(av3, tot3, 51)
	if s3 != "51% 500/1024 TB" {
		t.Errorf("FormatStorageCompact(500TB, 1024TB, 51) = %q, want \"51%% 500/1024 TB\"", s3)
	}
}

func TestStorageDisplayInfo(t *testing.T) {
	tests := []struct {
		name     string
		status   StorageStatusData
		wantOK   bool
		wantUsed int64 // only checked when wantOK
	}{
		{
			name: "normal SD card inserted (eMMC-boot board)",
			status: StorageStatusData{
				SDCard:         StorageInfo{Mounted: true, Total: 32e9, Used: 8e9},
				EMMC:           StorageInfo{Mounted: true, Total: 16e9, Used: 4e9},
				BootDeviceIsSD: false,
			},
			wantOK:   true,
			wantUsed: 8e9, // must be the SDCard figures, not EMMC's
		},
		{
			name: "eMMC-boot board with no SD card inserted -- nothing qualifies as 'SD'",
			status: StorageStatusData{
				SDCard:         StorageInfo{Mounted: false},
				EMMC:           StorageInfo{Mounted: true, Total: 16e9, Used: 4e9},
				BootDeviceIsSD: false,
			},
			wantOK: false, // must NOT fall back to showing eMMC usage under the SD icon
		},
		{
			name: "booted from SD slot -- EMMC field is actually this card's own storage",
			status: StorageStatusData{
				SDCard:         StorageInfo{Mounted: false}, // no separate slot left
				EMMC:           StorageInfo{Mounted: true, Total: 5e9, Used: 2e9},
				BootDeviceIsSD: true,
			},
			wantOK:   true,
			wantUsed: 2e9, // the EMMC figures, attributed to "SD"
		},
		{
			name:   "no storage status at all",
			status: StorageStatusData{},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := tt.status.StorageDisplayInfo()
			if ok != tt.wantOK {
				t.Fatalf("StorageDisplayInfo() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && info.Used != tt.wantUsed {
				t.Errorf("StorageDisplayInfo() Used = %d, want %d", info.Used, tt.wantUsed)
			}
		})
	}
}
