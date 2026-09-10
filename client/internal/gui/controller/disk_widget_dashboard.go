package controller

import (
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// GetDashboardContainer builds the card-grid Devices tab: a narrow left
// column (HID & Input Hub, Video Pipe & EDID, Audio Pipeline) stacked above
// one another, and a wide right column (Virtual Mass Storage & ISO Media,
// and -- once a KVM/agent session actually offers one -- Virtual Network &
// NDIS Bridge below it), all styled after the Connections grid's own cards
// (see view.NewDeviceDashboardCard).
//
// Storage/HID/Network rows carry a real mount/unmount toggle (see
// toggleDriveMount, reusing the exact same handleMount/handleUnmount flow
// the old selection-driven list used -- just pre-selecting the one row's
// own index instead of requiring the user to select it first). Video/Audio
// rows don't: those two kinds are excluded from that mount flow entirely
// (see handleMount/handleUnmount's own IsVideo/IsAudio checks) and are
// driven by the Control tab's own start/stop and the header's audio menu
// instead, so a toggle here would just be misleading.
func (dw *DiskWidget) GetDashboardContainer() fyne.CanvasObject {
	if dw.dashboardContainer != nil {
		return dw.dashboardContainer
	}

	dw.dashboardHID = container.NewVBox()
	dw.dashboardVideo = container.NewVBox()
	dw.dashboardAudio = container.NewVBox()
	dw.dashboardStorage = container.NewVBox()
	dw.dashboardNetworkRows = container.NewVBox()

	addImageBtn := view.NewDeviceActionButton("+ Mount ISO", nil, dw.handleAddImage)
	dw.dashboardNetworkCard = view.NewDeviceDashboardCard(assets.NetworkIcon, "Virtual Network & NDIS Bridge", nil, dw.dashboardNetworkRows)
	dw.dashboardNetworkCard.Hide() // only shown once a real RNDIS device exists -- see refreshDashboard

	dw.refreshDashboard()

	narrowColumn := container.NewVBox(
		view.NewDeviceDashboardCard(assets.KeyboardIcon, "HID & Input Hub", nil, dw.dashboardHID),
		view.NewDeviceDashboardCard(assets.MonitorTabIcon, "Video Pipe & EDID", nil, dw.dashboardVideo),
		view.NewDeviceDashboardCard(assets.AudioIcon, "Audio Pipeline (UAC2)", nil, dw.dashboardAudio),
	)
	dw.dashboardWideColumn = container.NewVBox(
		view.NewDeviceDashboardCard(assets.SDCardIcon, "Virtual Mass Storage & ISO Media", addImageBtn, dw.dashboardStorage),
		dw.dashboardNetworkCard,
	)

	columns := container.New(&view.DeviceDashboardColumnsLayout{Gap: 16, Ratio: 1.4}, narrowColumn, dw.dashboardWideColumn)
	// Scrollable, matching the old list view (view.DevicesListView is a
	// VScroll internally) -- the narrow column's three stacked cards plus
	// the wide column's storage list can easily exceed the tab's visible
	// height.
	dw.dashboardContainer = container.NewVScroll(view.NewInset(columns, 18, 18, 16, 16))
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

	var hidRows, videoRows, audioRows, storageRows, networkRows []fyne.CanvasObject
	for idx, drive := range dw.allDrives {
		badgeText, _ := driveBadge(drive)
		name := dw.deviceRowText(drive)

		// Video/Audio aren't mount-toggle-able through this flow (see this
		// method's own doc comment) -- their rows get no toggle at all.
		var toggle fyne.CanvasObject
		if !drive.IsVideo && !drive.IsAudio {
			mounted := drive.IsMounted
			t := view.NewDeviceToggle(mounted, func(bool) {
				dw.toggleDriveMount(idx)
			})
			toggle = t
		}

		row := view.NewDeviceDashboardRow(name, drive.IsMounted, badgeText, toggle)
		switch {
		case drive.IsKeyboard || drive.IsMouse || drive.IsGamepad:
			hidRows = append(hidRows, row)
		case drive.IsVideo:
			videoRows = append(videoRows, row)
		case drive.IsAudio || drive.IsUSBAudio:
			audioRows = append(audioRows, row)
		case drive.IsRNDIS:
			networkRows = append(networkRows, row)
		default:
			storageRows = append(storageRows, row)
		}
	}

	setDashboardRows(dw.dashboardHID, hidRows, "No keyboard, mouse, or gamepad devices")
	setDashboardRows(dw.dashboardVideo, videoRows, "No capture devices")
	setDashboardRows(dw.dashboardAudio, audioRows, "No audio devices")
	setDashboardRows(dw.dashboardStorage, storageRows, "No storage or ISO media")
	setDashboardRows(dw.dashboardNetworkRows, networkRows, "No network bridge devices")

	if dw.dashboardNetworkCard != nil {
		wasVisible := dw.dashboardNetworkCard.Visible()
		if len(networkRows) > 0 {
			dw.dashboardNetworkCard.Show()
		} else {
			dw.dashboardNetworkCard.Hide()
		}
		// Container.Show()/Hide() alone don't force a relayout -- without
		// this, the wide column wouldn't actually reserve or collapse the
		// card's space until something else happened to refresh it.
		if dw.dashboardNetworkCard.Visible() != wasVisible && dw.dashboardWideColumn != nil {
			dw.dashboardWideColumn.Refresh()
		}
	}
}

func setDashboardRows(target *fyne.Container, rows []fyne.CanvasObject, emptyText string) {
	if len(rows) == 0 {
		rows = []fyne.CanvasObject{view.NewDeviceDashboardEmptyState(emptyText)}
	}
	target.Objects = rows
	target.Refresh()
}

// toggleDriveMount mounts or unmounts exactly the one drive at index,
// reusing handleMount/handleUnmount's own real, selection-driven flow (NBD
// servers, gadget requests, confirmation dialogs -- everything the old
// list's per-row checkbox drove) by pre-selecting just that index instead
// of requiring the user to select it through a list first.
func (dw *DiskWidget) toggleDriveMount(index int) {
	if index < 0 || index >= len(dw.allDrives) {
		return
	}
	drive := dw.allDrives[index]

	dw.selectedItemsMu.Lock()
	dw.selectedItems = map[int]bool{index: true}
	dw.selectedItemsMu.Unlock()

	if drive.IsMounted {
		dw.handleUnmount()
	} else {
		dw.handleMount()
	}
}
