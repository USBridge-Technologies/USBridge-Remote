package controller

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"github.com/sirupsen/logrus"
)

// GetDashboardContainer builds the card-grid Devices tab: a narrow left
// column (HID & Input Hub, Video Pipe & EDID, Audio Pipeline) stacked above
// one another, and a wide right column (Virtual Mass Storage & ISO Media,
// then USB Emulation, then a short Network + Backups pair or the firmware
// promo on an agent), all styled
// after the Connections grid's own cards (see view.NewDeviceDashboardCard),
// including their own teal-on-hover border.
//
// HID/Network rows carry an on/off toggle (view.DeviceToggle); Storage rows
// carry Delete/Upload buttons plus a mount button, replaced by a disconnect
// button once actually mounted (view.NewDeviceDashboardMountButton/
// NewDeviceDashboardDisconnectButton) -- both drive the same
// toggleDriveMount, reusing the exact same handleMount/handleUnmount flow
// the old selection-driven list used, just pre-selecting the one row's own
// index instead of requiring the user to select it first. Video/Audio rows
// carry a round exclusive radio (view.NewDeviceDashboardCaptureSelector)
// instead: only one video source and only one audio source can be on, so
// a HID-style independent toggle would be misleading. USB Audio Codec
// also gets the UAC1/UAC2 picker the old list had.
func (dw *DiskWidget) GetDashboardContainer() fyne.CanvasObject {
	if dw.dashboardContainer != nil {
		return dw.dashboardContainer
	}

	dw.dashboardHID = container.NewVBox()
	dw.dashboardVideo = container.NewVBox()
	dw.dashboardAudio = container.NewVBox()
	dw.dashboardStorage = container.NewVBox()
	dw.dashboardEmulation = container.NewVBox()
	dw.dashboardNetworkRows = container.NewVBox()
	dw.dashboardBackup = container.NewVBox()

	// Each card's own hover cell (see view.NewDeviceDashboardHoverCell's
	// doc comment) is created up front -- refreshDashboard rebuilds every
	// row's buttons/toggles from scratch on every call, and each of those
	// needs the SAME onHover reference the card it lives in was bound to,
	// not a fresh one every refresh.
	var hidBind, videoBind, audioBind, storageBind, emulationBind, networkBind, backupBind func(func(bool))
	dw.dashboardHIDHover, hidBind = view.NewDeviceDashboardHoverCell()
	dw.dashboardVideoHover, videoBind = view.NewDeviceDashboardHoverCell()
	dw.dashboardAudioHover, audioBind = view.NewDeviceDashboardHoverCell()
	dw.dashboardStorageHover, storageBind = view.NewDeviceDashboardHoverCell()
	dw.dashboardEmulationHover, emulationBind = view.NewDeviceDashboardHoverCell()
	dw.dashboardNetworkHover, networkBind = view.NewDeviceDashboardHoverCell()
	dw.dashboardBackupHover, backupBind = view.NewDeviceDashboardHoverCell()

	plusGlyph := view.NewDeviceDashboardPlusGlyph(10, view.DeviceDashboardHeaderButtonTextColor)
	addImageBtn := view.NewDeviceDashboardHeaderButton("Mount New ISO", plusGlyph, view.DeviceDashboardAccentLime, dw.handleAddImage)
	addImageBtn.OnHover = dw.dashboardStorageHover
	dw.dashboardAddImageBtn = addImageBtn

	dashboardNetworkCard := view.NewDeviceDashboardCard(view.DeviceDashboardNetworkIconSVG, "Network", "", nil, dw.dashboardNetworkRows, networkBind)
	dw.dashboardNetworkCard = dashboardNetworkCard
	dw.dashboardNetworkCard.Hide() // only shown once a real RNDIS device exists -- see refreshDashboard

	dw.dashboardBackupSpace = view.NewDeviceDashboardSpaceMeter()
	dw.dashboardBackupSpace.OnHover = dw.dashboardBackupHover
	dw.syncDashboardBackupSpace()
	dashboardBackupCard := view.NewDeviceDashboardCard(view.DeviceDashboardBackupsIconSVG, "Backups", "", dw.dashboardBackupSpace, dw.dashboardBackup, backupBind)
	dw.dashboardBackupCard = dashboardBackupCard
	dw.dashboardBackupCard.Hide() // only shown once the MTP backup flash exists -- see refreshDashboard

	narrowColumn := container.NewVBox(
		view.NewDeviceDashboardCard(view.DeviceDashboardHIDIconSVG, "HID & Input Hub", "", nil, dw.dashboardHID, hidBind),
		view.NewDeviceDashboardCardGap(),
		view.NewDeviceDashboardCard(view.DeviceDashboardVideoIconSVG, "Video Pipe & EDID", "", nil, dw.dashboardVideo, videoBind),
		view.NewDeviceDashboardCardGap(),
		view.NewDeviceDashboardCard(view.DeviceDashboardAudioIconSVG, "Audio Pipeline (UAC2)", "", nil, dw.dashboardAudio, audioBind),
	)
	// Wrapped in a Scroll from the start (rather than only once there
	// happen to be enough drives) so refreshDashboard can just adjust its
	// own SetMinSize every time instead of swapping the card's content
	// object -- with dashboardStorage's own natural height as that
	// min size, the Scroll is indistinguishable from a plain VBox until
	// refreshDashboard caps it past dashboardStorageVisibleRows. A little
	// right padding on the row list itself (not the Scroll) keeps its own
	// mode-picker/Delete/Upload/mount buttons clear of where the vertical
	// scrollbar thumb overlays once scrolling is actually active.
	dw.dashboardStorageScroll = container.NewVScroll(view.NewInsetExact(dw.dashboardStorage, 0, 10, 0, 0))
	dw.dashboardEmulationScroll = container.NewVScroll(view.NewInsetExact(dw.dashboardEmulation, 0, 10, 0, 0))

	pairRow := container.New(&view.DeviceDashboardPairLayout{Gap: 12}, dw.dashboardNetworkCard, dw.dashboardBackupCard)
	dw.dashboardPairRow = pairRow
	dw.dashboardFirmwarePromo = view.NewDeviceFirmwarePromo()
	dw.dashboardFirmwarePromo.SetOnDismiss(dw.dismissFirmwarePromo)
	dw.dashboardFirmwarePromo.SetOnOpen(dw.openFirmwarePromo)
	dw.dashboardPairSection = container.NewVBox(view.NewDeviceDashboardCardGap(), pairRow, dw.dashboardFirmwarePromo)
	dw.dashboardPairSection.Hide()

	storageCard := view.NewDeviceDashboardCard(
		view.DeviceDashboardStorageIconSVG,
		deviceDashboardStorageTitle,
		"",
		addImageBtn,
		dw.dashboardStorageScroll,
		storageBind,
	)
	emulationCard := view.NewDeviceDashboardCard(
		view.DeviceDashboardUSBIconSVG,
		deviceDashboardEmulationTitle,
		"",
		nil,
		dw.dashboardEmulationScroll,
		emulationBind,
	)

	dw.dashboardWideColumn = container.NewVBox(
		storageCard,
		view.NewDeviceDashboardCardGap(),
		emulationCard,
		dw.dashboardPairSection,
	)

	columns := container.New(&view.DeviceDashboardColumnsLayout{Gap: 16, Ratio: 1.4}, narrowColumn, dw.dashboardWideColumn)
	// Scrollable, matching the old list view (view.DevicesListView is a
	// VScroll internally) -- the narrow column's three stacked cards plus
	// the wide column's storage list can easily exceed the tab's visible
	// height. Footer sits outside the scroll so the lime busy spinner,
	// Disconnect All, and version stay pinned to the bottom of the tab.
	scroll := container.NewVScroll(view.NewInset(columns, 18, 18, 16, 8))
	dw.dashboardFooterDisconnect = view.NewDeviceDashboardFooterTextButton(i18n.Current.DisconnectAllButton, func() {
		if dw.controlsLocked() {
			return
		}
		dw.selectedItemsMu.Lock()
		dw.selectedItems = map[int]bool{}
		dw.selectedItemsMu.Unlock()
		dw.handleUnmount()
	})
	dw.dashboardBusySpinner = view.NewDeviceDashboardBusyHint("connecting device")
	dw.firmwareChip = view.NewFooterLabelChip("software")
	dw.firmwareChip.SetOnOpen(dw.openFirmwarePromo)
	dw.firmwareChip.SetOnRestore(dw.restoreFirmwarePromo)
	footer := view.NewAppFooter(view.AppVersion(), dw.dashboardFooterDisconnect, dw.dashboardBusySpinner, dw.dashboardScriptFooter, dw.firmwareChip)
	dw.dashboardContainer = view.NewEdgeStack(nil, footer, scroll)
	dw.refreshDashboard()
	return dw.dashboardContainer
}

// SetDashboardScriptFooter injects the shared script-run chip into the
// Devices footer. Must be called before GetDashboardContainer builds the
// tab, otherwise the chip is ignored until the next rebuild.
func (dw *DiskWidget) SetDashboardScriptFooter(chip *view.ScriptFooterStatus) {
	if dw == nil {
		return
	}
	dw.dashboardScriptFooter = chip
}

// AttachConnectingHint registers another tab's "connecting device" spinner
// so beginOperation/endOperation can drive it alongside Devices' own.
func (dw *DiskWidget) AttachConnectingHint(hint *view.DeviceDashboardBusySpinner) {
	if dw == nil || hint == nil {
		return
	}
	dw.connectingHints = append(dw.connectingHints, hint)
}

const (
	devicesFirmwarePromoDismissedPrefKey = "devices.firmware_promo.dismissed"
	deviceDashboardStorageTitle          = "Virtual Mass Storage & ISO Media"
	deviceDashboardEmulationTitle        = "USB Emulation"
)

func (dw *DiskWidget) firmwarePromoDismissed() bool {
	if dw.app == nil {
		return false
	}
	return dw.app.Preferences().BoolWithFallback(devicesFirmwarePromoDismissedPrefKey, false)
}

func (dw *DiskWidget) setFirmwarePromoDismissed(on bool) {
	if dw.app != nil {
		dw.app.Preferences().SetBool(devicesFirmwarePromoDismissedPrefKey, on)
	}
}

func (dw *DiskWidget) dismissFirmwarePromo() {
	dw.setFirmwarePromoDismissed(true)
	dw.refreshDashboard()
}

func (dw *DiskWidget) restoreFirmwarePromo() {
	dw.setFirmwarePromoDismissed(false)
	dw.refreshDashboard()
}

func (dw *DiskWidget) openFirmwarePromo() {
	uri, err := url.Parse(view.FirmwarePromoURL)
	if err != nil {
		logrus.Errorf("failed to parse firmware promo URL %q: %v", view.FirmwarePromoURL, err)
		return
	}
	fyneApp := dw.app
	if fyneApp == nil {
		fyneApp = fyne.CurrentApp()
	}
	if fyneApp == nil {
		logrus.Errorf("failed to open firmware promo URL: fyne app is nil")
		return
	}
	go func() {
		if err := fyneApp.OpenURL(uri); err != nil {
			logrus.Errorf("failed to open firmware promo URL %q: %v", view.FirmwarePromoURL, err)
		}
	}()
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

	// "Mount New ISO" darkens while its own file picker is open -- and
	// greys out (SetEnabled) while a mount/unmount is in flight. The
	// picker uses imagePickerInFlight so it doesn't share the footer's
	// lime spinner, which is reserved for gadget connect/disconnect.
	if dw.dashboardAddImageBtn != nil {
		pickerOpen := dw.imagePickerInFlight.Load()
		dw.dashboardAddImageBtn.SetBusy(pickerOpen)
		// File-picker busy is its own visual; while a mount/unmount is in
		// flight the pill is just unclickable so it doesn't compete with
		// the footer spinner.
		dw.dashboardAddImageBtn.SetEnabled(!dw.controlsLocked() || pickerOpen)
	}

	var hidRows, videoRows, audioRows, storageRows, emulationRows, networkRows, backupRows []fyne.CanvasObject
	type hidDrive struct {
		idx   int
		drive DriveItem
	}
	var hidKeyboard, hidMouse *hidDrive
	var hidGamepads []hidDrive
	for idx, drive := range dw.allDrives {
		name := dw.deviceRowText(drive)
		icon := driveIconResource(drive)

		switch {
		case drive.IsKeyboard:
			d := hidDrive{idx: idx, drive: drive}
			hidKeyboard = &d
		case drive.IsMouse:
			d := hidDrive{idx: idx, drive: drive}
			hidMouse = &d
		case drive.IsGamepad:
			hidGamepads = append(hidGamepads, hidDrive{idx: idx, drive: drive})
		case drive.IsVideo:
			chipText, tealChip := videoDashboardLatencyChip(drive)
			videoActive := dw.isPreferredVideoDrive(drive)
			if videoActive {
				icon = view.DeviceDashboardCameraIconActive
			}
			videoRows = append(videoRows, view.NewDeviceDashboardVideoRow(
				icon,
				dw.captureDeviceBaseTitle(drive),
				videoActive,
				chipText,
				tealChip,
				dw.newDashboardVideoSettingsButton(drive),
				dw.newDashboardVideoRadio(drive),
			))
		case drive.IsAudio || drive.IsUSBAudio:
			var extras []fyne.CanvasObject
			if drive.IsUSBAudio {
				extras = append(extras, dw.newDashboardUSBAudioModePicker(idx, drive))
			}
			extras = append(extras, dw.newDashboardAudioRadio(idx, drive))
			audioRows = append(audioRows, view.NewDeviceDashboardAudioRow(icon, name, drive.IsMounted, extras...))
		case drive.IsRNDIS:
			netName, netChip := dashboardNetworkTitle(name)
			networkRows = append(networkRows, view.NewDeviceDashboardLimeRow(
				icon, netName, drive.IsMounted, netChip,
				dw.newDashboardRNDISSettingsButton(idx, drive),
				dw.newDriveToggle(idx, drive, dw.dashboardNetworkHover),
			))
		case isDashboardBackupDrive(drive):
			backupRows = append(backupRows, view.NewDeviceDashboardStorageRow(
				icon, name, drive.IsMounted, nil, nil, nil,
				dw.newDashboardConnectSlot(idx, drive, dw.dashboardBackupHover),
				nil, dw.dashboardBackupChips(drive.Size)...,
			))
		case drive.IsUSBPassthrough:
			emulationRows = append(emulationRows, view.NewDeviceDashboardStorageRow(
				icon, name, drive.IsMounted, nil, nil, nil,
				dw.newDashboardConnectSlot(idx, drive, dw.dashboardEmulationHover),
				nil, drive.Size,
			))
		default:
			if drive.IsUploading {
				storageRows = append(storageRows, view.NewDeviceDashboardStorageRow(icon, name, drive.IsMounted, nil, nil, nil, nil, view.NewDeviceDashboardUploadProgress(drive.UploadProgress), drive.Size))
				continue
			}
			modePicker, deleteBtn, uploadBtn := dw.buildStorageRowExtras(idx, drive)
			storageRows = append(storageRows, view.NewDeviceDashboardStorageRow(icon, name, drive.IsMounted, modePicker, deleteBtn, uploadBtn, dw.newDashboardConnectSlot(idx, drive, dw.dashboardStorageHover), nil, drive.Size))
		}
	}

	if hidKeyboard != nil || hidMouse != nil {
		var kbCell, mouseCell fyne.CanvasObject
		if hidKeyboard != nil {
			kbCell = view.NewDeviceDashboardHIDCell(
				driveIconResource(hidKeyboard.drive),
				dw.deviceRowText(hidKeyboard.drive),
				hidKeyboard.drive.IsMounted,
				dw.newDriveToggle(hidKeyboard.idx, hidKeyboard.drive, dw.dashboardHIDHover),
			)
		}
		if hidMouse != nil {
			mouseCell = view.NewDeviceDashboardHIDCell(
				driveIconResource(hidMouse.drive),
				dw.deviceRowText(hidMouse.drive),
				hidMouse.drive.IsMounted,
				dw.newDashboardMouseSettingsButton(hidMouse.idx, hidMouse.drive),
				dw.newDriveToggle(hidMouse.idx, hidMouse.drive, dw.dashboardHIDHover),
			)
		}
		hidRows = append(hidRows, view.NewDeviceDashboardHIDPairRow(kbCell, mouseCell))
	}
	for i, pad := range hidGamepads {
		name := strings.TrimSpace(dw.deviceRowText(pad.drive))
		if name == "" {
			name = i18n.Current.DeviceGamepad
		}
		if len(hidGamepads) > 1 {
			name = fmt.Sprintf("%s (%d)", name, i+1)
		}
		hidRows = append(hidRows, view.NewDeviceDashboardTealRow(
			driveIconResource(pad.drive),
			name,
			pad.drive.IsMounted,
			dw.newDashboardGamepadModePicker(pad.idx, pad.drive),
			dw.newDriveToggle(pad.idx, pad.drive, dw.dashboardHIDHover),
		))
	}

	setDashboardRows(dw.dashboardHID, hidRows, "No keyboard, mouse, or gamepad devices")
	setDashboardRows(dw.dashboardVideo, videoRows, "No capture devices")
	setDashboardRows(dw.dashboardAudio, audioRows, "No audio devices")
	setDashboardRows(dw.dashboardStorage, storageRows, "No storage or ISO media")
	if dw.dashboardEmulation != nil {
		setDashboardRows(dw.dashboardEmulation, emulationRows, "No USB devices")
	}

	softwareAgent := !isUSBridgeAgentOS(dw.agentOS)
	promoDismissed := dw.firmwarePromoDismissed()
	showPromo := softwareAgent && !promoDismissed

	setDashboardRows(dw.dashboardNetworkRows, networkRows, "No network bridge devices")
	if dw.dashboardBackup != nil {
		setDashboardRows(dw.dashboardBackup, backupRows, "No backup devices")
	}

	networkOn := !softwareAgent && len(networkRows) > 0
	backupOn := !softwareAgent && len(backupRows) > 0
	showPair := networkOn || backupOn
	if dw.dashboardNetworkCard != nil {
		if networkOn {
			dw.dashboardNetworkCard.Show()
		} else {
			dw.dashboardNetworkCard.Hide()
		}
	}
	if dw.dashboardBackupCard != nil {
		if backupOn {
			dw.dashboardBackupCard.Show()
		} else {
			dw.dashboardBackupCard.Hide()
		}
	}
	if dw.dashboardPairRow != nil {
		if showPair {
			dw.dashboardPairRow.Show()
		} else {
			dw.dashboardPairRow.Hide()
		}
	}
	if dw.dashboardFirmwarePromo != nil {
		if showPromo {
			dw.dashboardFirmwarePromo.Show()
		} else {
			dw.dashboardFirmwarePromo.Hide()
		}
	}
	if dw.dashboardPairSection != nil {
		if showPromo || showPair {
			dw.dashboardPairSection.Show()
		} else {
			dw.dashboardPairSection.Hide()
		}
	}
	if dw.firmwareChip != nil {
		dw.firmwareChip.SetActive(softwareAgent && promoDismissed)
	}

	if dw.dashboardStorageScroll != nil {
		// Past dashboardStorageVisibleRows, cap the Scroll's own height at
		// exactly that many rows (dashboardStorageCapHeight) so the rest
		// become internally scrollable instead of pushing the Video/Audio/
		// HID cards in the other column further down the page; below that,
		// give it the row list's own natural height so it reads exactly
		// like a plain, non-scrolling list (see NewDeviceDashboardCard's
		// content, wired to this Scroll in GetDashboardContainer).
		height := dashboardStorageCapHeight(storageRows)
		if height <= 0 {
			height = dw.dashboardStorage.MinSize().Height
		}
		dw.dashboardStorageScroll.SetMinSize(fyne.NewSize(0, height))
	}
	if dw.dashboardEmulationScroll != nil && dw.dashboardEmulation != nil {
		height := dashboardStorageCapHeight(emulationRows)
		if height <= 0 {
			height = dw.dashboardEmulation.MinSize().Height
		}
		dw.dashboardEmulationScroll.SetMinSize(fyne.NewSize(0, height))
	}

	// Container.Show()/Hide()/SetMinSize() alone don't force a relayout --
	// without this, the wide column wouldn't actually reserve/collapse the
	// network/backup cards' space or resize the storage scroll's own viewport
	// until something else happened to refresh it.
	if dw.dashboardWideColumn != nil {
		dw.dashboardWideColumn.Refresh()
	}
	dw.syncDashboardBackupSpace()
	dw.syncDashboardFooter()
}

// dashboardStorageVisibleRows caps how many Storage rows show before the
// card's own row list becomes internally scrollable (see refreshDashboard).
// Enough to read as "several drives" without letting one Storage card with
// many ISOs push the Video/Audio/HID cards in the other column far down
// the page.
const dashboardStorageVisibleRows = 6

// dashboardStorageCapHeight returns the pixel height of the first
// dashboardStorageVisibleRows rows plus the separators between them,
// built the exact same way setDashboardRows interleaves the real list --
// and measured via layout.NewVBoxLayout().MinSize() (dashboardStorage's
// own layout) rather than just summing each item's own MinSize, since
// VBoxLayout also adds theme.Padding() between every pair of children.
// Missing that padding here previously undercounted the cap by about one
// row's worth across 6 rows + 5 separators (10 gaps), so a 7th row made
// the card actually shrink to fit only 5 fully instead of the intended 6.
// Returns 0 if there aren't more rows than that, meaning the caller
// should leave the row list sized naturally instead of capping it.
func dashboardStorageCapHeight(rows []fyne.CanvasObject) float32 {
	if len(rows) <= dashboardStorageVisibleRows {
		return 0
	}
	items := make([]fyne.CanvasObject, 0, dashboardStorageVisibleRows*2-1)
	for i := 0; i < dashboardStorageVisibleRows; i++ {
		if i > 0 {
			items = append(items, view.NewDeviceDashboardRowSeparator())
		}
		items = append(items, rows[i])
	}
	return layout.NewVBoxLayout().MinSize(items).Height
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
			iconRes = view.DeviceDashboardKeyboardIconActive
		}
	case "mouse":
		iconRes = assets.MouseIcon
		if drive.IsMounted {
			iconRes = view.DeviceDashboardMouseIconActive
		}
	case "rndis":
		iconRes = assets.NetworkIcon
		if drive.IsMounted {
			iconRes = view.DeviceDashboardNetworkIconActive
		}
	case "gamepad":
		iconRes = assets.GamepadIcon
		if drive.IsMounted {
			iconRes = view.DeviceDashboardGamepadIconActive
		}
	case "video":
		iconRes = assets.CameraIcon
		if drive.IsMounted {
			iconRes = view.DeviceDashboardCameraIconActive
		}
	case "audio", "usbaudio":
		iconRes = assets.AudioIcon
		if drive.IsMounted {
			iconRes = view.DeviceDashboardAudioIconActive
		}
	case "usbpass":
		iconRes = assets.USBTabIcon
		if drive.IsMounted {
			iconRes = view.DeviceDashboardUSBIconActive
		}
	default:
		iconRes = assets.DiscIcon
	}

	if useStorageIcon && drive.IsMounted {
		// This dashboard's own lime (#c4e77a) instead of the shared
		// assets.*Active constants' green (#93C572) -- matches the row's
		// own name text color (see newDeviceDashboardRowLeftSized).
		switch iconRes {
		case assets.FolderIcon:
			iconRes = view.DeviceDashboardFolderIconActive
		case assets.SDCardIcon:
			iconRes = view.DeviceDashboardSDCardIconActive
		default:
			iconRes = view.DeviceDashboardDiscIconActive
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
		picker.SetDisabled(dw.controlsLocked())
		modePicker = picker
	}

	if drive.Source == "user" && drive.DiskInfo != nil && !drive.IsMounting && !drive.IsMounted {
		btn := view.NewDeviceDashboardUploadButton(func() {
			if !dw.controlsLocked() {
				dw.handleUploadImage(idx)
			}
		}, dw.dashboardStorageHover)
		btn.SetDisabled(dw.controlsLocked())
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

// isDashboardBackupDrive reports the MTP backup flash (api source, mtp
// "data" partition) -- the old list put this in its own Backup section
// (disk_widget_sections.go); the dashboard keeps it out of Storage and
// on its own Backups card instead.
func isDashboardBackupDrive(drive DriveItem) bool {
	return drive.Source == "api" && drive.LocalDrive != nil && drive.LocalDrive.SourceType == "mtp"
}

// SetDashboardSnapshotCount updates the Backups row's snapshot-count plaque
// and whether a snapshot MTP is currently mounted. Wired from BackupWidget
// after each GetSnapshots so Devices doesn't poll the same endpoint a second
// time -- and so the Backups mount button can warn before a second MTP source.
func (dw *DiskWidget) SetDashboardSnapshotCount(n int, snapshotMounted bool) {
	if n < 0 {
		n = 0
	}
	if dw.dashboardSnapshotKnown && dw.dashboardSnapshotCount == n && dw.dashboardSnapshotMounted == snapshotMounted {
		return
	}
	dw.dashboardSnapshotCount = n
	dw.dashboardSnapshotMounted = snapshotMounted
	dw.dashboardSnapshotKnown = true
	dw.refreshDashboard()
}

// dashboardBackupChips is the under-name plaques on the Backups row:
// snapshot count plus the MTP flash size (backup weight), including "0 B"
// when the agent has not yet reported a real size.
func (dw *DiskWidget) dashboardBackupChips(size string) []string {
	var chips []string
	if dw.dashboardSnapshotKnown {
		if dw.dashboardSnapshotCount == 1 {
			chips = append(chips, "1 snapshot")
		} else {
			chips = append(chips, fmt.Sprintf("%d snapshots", dw.dashboardSnapshotCount))
		}
	}
	if strings.TrimSpace(size) != "" {
		chips = append(chips, strings.TrimSpace(size))
	}
	return chips
}

// dashboardNetworkTitle splits "Network Card (RNDIS)" into the row name
// and an under-name chip -- same plaque Backups uses for size -- so RNDIS
// isn't sitting in the title. The old list (deviceRowText) still shows
// the combined string.
func dashboardNetworkTitle(title string) (name, chip string) {
	chip = "RNDIS"
	name = strings.TrimSpace(title)
	name = strings.TrimSpace(strings.TrimSuffix(name, "(RNDIS)"))
	name = strings.TrimSpace(strings.TrimSuffix(name, "RNDIS"))
	name = strings.Trim(name, "()- ")
	if name == "" {
		return title, chip
	}
	return name, chip
}

// newDashboardConnectSlot is Storage/Backups' trailing mount-or-disconnect
// control: a "disconnect" button once mounted (the mounted state itself is
// read off the row's own lime name/icon color, see
// newDeviceDashboardRowLeftSized), or a lime "mount it" button otherwise.
// While IsMounting the mount button stays in place but darkens -- the same
// busy fill "Mount New ISO" uses -- instead of disappearing.
func (dw *DiskWidget) newDashboardConnectSlot(idx int, drive DriveItem, hover func(bool)) fyne.CanvasObject {
	locked := dw.controlsLocked()
	if drive.USBPassthrough != nil && drive.USBPassthrough.Protected && !drive.IsMounted {
		locked = true
	}
	if drive.IsMounted {
		btn := view.NewDeviceDashboardDisconnectButton(func() {
			dw.toggleDriveMount(idx)
		}, hover)
		btn.SetDisabled(locked)
		return btn
	}
	btn := view.NewDeviceDashboardMountButton(func() {
		dw.toggleDriveMount(idx)
	}, hover, drive.IsMounting && !locked)
	btn.SetDisabled(locked)
	return btn
}

// newDriveToggle builds a HID/Network row's own on/off switch, wired to
// toggleDriveMount and to cardHover (that row's own card's onHover cell --
// see view.NewDeviceDashboardHoverCell).
func (dw *DiskWidget) newDriveToggle(idx int, drive DriveItem, cardHover func(bool)) *view.DeviceToggle {
	t := view.NewDeviceToggle(drive.IsMounted, func(bool) {
		dw.toggleDriveMount(idx)
	})
	t.OnHover = cardHover
	if drive.IsRNDIS {
		t.ActiveFill = view.DeviceDashboardAccentLime
	}
	t.SetEnabled(!dw.controlsLocked())
	return t
}

// newDashboardGamepadModePicker is a gamepad row's DirectInput/XInput
// dropdown -- same compact teal HeaderDropdown Storage uses for USB
// Stick/CD-ROM (NewDeviceDashboardModePicker), matching the header
// settings menus' own look.
func (dw *DiskWidget) newDashboardGamepadModePicker(idx int, drive DriveItem) *view.HeaderDropdown {
	picker := view.NewDeviceDashboardModePicker(
		[]string{i18n.Current.DeviceDirectInput, i18n.Current.DeviceXInput},
		gamepadModeLabel(normalizeGamepadMode(drive.GamepadMode)),
		func(s string) {
			if dw.controlsLocked() || idx >= len(dw.allDrives) {
				return
			}
			mode := gamepadModeDirectInput
			if s == i18n.Current.DeviceXInput {
				mode = gamepadModeXInput
			}
			dw.allDrives[idx].GamepadMode = mode
		},
	)
	picker.OnHover = dw.dashboardHIDHover
	picker.SetDisabled(dw.controlsLocked())
	return picker
}

// newDashboardUSBAudioModePicker is USB Audio Codec's UAC1/UAC2 dropdown --
// the same compact HeaderDropdown gamepad and Storage rows already use.
// Capture-audio rows do not get one; only the gadget codec has a mode.
func (dw *DiskWidget) newDashboardUSBAudioModePicker(idx int, drive DriveItem) *view.HeaderDropdown {
	selected := i18n.Current.AudioDeviceUAC1
	if drive.USBAudioMode == "uac2" {
		selected = i18n.Current.AudioDeviceUAC2
	}
	picker := view.NewDeviceDashboardModePicker(
		[]string{i18n.Current.AudioDeviceUAC1, i18n.Current.AudioDeviceUAC2},
		selected,
		func(s string) {
			if dw.controlsLocked() || idx < 0 || idx >= len(dw.allDrives) {
				return
			}
			mode := "uac1"
			if s == i18n.Current.AudioDeviceUAC2 {
				mode = "uac2"
			}
			dw.allDrives[idx].USBAudioMode = mode
		},
	)
	picker.OnHover = dw.dashboardAudioHover
	picker.SetDisabled(dw.controlsLocked())
	return picker
}

// newDashboardVideoRadio is Video Pipe's exclusive round selector -- tapping
// it switches capture to this device. With only one available screen the
// radio is gray and inert: there is nothing to switch to, and tapping the
// already-active one used to bounce the pipeline off and back on.
func (dw *DiskWidget) newDashboardVideoRadio(drive DriveItem) fyne.CanvasObject {
	unavailable := drive.IsVideo && drive.VideoDevice != nil && !drive.VideoDevice.Connected && !drive.IsMounted && isUSBridgeAgentOS(dw.agentOS)
	selected := dw.isPreferredVideoDrive(drive)
	disabled := dw.controlsLocked() || unavailable || dw.availableVideoDriveCount() <= 1
	onTap := func() {}
	if drive.VideoDevice != nil {
		deviceCopy := *drive.VideoDevice
		onTap = func() {
			if dw.controlsLocked() || unavailable || dw.availableVideoDriveCount() <= 1 {
				return
			}
			dw.selectVideoDevice(deviceCopy)
		}
	}
	return view.NewDeviceDashboardCaptureSelector(selected, disabled, onTap, dw.dashboardVideoHover)
}

// newDashboardAudioRadio is Audio Pipeline's exclusive round selector --
// capture devices call setPreferredAudioDevice, USB Audio Codec calls
// selectUSBAudio. Only one of those can be on at a time: tapping this
// one fills it and clears the sibling radios.
func (dw *DiskWidget) newDashboardAudioRadio(idx int, drive DriveItem) fyne.CanvasObject {
	unavailable := drive.IsAudio && drive.AudioDevice != nil && !drive.AudioDevice.Connected && !drive.IsMounted
	selected := false
	if drive.IsUSBAudio {
		selected = drive.IsMounted
	} else {
		selected = dw.isPreferredAudioDrive(drive)
	}
	disabled := dw.controlsLocked() || unavailable
	isUSB := drive.IsUSBAudio
	onTap := func() {
		if dw.controlsLocked() || unavailable {
			return
		}
		if isUSB {
			mode := "uac1"
			if idx >= 0 && idx < len(dw.allDrives) && dw.allDrives[idx].USBAudioMode != "" {
				mode = dw.allDrives[idx].USBAudioMode
			}
			dw.selectUSBAudio(mode)
			dw.requestDevicesRefresh()
		}
	}
	if drive.AudioDevice != nil {
		audioCopy := *drive.AudioDevice
		onTap = func() {
			if dw.controlsLocked() || unavailable {
				return
			}
			dw.setPreferredAudioDevice(audioCopy)
			dw.requestDevicesRefresh()
		}
	}
	return view.NewDeviceDashboardCaptureSelector(selected, disabled, onTap, dw.dashboardAudioHover)
}

// newDashboardRNDISSettingsButton is Network's gear -- tapping it opens
// the same teal styled menu the header RNDIS icon uses (auto / wifirouter
// / etherouter / etherbridge). A compact HeaderDropdown did not fit the
// half-width card next to Backups; the mouse row already uses this gear
// + menu pattern.
func (dw *DiskWidget) newDashboardRNDISSettingsButton(idx int, _ DriveItem) fyne.CanvasObject {
	var btn fyne.CanvasObject
	btn = view.NewDeviceDashboardSettingsButton(func() {
		if dw.controlsLocked() {
			return
		}
		view.ShowStyledMenuTeal(btn, dw.dashboardRNDISModeMenuItems(idx))
	}, dw.dashboardNetworkHover)
	view.DisableDashboardAction(btn, dw.controlsLocked())
	return btn
}

func (dw *DiskWidget) dashboardRNDISModeMenuItems(idx int) []view.StyledMenuItem {
	current := "auto"
	if idx >= 0 && idx < len(dw.allDrives) {
		current = normalizeRNDISMode(dw.allDrives[idx].RNDISMode)
	}
	items := make([]view.StyledMenuItem, 0, len(rndisModeOptions))
	for _, label := range rndisModeOptions {
		mode := label
		items = append(items, view.StyledMenuItem{
			Label:    label,
			Selected: mode == current,
			OnTap: func() {
				if dw.controlsLocked() || idx < 0 || idx >= len(dw.allDrives) {
					return
				}
				dw.allDrives[idx].RNDISMode = normalizeRNDISMode(mode)
			},
		})
	}
	return items
}

// newDashboardMouseSettingsButton is the Mouse half's gear -- tapping it
// opens the same teal styled menu the header mouse icon uses, with just
// the pointing-mode choices (TouchPad/Absolute, plus mobile extras).
func (dw *DiskWidget) newDashboardMouseSettingsButton(idx int, drive DriveItem) fyne.CanvasObject {
	var btn fyne.CanvasObject
	btn = view.NewDeviceDashboardSettingsButton(func() {
		if dw.controlsLocked() {
			return
		}
		view.ShowStyledMenuTeal(btn, dw.dashboardMouseModeMenuItems(idx))
	}, dw.dashboardHIDHover)
	view.DisableDashboardAction(btn, dw.controlsLocked())
	return btn
}

func (dw *DiskWidget) dashboardMouseModeMenuItems(idx int) []view.StyledMenuItem {
	current := ""
	if idx >= 0 && idx < len(dw.allDrives) {
		current = normalizeMouseMode(dw.allDrives[idx].MouseType)
	}
	items := make([]view.StyledMenuItem, 0, len(mouseConfigOptions()))
	for _, label := range mouseConfigOptions() {
		lab := label
		mode, _, _ := mouseLabelToConfig(lab)
		items = append(items, view.StyledMenuItem{
			Label:    lab,
			Selected: mode == current,
			OnTap: func() {
				dw.applyMouseModeSelection(idx, mode)
			},
		})
	}
	return items
}

// videoDashboardLatencyChip is the under-name plaque on a Video Pipe row:
// USB 3.x reads as a turquoise "Ultra Low Latency", USB 2.0 (480) as a
// gray "Medium". Anything else (no bus, USB 1.1) has no chip.
func videoDashboardLatencyChip(drive DriveItem) (text string, teal bool) {
	if drive.VideoDevice == nil {
		return "", false
	}
	switch drive.VideoDevice.Bus {
	case "usb-3.2", "usb-3.0":
		return "Ultra Low Latency", true
	case "usb-2.0":
		return "Medium", false
	default:
		return "", false
	}
}

// newDashboardVideoSettingsButton opens this capture device's config
// window -- same onVideoConfigRequested path the old list's Config button
// used (disk_widget_row.go).
func (dw *DiskWidget) newDashboardVideoSettingsButton(drive DriveItem) fyne.CanvasObject {
	if drive.VideoDevice == nil {
		return nil
	}
	deviceCopy := *drive.VideoDevice
	btn := view.NewDeviceDashboardSettingsButton(func() {
		if dw.controlsLocked() {
			return
		}
		dw.setPreferredVideoDevice(deviceCopy)
		if dw.onVideoConfigRequested != nil {
			dw.onVideoConfigRequested(deviceCopy.Path)
		}
	}, dw.dashboardVideoHover)
	btn.SetDisabled(dw.controlsLocked())
	return btn
}

// syncDashboardFooter shows Devices' footer "Disconnect All" only while
// something is actually mounted (same condition the old compact unmount
// button used). No-op until GetDashboardContainer has built the footer.
func (dw *DiskWidget) syncDashboardFooter() {
	if dw.dashboardFooterDisconnect == nil {
		return
	}
	hasMounted := false
	for _, drive := range dw.allDrives {
		if drive.IsMounted {
			hasMounted = true
			break
		}
	}
	dw.dashboardFooterDisconnect.SetEnabled(!dw.controlsLocked())
	if hasMounted {
		dw.dashboardFooterDisconnect.Show()
	} else {
		dw.dashboardFooterDisconnect.Hide()
	}
}

// toggleDriveMount mounts or unmounts exactly the one drive at index,
// reusing handleMount/handleUnmount's own real, selection-driven flow (NBD
// servers, gadget requests, confirmation dialogs -- everything the old
// list's per-row checkbox drove) by pre-selecting just that index instead
// of requiring the user to select it through a list first.
func (dw *DiskWidget) toggleDriveMount(index int) {
	if dw.controlsLocked() || index < 0 || index >= len(dw.allDrives) {
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
