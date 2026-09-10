package controller

import (
	"path/filepath"
	"strings"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// GetDashboardContainer builds the card-grid Devices tab: a narrow left
// column (HID & Input Hub, Video Pipe & EDID, Audio Pipeline) stacked above
// one another, and a wide right column (Virtual Mass Storage & ISO Media,
// and -- once a KVM/agent session actually offers one -- Virtual Network &
// NDIS Bridge below it), all styled after the Connections grid's own cards
// (see view.NewDeviceDashboardCard), including their own teal-on-hover
// border.
//
// HID/Network rows carry an on/off toggle (view.DeviceToggle); Storage rows
// carry Delete/Upload buttons plus a "Connected" badge once actually
// mounted (view.NewDeviceDashboardConnectedBadge) -- both drive the same
// toggleDriveMount, reusing the exact same handleMount/handleUnmount flow
// the old selection-driven list used, just pre-selecting the one row's own
// index instead of requiring the user to select it first. Video/Audio rows
// get neither: those two kinds are excluded from that mount flow entirely
// (see handleMount/handleUnmount's own IsVideo/IsAudio checks) and are
// driven by the Control tab's own start/stop and the header's audio menu
// instead, so a connect control here would just be misleading.
func (dw *DiskWidget) GetDashboardContainer() fyne.CanvasObject {
	if dw.dashboardContainer != nil {
		return dw.dashboardContainer
	}

	dw.dashboardHID = container.NewVBox()
	dw.dashboardVideo = container.NewVBox()
	dw.dashboardAudio = container.NewVBox()
	dw.dashboardStorage = container.NewVBox()
	dw.dashboardNetworkRows = container.NewVBox()

	// Each card's own hover cell (see view.NewDeviceDashboardHoverCell's
	// doc comment) is created up front -- refreshDashboard rebuilds every
	// row's buttons/toggles from scratch on every call, and each of those
	// needs the SAME onHover reference the card it lives in was bound to,
	// not a fresh one every refresh.
	var hidBind, videoBind, audioBind, storageBind, networkBind func(func(bool))
	dw.dashboardHIDHover, hidBind = view.NewDeviceDashboardHoverCell()
	dw.dashboardVideoHover, videoBind = view.NewDeviceDashboardHoverCell()
	dw.dashboardAudioHover, audioBind = view.NewDeviceDashboardHoverCell()
	dw.dashboardStorageHover, storageBind = view.NewDeviceDashboardHoverCell()
	dw.dashboardNetworkHover, networkBind = view.NewDeviceDashboardHoverCell()

	plusGlyph := view.NewDeviceDashboardPlusGlyph(10, view.DeviceDashboardHeaderButtonTextColor)
	addImageBtn := view.NewDeviceDashboardHeaderButton("Mount New ISO", plusGlyph, view.DeviceDashboardAccentLime, dw.handleAddImage)
	addImageBtn.OnHover = dw.dashboardStorageHover

	dw.refreshDashboard()

	dashboardNetworkCard := view.NewDeviceDashboardCard(assets.NetworkIcon, "Virtual Network & NDIS Bridge", "", nil, dw.dashboardNetworkRows, networkBind)
	dw.dashboardNetworkCard = dashboardNetworkCard
	dw.dashboardNetworkCard.Hide() // only shown once a real RNDIS device exists -- see refreshDashboard

	narrowColumn := container.NewVBox(
		view.NewDeviceDashboardCard(assets.KeyboardIcon, "HID & Input Hub", "", nil, dw.dashboardHID, hidBind),
		view.NewDeviceDashboardCardGap(),
		view.NewDeviceDashboardCard(assets.MonitorTabIcon, "Video Pipe & EDID", "", nil, dw.dashboardVideo, videoBind),
		view.NewDeviceDashboardCardGap(),
		view.NewDeviceDashboardCard(assets.AudioIcon, "Audio Pipeline (UAC2)", "", nil, dw.dashboardAudio, audioBind),
	)
	dw.dashboardWideColumn = container.NewVBox(
		view.NewDeviceDashboardCard(
			view.DeviceDashboardStorageIconSVG,
			"Virtual Mass Storage & ISO Media",
			"",
			addImageBtn,
			dw.dashboardStorage,
			storageBind,
		),
		view.NewDeviceDashboardCardGap(),
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
		name := dw.deviceRowText(drive)
		icon := driveIconResource(drive)

		switch {
		case drive.IsKeyboard || drive.IsMouse || drive.IsGamepad:
			hidRows = append(hidRows, view.NewDeviceDashboardRow(icon, name, drive.IsMounted, dw.newDriveToggle(idx, drive, dw.dashboardHIDHover)))
		case drive.IsVideo:
			// Excluded from the mount flow entirely (see this method's own
			// doc comment) -- no toggle.
			videoRows = append(videoRows, view.NewDeviceDashboardRow(icon, name, drive.IsMounted, nil))
		case drive.IsAudio || drive.IsUSBAudio:
			audioRows = append(audioRows, view.NewDeviceDashboardRow(icon, name, drive.IsMounted, nil))
		case drive.IsRNDIS:
			networkRows = append(networkRows, view.NewDeviceDashboardRow(icon, name, drive.IsMounted, dw.newDriveToggle(idx, drive, dw.dashboardNetworkHover)))
		default:
			if drive.IsUploading {
				storageRows = append(storageRows, view.NewDeviceDashboardStorageRow(icon, name, drive.IsMounted, nil, nil, nil, nil, view.NewDeviceDashboardUploadProgress(drive.UploadProgress)))
				continue
			}
			modePicker, deleteBtn, uploadBtn := dw.buildStorageRowExtras(idx, drive)
			// The "Connected" badge only shows up once the drive is
			// actually mounted -- before that, Upload (if applicable) and
			// Delete are the only actions; there's no separate "not yet
			// connected" state to show.
			var connectedBadge fyne.CanvasObject
			if drive.IsMounted {
				connectedBadge = view.NewDeviceDashboardConnectedBadge(func() {
					dw.toggleDriveMount(idx)
				}, dw.dashboardStorageHover)
			}
			storageRows = append(storageRows, view.NewDeviceDashboardStorageRow(icon, name, drive.IsMounted, modePicker, deleteBtn, uploadBtn, connectedBadge, nil))
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

// setDashboardRows fills target with rows, separated by a short inset
// divider between each pair (see view.NewDeviceDashboardRowSeparator) --
// not after the last row, so the card's own bottom padding stays clean.
func setDashboardRows(target *fyne.Container, rows []fyne.CanvasObject, emptyText string) {
	if len(rows) == 0 {
		target.Objects = []fyne.CanvasObject{view.NewDeviceDashboardEmptyState(emptyText)}
		target.Refresh()
		return
	}
	interleaved := make([]fyne.CanvasObject, 0, len(rows)*2-1)
	for i, row := range rows {
		if i > 0 {
			interleaved = append(interleaved, view.NewDeviceDashboardRowSeparator())
		}
		interleaved = append(interleaved, row)
	}
	target.Objects = interleaved
	target.Refresh()
}

// driveIconResource picks a drive's own type icon (folder for the user's
// own local files, disc for an API-provided ISO/drive, SD card for MTP,
// keyboard/mouse/gamepad/network/camera/audio for the matching HID/video/
// audio/network kind), brightened to its "Active" variant once mounted --
// mirrors configureDriveRow's own icon switch (disk_widget_row.go) exactly,
// just returning the resource instead of mutating a *canvas.Image in
// place.
func driveIconResource(drive DriveItem) fyne.Resource {
	var iconRes fyne.Resource
	useStorageIcon := false
	switch drive.Source {
	case "api":
		useStorageIcon = true
		if drive.LocalDrive != nil && drive.LocalDrive.SourceType == "mtp" {
			iconRes = assets.SDCardIcon
		} else {
			iconRes = assets.DiscIcon
		}
	case "local", "user":
		useStorageIcon = true
		iconRes = assets.FolderIcon
	case "keyboard":
		iconRes = assets.KeyboardIcon
		if drive.IsMounted {
			iconRes = assets.KeyboardIconActive
		}
	case "mouse":
		iconRes = assets.MouseIcon
		if drive.IsMounted {
			iconRes = assets.MouseIconActive
		}
	case "rndis":
		iconRes = assets.NetworkIcon
		if drive.IsMounted {
			iconRes = assets.NetworkIconActive
		}
	case "gamepad":
		iconRes = assets.GamepadIcon
		if drive.IsMounted {
			iconRes = assets.GamepadIconActive
		}
	case "video":
		iconRes = assets.CameraIcon
		if drive.IsMounted {
			iconRes = assets.CameraIconActive
		}
	case "audio", "usbaudio":
		iconRes = assets.AudioIcon
		if drive.IsMounted {
			iconRes = assets.AudioIconActive
		}
	default:
		iconRes = assets.DiscIcon
	}

	if useStorageIcon && drive.IsMounted {
		switch iconRes {
		case assets.FolderIcon:
			iconRes = assets.FolderIconActive
		case assets.SDCardIcon:
			iconRes = assets.SDCardIconActive
		default:
			iconRes = assets.DiscIconActive
		}
	}
	return iconRes
}

// buildStorageRowExtras builds a Storage row's own USB Stick/CD-ROM mode
// picker and Delete/Upload buttons, mirroring configureDriveRow's own
// gating and wiring (disk_widget_row.go) exactly -- any return value may be
// nil, meaning that row doesn't get that control (e.g. a plain local file
// isn't ISO-compatible, so it gets no mode picker; an API/local drive that
// isn't the user's own upload gets no delete button unless it's not the
// backup flash). The upload button only shows up before the file is
// mounted -- once mounted, the row's own connect button (bright "Connected")
// already speaks for it. Not called at all while the drive is actively
// uploading -- see refreshDashboard, which shows a progress bar instead.
func (dw *DiskWidget) buildStorageRowExtras(idx int, drive DriveItem) (modePicker, deleteBtn, uploadBtn fyne.CanvasObject) {
	isAPIISODrive := drive.Source == "api" && drive.LocalDrive != nil && drive.LocalDrive.SourceType != "mtp"
	isNBDISODrive := (drive.Source == "local" || drive.Source == "user") && drive.DiskInfo != nil &&
		isISOCompatibleExt(strings.ToLower(filepath.Ext(drive.DiskInfo.Path)))
	if isAPIISODrive || isNBDISODrive {
		selected := i18n.Current.DriveModeDisk
		if drive.DriveMode == "cdrom" {
			selected = i18n.Current.DriveModeCDROM
		}
		picker := view.NewDeviceDashboardModePicker(
			[]string{i18n.Current.DriveModeDisk, i18n.Current.DriveModeCDROM},
			selected,
			func(s string) {
				if dw.controlsLocked() || idx >= len(dw.allDrives) {
					return
				}
				mode := "disk"
				if s == i18n.Current.DriveModeCDROM {
					mode = "cdrom"
					// CD-ROM mode is always read-only (physical CD-ROMs
					// cannot be written).
					dw.allDrives[idx].ReadOnly = true
				}
				dw.allDrives[idx].DriveMode = mode
			},
		)
		picker.OnHover = dw.dashboardStorageHover
		modePicker = picker
	}

	if drive.Source == "user" && drive.DiskInfo != nil && !drive.IsMounting && !drive.IsMounted {
		btn := view.NewDeviceDashboardUploadButton(func() {
			if !dw.controlsLocked() {
				dw.handleUploadImage(idx)
			}
		}, dw.dashboardStorageHover)
		btn.SetDisabled(drive.IsUploading || dw.controlsLocked())
		uploadBtn = btn
	}

	shouldShowDelete := false
	if !drive.IsMounting {
		if drive.Source == "user" {
			shouldShowDelete = true
		} else if drive.Source == "api" || drive.Source == "local" {
			isBackupFlash := drive.LocalDrive != nil && drive.LocalDrive.Name == "data" && drive.LocalDrive.SourceType == "mtp"
			shouldShowDelete = !isBackupFlash
		}
	}
	if shouldShowDelete {
		var onTap func()
		switch drive.Source {
		case "user":
			onTap = func() {
				if !dw.controlsLocked() {
					dw.removeUserImage(idx)
				}
			}
		default: // "api" or "local"
			filename := drive.Name
			if drive.LocalDrive != nil {
				filename = drive.LocalDrive.Name
			} else if drive.DiskInfo != nil {
				filename = drive.DiskInfo.Name
			}
			onTap = func() {
				if !dw.controlsLocked() {
					dw.handleDeleteImageFromDevice(idx, filename)
				}
			}
		}
		btn := view.NewDeviceDashboardDeleteButton(onTap, dw.dashboardStorageHover)
		btn.SetDisabled(dw.controlsLocked())
		deleteBtn = btn
	}

	return modePicker, deleteBtn, uploadBtn
}

// newDriveToggle builds a HID/Network row's own on/off switch, wired to
// toggleDriveMount and to cardHover (that row's own card's onHover cell --
// see view.NewDeviceDashboardHoverCell).
func (dw *DiskWidget) newDriveToggle(idx int, drive DriveItem, cardHover func(bool)) *view.DeviceToggle {
	t := view.NewDeviceToggle(drive.IsMounted, func(bool) {
		dw.toggleDriveMount(idx)
	})
	t.OnHover = cardHover
	return t
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
