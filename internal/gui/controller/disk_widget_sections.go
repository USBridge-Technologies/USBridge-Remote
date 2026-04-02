package controller

import (
	"image/color"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
)

type deviceSection struct {
	key         string
	eyebrow     string
	title       string
	description string
	count       int
	indexes     []int
}

func (dw *DiskWidget) rebuildListItems() {
	if dw.devicesList != nil {
		dw.requestDevicesRefresh()
	}
}

func (dw *DiskWidget) groupDriveIndexes() []deviceSection {
	storage := make([]int, 0)
	backup := make([]int, 0)
	control := make([]int, 0)
	connectivity := make([]int, 0)

	for idx, drive := range dw.allDrives {
		switch {
		case drive.Source == "api" && drive.LocalDrive != nil && drive.LocalDrive.SourceType == "mtp":
			backup = append(backup, idx)
		case drive.IsRNDIS:
			connectivity = append(connectivity, idx)
		case drive.IsVideo || drive.IsKeyboard || drive.IsMouse:
			control = append(control, idx)
		default:
			storage = append(storage, idx)
		}
	}

	return []deviceSection{
		{
			key:         "storage",
			eyebrow:     i18n.Current.DevicesSectionStorageEyebrow,
			title:       i18n.Current.DevicesSectionStorage,
			description: i18n.Current.DevicesSectionStorageHint,
			count:       len(storage),
			indexes:     storage,
		},
		{
			key:         "backup",
			eyebrow:     i18n.Current.DevicesSectionBackupEyebrow,
			title:       i18n.Current.DevicesSectionBackup,
			description: i18n.Current.DevicesSectionBackupHint,
			count:       len(backup),
			indexes:     backup,
		},
		{
			key:         "control",
			eyebrow:     i18n.Current.DevicesSectionControlEyebrow,
			title:       i18n.Current.DevicesSectionControl,
			description: i18n.Current.DevicesSectionControlHint,
			count:       len(control),
			indexes:     control,
		},
		{
			key:         "connectivity",
			eyebrow:     i18n.Current.DevicesSectionConnectivityEyebrow,
			title:       i18n.Current.DevicesSectionConnectivity,
			description: i18n.Current.DevicesSectionConnectivityHint,
			count:       len(connectivity),
			indexes:     connectivity,
		},
	}
}

func formatSectionCount(count int) string { return "" }

func sectionPalette(key string) (fill color.Color, border color.Color, badge color.Color) {
	_ = key
	return color.NRGBA{R: 0x2c, G: 0x2c, B: 0x2c, A: 0xff}, design.ColorBorder, color.NRGBA{R: 0x2c, G: 0x2c, B: 0x2c, A: 0xff}
}

func driveBadge(drive DriveItem) (string, color.Color) {
	switch {
	case drive.IsVideo:
		return "VID", color.NRGBA{R: 0x9b, G: 0x47, B: 0x2d, A: 0xff}
	case drive.IsKeyboard:
		return "KBD", color.NRGBA{R: 0x4e, G: 0x77, B: 0x2f, A: 0xff}
	case drive.IsMouse:
		return "PTR", color.NRGBA{R: 0x3d, G: 0x72, B: 0x56, A: 0xff}
	case drive.IsRNDIS:
		return "NET", color.NRGBA{R: 0x2b, G: 0x5d, B: 0x8c, A: 0xff}
	case drive.Source == "api" && drive.LocalDrive != nil && drive.LocalDrive.SourceType == "mtp":
		return "MTP", color.NRGBA{R: 0x6e, G: 0x58, B: 0x2c, A: 0xff}
	case drive.Source == "api":
		return "ISO", color.NRGBA{R: 0x7a, G: 0x59, B: 0x20, A: 0xff}
	case drive.Source == "local":
		return "LOC", color.NRGBA{R: 0x2c, G: 0x5f, B: 0x7d, A: 0xff}
	case drive.Source == "user":
		return "ADD", color.NRGBA{R: 0x6d, G: 0x47, B: 0x7a, A: 0xff}
	default:
		return "DEV", design.ColorSurfaceLight
	}
}

func driveRowPalette(isMounted, isMounting, unavailable bool) (fill color.Color, border color.Color) {
	switch {
	case isMounted:
		return color.NRGBA{R: 0x1d, G: 0x2d, B: 0x18, A: 0xff}, design.ColorAccent
	case isMounting:
		return color.NRGBA{R: 0x26, G: 0x26, B: 0x26, A: 0xff}, color.NRGBA{R: 0xc7, G: 0x9b, B: 0x52, A: 0xff}
	case unavailable:
		return color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff}, design.ColorBorder
	default:
		return design.ColorSurface, color.NRGBA{R: 0x4f, G: 0x4f, B: 0x4f, A: 0xff}
	}
}
