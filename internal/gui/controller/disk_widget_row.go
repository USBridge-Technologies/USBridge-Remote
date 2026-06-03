package controller

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

func (dw *DiskWidget) buildDeviceCards() []fyne.CanvasObject {
	sections := dw.groupDriveIndexes()
	cards := make([]fyne.CanvasObject, 0, len(sections))

	for _, section := range sections {
		if len(section.indexes) == 0 && section.key != "storage" {
			continue
		}

		rows := make([]fyne.CanvasObject, 0, len(section.indexes))
		for _, driveIndex := range section.indexes {
			drive := dw.allDrives[driveIndex]
			id := dw.getDriveUniqueID(drive)

			rowObj, ok := dw.rowsCache[id]
			if !ok {
				rowObj = view.NewDiskRowTemplate()
				dw.rowsCache[id] = rowObj
			}
			dw.configureDriveRow(driveIndex, rowObj)
			rows = append(rows, rowObj)
		}

		fill, border, badge := sectionPalette(section.key)
		var sectionAction fyne.CanvasObject
		var sectionTrailingAction fyne.CanvasObject
		if section.key == "storage" {
			sectionAction = view.NewDeviceSectionAddButton(dw.handleAddImage)
			sectionTrailingAction = view.NewFooterIconButton(assets.QuestionIconDim, assets.QuestionIcon, fyne.NewSize(13, 13), func() {
				dw.openQuickStartDocs()
			})
		}

		card, ok := dw.cardsCache[section.key]
		if !ok {
			card = view.NewDeviceSectionCard(
				section.title,
				section.title,
				section.description,
				formatSectionCount(len(section.indexes)),
				fill,
				border,
				badge,
				rows,
				sectionAction,
				sectionTrailingAction,
			)
			dw.cardsCache[section.key] = card
		} else {
			view.UpdateDeviceSectionCard(card, rows, formatSectionCount(len(section.indexes)))
		}

		cards = append(cards, card)
	}

	return cards
}

// configureDriveRow populates an existing row template with state from allDrives[id].
// Called on the Fyne event-loop thread — all widget mutations are direct (no fyne.Do).
func (dw *DiskWidget) configureDriveRow(id int, obj fyne.CanvasObject) {
	if id < 0 || id >= len(dw.allDrives) {
		return
	}

	drive := dw.allDrives[id]
	row := view.ResolveDiskRowWidgets(obj)
	if row == nil {
		return
	}

	checkbox := row.Checkbox
	captureSelector := row.CaptureSelector
	prefixIcon := row.PrefixIcon
	nameLabel := row.NameLabel
	statusLabel := row.StatusLabel
	statusDot := row.StatusDot
	roRwBtn := row.RORWButton
	modeSelect := row.ModeSelect
	uploadBtn := row.UploadButton
	deleteBtn := row.DeleteButton
	settingsBtn := row.SettingsButton
	modeRowIconText := row.ModeIcon
	modeTitleLabel := row.ModeTitleLabel

	if checkbox == nil || prefixIcon == nil || nameLabel == nil || statusLabel == nil || statusDot == nil {
		return
	}

	videoUnavailable := drive.IsVideo && drive.VideoDevice != nil && !drive.VideoDevice.Connected && !drive.IsMounted
	audioUnavailable := drive.IsAudio && drive.AudioDevice != nil && !drive.AudioDevice.Connected && !drive.IsMounted
	controlsLocked := dw.controlsLocked()
	checked := false
	if !drive.IsVideo && !drive.IsAudio {
		dw.selectedItemsMu.RLock()
		checked = dw.selectedItems[id]
		dw.selectedItemsMu.RUnlock()
	}

	baseTextColor := design.ColorTextLight
	if drive.IsMounted {
		baseTextColor = design.ColorAccent
	}

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
		if drive.IsMounted {
			iconRes = assets.KeyboardIconActive
		} else {
			iconRes = assets.KeyboardIcon
		}
	case "mouse":
		if drive.IsMounted {
			iconRes = assets.MouseIconActive
		} else {
			iconRes = assets.MouseIcon
		}
	case "rndis":
		if drive.IsMounted {
			iconRes = assets.NetworkIconActive
		} else {
			iconRes = assets.NetworkIcon
		}
	case "gamepad":
		if drive.IsMounted {
			iconRes = assets.GamepadIconActive
		} else {
			iconRes = assets.GamepadIcon
		}
	case "video":
		if drive.IsMounted {
			iconRes = assets.CameraIconActive
		} else {
			iconRes = assets.CameraIcon
		}
	case "audio":
		if drive.IsMounted {
			iconRes = assets.AudioIconActive
		} else {
			iconRes = assets.AudioIcon
		}
	case "usbaudio":
		if drive.IsMounted {
			iconRes = assets.AudioIconActive
		} else {
			iconRes = assets.AudioIcon
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

	var statusColor color.Color
	switch {
	case drive.IsMounting:
		statusColor = color.NRGBA{R: 0xc7, G: 0x9b, B: 0x52, A: 0xff}
	case drive.IsMounted:
		statusColor = design.ColorAccent
	case videoUnavailable:
		statusColor = design.ColorBorder
	default:
		statusColor = color.NRGBA{R: 0x86, G: 0x86, B: 0x86, A: 0xff}
	}

	nameText := dw.deviceRowText(drive)
	if drive.Source == "mouse" {
		switch normalizeMouseMode(drive.MouseType) {
		case mouseModeTouchScreen:
			nameText = fmt.Sprintf("TCH %s", nameText)
		case mouseModeAbsolute:
			nameText = fmt.Sprintf("ABS %s", nameText)
		default:
			nameText = fmt.Sprintf("PTR %s", nameText)
		}
	}

	overlayCapable := false
	if (drive.Source == "local" || drive.Source == "user") && drive.DiskInfo != nil {
		overlayCapable = service.IsOverlayCapableExtension(strings.ToLower(filepath.Ext(drive.DiskInfo.Path)))
	} else if drive.Source == "api" && drive.LocalDrive != nil && drive.LocalDrive.SourceType != "mtp" {
		overlayCapable = service.IsOverlayCapableExtension(strings.ToLower(filepath.Ext(drive.LocalDrive.Name)))
	}

	// All UI mutations happen directly — configureDriveRow is always called on the
	// Fyne event-loop thread (via DevicesListView.Refresh → buildDeviceCards).
	// A nested fyne.Do here would queue the updates to the next event-loop tick,
	// causing a one-frame flash of blank/default values.

	prefixIcon.Resource = iconRes
	prefixIcon.SetMinSize(fyne.NewSize(18, 18))
	if drive.Source == "api" && drive.LocalDrive != nil && drive.LocalDrive.SourceType == "mtp" {
		prefixIcon.SetMinSize(fyne.NewSize(16, 16))
	}
	prefixIcon.Show()
	prefixIcon.Refresh()

	nameLabel.SetColor(baseTextColor)
	nameLabel.SetText(nameText)
	nameLabel.Show()

	statusDot.FillColor = statusColor
	statusDot.Refresh()
	statusLabel.Hide()

	modeRowIconText.Hide()
	modeTitleLabel.Hide()

	if drive.Source == "mouse" || drive.Source == "rndis" || drive.Source == "gamepad" || drive.Source == "usbaudio" {
		modeSelect.Show()
		modeSelect.SetDisabled(controlsLocked)
		switch drive.Source {
		case "rndis":
			modeSelect.SetOptions(rndisModeOptions)
			modeSelect.SetSelected(normalizeRNDISMode(drive.RNDISMode))
		case "gamepad":
			modeSelect.SetOptions([]string{i18n.Current.DeviceDirectInput, i18n.Current.DeviceXInput})
			modeSelect.SetSelected(gamepadModeLabel(normalizeGamepadMode(drive.GamepadMode)))
		case "usbaudio":
			modeSelect.SetOptions([]string{i18n.Current.AudioDeviceUAC1, i18n.Current.AudioDeviceUAC2})
			if drive.USBAudioMode == "uac2" {
				modeSelect.SetSelected(i18n.Current.AudioDeviceUAC2)
			} else {
				modeSelect.SetSelected(i18n.Current.AudioDeviceUAC1)
			}
		default: // mouse
			mode := normalizeMouseMode(drive.MouseType)
			if mode == mouseModeAbsolute {
				modeSelect.SetSelected(i18n.Current.DeviceAbsolute)
			} else {
				modeSelect.SetSelected(i18n.Current.DeviceTouchPad)
			}
			modeSelect.SetOptions([]string{i18n.Current.DeviceTouchPad, i18n.Current.DeviceAbsolute})
		}
	} else {
		modeSelect.Hide()
	}

	if drive.IsVideo {
		checkbox.Hide()
		if captureSelector != nil {
			captureSelector.Show()
			captureSelector.SetSelected(dw.isPreferredVideoDrive(drive))
			captureSelector.SetDisabled(controlsLocked || videoUnavailable)
		}
		settingsBtn.Show()
		if controlsLocked || videoUnavailable {
			settingsBtn.Disable()
		} else {
			settingsBtn.Enable()
		}
	} else if drive.IsAudio {
		checkbox.Hide()
		if captureSelector != nil {
			captureSelector.Show()
			captureSelector.SetSelected(dw.isPreferredAudioDrive(drive))
			captureSelector.SetDisabled(controlsLocked || audioUnavailable)
		}
		settingsBtn.Hide()
	} else if drive.IsUSBAudio {
		checkbox.Hide()
		if captureSelector != nil {
			captureSelector.Show()
			captureSelector.SetSelected(drive.IsMounted)
			captureSelector.SetDisabled(controlsLocked)
		}
		settingsBtn.Hide()
	} else {
		if captureSelector != nil {
			captureSelector.Hide()
		}
		checkbox.SetChecked(checked)
		checkbox.Show()
		if controlsLocked || drive.IsMounting || videoUnavailable {
			checkbox.Disable()
		} else {
			checkbox.Enable()
		}
		settingsBtn.Hide()
	}

	if drive.Source == "user" && drive.DiskInfo != nil && !drive.IsMounting {
		uploadBtn.Show()
		if drive.IsUploading || controlsLocked {
			uploadBtn.SetIcons(assets.UploadIconMuted, assets.UploadIconMuted, assets.UploadIconMuted)
			if drive.IsUploading {
				uploadBtn.SetText(fmt.Sprintf("%.0f%%", drive.UploadProgress))
			} else {
				uploadBtn.SetText("")
			}
			uploadBtn.SetDisabled(true)
		} else {
			uploadBtn.SetIcons(assets.UploadIcon, assets.UploadIcon, assets.UploadIconMuted)
			uploadBtn.SetText("")
			uploadBtn.SetDisabled(false)
		}
	} else {
		uploadBtn.Hide()
	}

	shouldShowDelete := false
	if !drive.IsMounting {
		if drive.Source == "user" {
			shouldShowDelete = true
		} else if drive.Source == "api" || drive.Source == "local" {
			isBackupFlash := drive.LocalDrive != nil && drive.LocalDrive.Name == "data" && drive.LocalDrive.SourceType == "mtp"
			if !isBackupFlash {
				shouldShowDelete = true
			}
		}
	}
	if shouldShowDelete {
		deleteBtn.Show()
		deleteBtn.SetDisabled(controlsLocked)
	} else {
		deleteBtn.Hide()
	}

	if overlayCapable && !drive.IsMounting {
		roRwBtn.Show()
		if drive.ReadOnly {
			roRwBtn.SetText("RO")
		} else {
			roRwBtn.SetText("RW")
		}
		if controlsLocked {
			roRwBtn.Disable()
		} else {
			roRwBtn.Enable()
		}
	} else {
		roRwBtn.Hide()
	}

	// Callbacks
	if drive.IsVideo && drive.VideoDevice != nil {
		deviceCopy := *drive.VideoDevice
		if captureSelector != nil {
			captureSelector.SetOnTapped(func() {
				if dw.controlsLocked() || videoUnavailable {
					return
				}
				dw.selectVideoDevice(deviceCopy)
			})
		}
		settingsBtn.SetOnTapped(func() {
			dw.setPreferredVideoDevice(deviceCopy)
			if dw.onVideoConfigRequested != nil {
				dw.onVideoConfigRequested(deviceCopy.Path)
			}
			dw.requestDevicesRefresh()
		})
	} else if drive.IsAudio && drive.AudioDevice != nil {
		deviceCopy := *drive.AudioDevice
		if captureSelector != nil {
			captureSelector.SetOnTapped(func() {
				if dw.controlsLocked() || audioUnavailable {
					return
				}
				dw.setPreferredAudioDevice(deviceCopy)
				dw.requestDevicesRefresh()
			})
		}
	} else if drive.IsUSBAudio {
		rowID := id
		if captureSelector != nil {
			captureSelector.SetOnTapped(func() {
				if dw.controlsLocked() {
					return
				}
				mode := "uac1"
				if rowID < len(dw.allDrives) && dw.allDrives[rowID].USBAudioMode != "" {
					mode = dw.allDrives[rowID].USBAudioMode
				}
				dw.selectUSBAudio(mode)
				dw.requestDevicesRefresh()
			})
		}
	} else {
		if captureSelector != nil {
			captureSelector.SetOnTapped(nil)
		}
		checkbox.OnChanged = func(checked bool) {
			if dw.controlsLocked() || drive.IsMounting || videoUnavailable {
				return
			}
			if checked {
				if dw.countSelectedGadgetItems() >= MaxDevicesToMount {
					checkbox.SetChecked(false)
					if dw.window != nil {
						dialog.ShowInformation(i18n.Current.Information, i18n.Current.MaxDevicesReached, dw.window)
					}
					return
				}
			}
			dw.selectedItemsMu.Lock()
			dw.selectedItems[id] = checked
			dw.selectedItemsMu.Unlock()
			dw.updateButtons()
		}
		settingsBtn.SetOnTapped(nil)
	}

	if drive.Source == "rndis" {
		rowID := id
		modeSelect.OnSelected = func(s string) {
			if dw.controlsLocked() || rowID >= len(dw.allDrives) {
				return
			}
			dw.allDrives[rowID].RNDISMode = normalizeRNDISMode(s)
		}
	} else if drive.Source == "gamepad" {
		rowID := id
		modeSelect.OnSelected = func(s string) {
			if dw.controlsLocked() || rowID >= len(dw.allDrives) {
				return
			}
			mode := gamepadModeDirectInput
			if s == i18n.Current.DeviceXInput {
				mode = gamepadModeXInput
			}
			dw.allDrives[rowID].GamepadMode = mode
		}
	} else if drive.Source == "mouse" {
		rowID := id
		modeSelect.OnSelected = func(s string) {
			if dw.controlsLocked() || rowID >= len(dw.allDrives) {
				return
			}
			newMode := mouseModeTouchPad
			if s == i18n.Current.DeviceAbsolute {
				newMode = mouseModeAbsolute
			}
			dw.applyMouseModeSelection(rowID, newMode)
		}
	} else if drive.Source == "usbaudio" {
		rowID := id
		modeSelect.OnSelected = func(s string) {
			if dw.controlsLocked() || rowID >= len(dw.allDrives) {
				return
			}
			mode := "uac1"
			if s == i18n.Current.AudioDeviceUAC2 {
				mode = "uac2"
			}
			dw.allDrives[rowID].USBAudioMode = mode
		}
	}

	if drive.Source == "user" {
		rowID := id
		deleteBtn.SetOnTapped(func() {
			if !dw.controlsLocked() {
				dw.removeUserImage(rowID)
			}
		})
		uploadBtn.SetOnTapped(func() {
			if !dw.controlsLocked() {
				dw.handleUploadImage(rowID)
			}
		})
	} else {
		deleteBtn.SetOnTapped(nil)
		uploadBtn.SetOnTapped(nil)
		if !drive.IsMounting && (drive.Source == "api" || drive.Source == "local") {
			isBackupFlash := drive.LocalDrive != nil && drive.LocalDrive.Name == "data" && drive.LocalDrive.SourceType == "mtp"
			if !isBackupFlash {
				rowID := id
				var filename string
				if drive.LocalDrive != nil {
					filename = drive.LocalDrive.Name
				} else if drive.DiskInfo != nil {
					filename = drive.DiskInfo.Name
				} else {
					filename = drive.Name
				}
				deleteBtn.SetOnTapped(func() {
					if !dw.controlsLocked() {
						dw.handleDeleteImageFromDevice(rowID, filename)
					}
				})
			}
		}
	}

	if overlayCapable && !drive.IsMounting {
		rowID := id
		roRwBtn.OnTapped = func() {
			if !dw.controlsLocked() && rowID < len(dw.allDrives) {
				dw.allDrives[rowID].ReadOnly = !dw.allDrives[rowID].ReadOnly
				dw.requestDevicesRefresh()
			}
		}
	} else {
		roRwBtn.OnTapped = nil
	}
}

func (dw *DiskWidget) deviceRowText(drive DriveItem) string {
	if drive.IsVideo {
		return dw.captureDeviceTitle(drive)
	}
	if drive.Source == "user" || drive.Source == "local" {
		if drive.DiskInfo != nil {
			title := strings.TrimSpace(drive.DiskInfo.Name)
			if title == "" && strings.TrimSpace(drive.DiskInfo.Path) != "" {
				title = filepath.Base(filepath.Clean(drive.DiskInfo.Path))
			}
			if title == "" {
				title = drive.Name
			}
			return title
		}
	}
	if drive.Source == "api" && drive.LocalDrive != nil {
		name := strings.TrimSpace(dw.localizedAPIDriveName(drive.LocalDrive))
		if name == "" {
			name = drive.Name
		}
		return name
	}
	return drive.Name
}

func (dw *DiskWidget) localizedAPIDriveName(drive *models.LocalDrive) string {
	if drive == nil {
		return ""
	}
	if drive.Name == "data" && drive.SourceType == "mtp" {
		return i18n.Current.BackupFlashName
	}
	return drive.Name
}

func (dw *DiskWidget) captureDeviceTitle(drive DriveItem) string {
	if drive.VideoDevice == nil {
		return i18n.Current.CaptureDevice
	}
	name := strings.TrimSpace(firstNonEmpty(drive.VideoDevice.Name, drive.VideoDevice.Description))
	if name == "" {
		name = i18n.Current.CaptureDevice
	}
	if len(dw.videoDevices) > 1 {
		for index, device := range dw.videoDevices {
			if device.Path == drive.VideoDevice.Path {
				name = fmt.Sprintf("%s (%d)", name, index+1)
				break
			}
		}
	}
	if busLabel := formatVideoBusLabel(drive.VideoDevice.Bus); busLabel != "" {
		return fmt.Sprintf("%s [%s]", name, busLabel)
	}
	return name
}

func formatVideoBusLabel(bus string) string {
	switch bus {
	case "usb-3.2":
		return "10G"
	case "usb-3.0":
		return "5G"
	case "usb-2.0":
		return "480M"
	case "usb-1.1":
		return "USB 1.1"
	case "usb":
		return "USB"
	default:
		return ""
	}
}
