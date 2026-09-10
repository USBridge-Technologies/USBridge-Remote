package controller

import (
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// GetDashboardContainer builds the card-grid Devices tab: a narrow left
// column (HID & Input Hub, Video Pipe & EDID, Audio Pipeline) stacked above
// one another, and Virtual Mass Storage & ISO Media alone in a wide right
// column, all styled after the Connections grid's own cards (see
// view.NewDeviceDashboardCard).
//
// This is a first draft, read-only status view: rows show a device's name,
// connection state, and type badge, but don't yet carry the old list's
// mount/eject/upload actions -- see GetContainer's own selection-driven
// mount/unmount flow (disk_widget_mount.go) for that, which this doesn't
// replace yet.
func (dw *DiskWidget) GetDashboardContainer() fyne.CanvasObject {
	if dw.dashboardContainer != nil {
		return dw.dashboardContainer
	}

	dw.dashboardHID = container.NewVBox()
	dw.dashboardVideo = container.NewVBox()
	dw.dashboardAudio = container.NewVBox()
	dw.dashboardStorage = container.NewVBox()
	dw.refreshDashboard()

	narrowColumn := container.NewVBox(
		view.NewDeviceDashboardCard(assets.KeyboardIcon, "HID & Input Hub", nil, dw.dashboardHID),
		view.NewDeviceDashboardCard(assets.MonitorTabIcon, "Video Pipe & EDID", nil, dw.dashboardVideo),
		view.NewDeviceDashboardCard(assets.AudioIcon, "Audio Pipeline (UAC2)", nil, dw.dashboardAudio),
	)
	wideColumn := view.NewDeviceDashboardCard(assets.SDCardIcon, "Virtual Mass Storage & ISO Media", nil, dw.dashboardStorage)

	columns := container.New(&view.DeviceDashboardColumnsLayout{Gap: 16, Ratio: 2}, narrowColumn, wideColumn)
	// Scrollable, matching the old list view (view.DevicesListView is a
	// VScroll internally) -- the narrow column's three stacked cards plus
	// the wide column's storage list can easily exceed the tab's visible
	// height.
	dw.dashboardContainer = container.NewVScroll(view.NewInset(columns, 4, 4, 4, 4))
	return dw.dashboardContainer
}

// refreshDashboard repopulates each dashboard card's rows from dw.allDrives.
// Called once by GetDashboardContainer, and again by requestDevicesRefresh
// (disk_widget_refresh.go) whenever the underlying device set changes --
// a no-op while the dashboard hasn't been built (dashboardHID is nil), so
// this costs nothing for callers still on the old list-based GetContainer.
func (dw *DiskWidget) refreshDashboard() {
	if dw.dashboardHID == nil {
		return
	}

	var hidRows, videoRows, audioRows, storageRows []fyne.CanvasObject
	for _, drive := range dw.allDrives {
		badgeText, _ := driveBadge(drive)
		row := view.NewDeviceDashboardRow(dw.deviceRowText(drive), drive.IsMounted, badgeText)
		switch {
		case drive.IsKeyboard || drive.IsMouse || drive.IsGamepad:
			hidRows = append(hidRows, row)
		case drive.IsVideo:
			videoRows = append(videoRows, row)
		case drive.IsAudio || drive.IsUSBAudio:
			audioRows = append(audioRows, row)
		case drive.IsRNDIS:
			// Not shown in this first draft -- the dashboard doesn't have a
			// network-bridge card yet.
		default:
			storageRows = append(storageRows, row)
		}
	}

	setDashboardRows(dw.dashboardHID, hidRows, "No keyboard, mouse, or gamepad devices")
	setDashboardRows(dw.dashboardVideo, videoRows, "No capture devices")
	setDashboardRows(dw.dashboardAudio, audioRows, "No audio devices")
	setDashboardRows(dw.dashboardStorage, storageRows, "No storage or ISO media")
}

func setDashboardRows(target *fyne.Container, rows []fyne.CanvasObject, emptyText string) {
	if len(rows) == 0 {
		rows = []fyne.CanvasObject{view.NewDeviceDashboardEmptyState(emptyText)}
	}
	target.Objects = rows
	target.Refresh()
}
