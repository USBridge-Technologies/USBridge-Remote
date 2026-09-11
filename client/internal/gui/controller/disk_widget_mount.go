package controller

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
	"usbridge-client/internal/usbpass"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// startDevicesWithRetry runs StartDevicesBatchWithMerge with 3 attempts and a 3s pause between them.
func (dw *DiskWidget) startDevicesWithRetry(batchRequest models.DeviceStartBatchRequest, merge bool) (*models.APIResponse, error) {
	const maxAttempts = 3
	const retryDelay = 3 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := dw.usbClient.StartDevicesBatchWithMerge(batchRequest, merge)
		if err == nil {
			if attempt > 1 {
				logrus.Infof("✅ [MOUNT-API-RETRY] Attempt %d/%d succeeded", attempt, maxAttempts)
			}
			return resp, nil
		}
		lastErr = err
		errStr := err.Error()
		isRetryable := strings.Contains(errStr, "EOF") || strings.Contains(errStr, "NBD") ||
			strings.Contains(errStr, "Failed to connect NBD") || strings.Contains(errStr, "connection reset") ||
			strings.Contains(errStr, "connection refused")
		if !isRetryable || attempt == maxAttempts {
			return nil, err
		}
		logrus.Warnf("⚠️ [MOUNT-API-RETRY] Attempt %d/%d: %v, retrying in 3s...", attempt, maxAttempts, err)
		dw.updateStatusAsync(fmt.Sprintf("Retrying connection %d/%d in 3s...", attempt, maxAttempts))
		time.Sleep(retryDelay)
	}
	return nil, lastErr
}

// beginOperation locks the UI before starting a mount/unmount operation.
// Called from the Fyne thread.
func (dw *DiskWidget) beginOperation() {
	dw.userOperationInFlight.Store(true)
	dw.setButtonsEnabled(false)
	if dw.dashboardBusySpinner != nil {
		dw.dashboardBusySpinner.Start()
	}
	dw.refreshDashboard()
	if dw.dashboardContainer != nil {
		dw.dashboardContainer.Refresh()
	}
}

// endOperation finishes a mount/unmount operation.
// Makes HTTP requests to fetch fresh data, then in a SINGLE fyne.Do:
//   - updates device data
//   - resets all IsMounting flags
//   - unconditionally resets userOperationInFlight and apiMountInProgress
//   - updates status and buttons
//
// All changes happen in one fyne.Do — no other async updater can slip in between
// clearing the flags and updateButtons(), which eliminates permanent UI lockups.
// Safe to call from any goroutine; used as a defer in operation goroutines.
func (dw *DiskWidget) endOperation() {
	var newMounted []*models.DeviceInfo
	var newLocalDrives []*models.LocalDrive
	var newAgentOS string
	var newPassSessions []string

	if dw.usbClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if deviceInfo, err := dw.usbClient.GetDeviceInfoWithContext(ctx); err == nil {
			newMounted = make([]*models.DeviceInfo, len(deviceInfo.Devices))
			for i := range deviceInfo.Devices {
				newMounted[i] = &deviceInfo.Devices[i]
			}
			newAgentOS = deviceInfo.AgentOS
		} else {
			logrus.Errorf("endOperation: GetDeviceInfo: %v", err)
		}

		if localDrives, err := dw.usbClient.GetLocalDrives(); err == nil {
			newLocalDrives = make([]*models.LocalDrive, len(localDrives.Drives))
			for i := range localDrives.Drives {
				newLocalDrives[i] = &localDrives.Drives[i]
			}
		} else {
			logrus.Errorf("endOperation: GetLocalDrives: %v", err)
		}

		if st, err := dw.usbClient.GetUSBPassthroughStatus(); err == nil && st != nil {
			newPassSessions = append([]string(nil), st.Sessions...)
		}
	}

	// All changes happen in one fyne.Do — atomic from the event loop's point of view.
	// This rules out a race where a late server response (apiMountInProgress=true) in
	// another fyne.Do could land between clearing the flags and updateButtons().
	fyne.Do(func() {
		if newMounted != nil {
			dw.mountedDevices = newMounted
			dw.agentOS = newAgentOS
		}
		if newLocalDrives != nil {
			dw.localDrives = newLocalDrives
		}
		dw.usbPassSessions = newPassSessions
		// Reset all mounting animations
		for i := range dw.allDrives {
			dw.allDrives[i].IsMounting = false
		}
		// Unconditionally reset the flags BEFORE calling updateDevicesStatus/updateButtons,
		// so controlsLocked() returns false and buttons are guaranteed to re-enable.
		dw.userOperationInFlight.Store(false)
		dw.apiMountInProgress.Store(false)
		if dw.dashboardBusySpinner != nil {
			dw.dashboardBusySpinner.Stop()
		}
		if dw.dashboardContainer != nil {
			dw.dashboardContainer.Refresh()
		}
		// Rebuild status and list from fresh data
		dw.updateDevicesStatus() // updates IsMounted, calls updateButtons
		dw.lastDrivesTraceSig = ""
		dw.requestDevicesRefresh()
	})
	// Refresh the main window header status icons (keyboard/mouse/gamepad/rndis).
	if dw.updateStatus != nil {
		go dw.updateStatus()
	}
}

// handleMount handles the Connect button press.
func (dw *DiskWidget) handleMount() {
	logrus.Infof("📍 [MOUNT] handleMount called, GOOS: %s", runtime.GOOS)

	if dw.controlsLocked() {
		return
	}
	if dw.usbClient == nil {
		if dw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		return
	}

	// Collect selected unmounted non-video/non-audio devices
	var selectedDrives []DriveItem
	dw.selectedItemsMu.RLock()
	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) {
			d := dw.allDrives[id]
			if !d.IsMounted && !d.IsVideo && !d.IsAudio {
				selectedDrives = append(selectedDrives, d)
			}
		}
	}
	dw.selectedItemsMu.RUnlock()

	if len(selectedDrives) == 0 {
		if dw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.SelectDevicesToMount), dw.window)
		}
		return
	}

	mountedGadgetCount := 0
	hidMountedCount := 0
	xinputMountedCount := 0
	for _, d := range dw.allDrives {
		if d.IsMounted && !d.IsVideo {
			mountedGadgetCount++
		}
		if d.IsMounted && (d.IsKeyboard || d.IsMouse) {
			hidMountedCount++
		}
		if d.IsMounted && d.IsGamepad && normalizeGamepadMode(d.GamepadMode) == gamepadModeXInput {
			xinputMountedCount++
		}
	}

	hasXInputSelected := false
	hidSelectedCount := 0
	xinputSelectedCount := 0
	for _, d := range selectedDrives {
		if d.IsGamepad && normalizeGamepadMode(d.GamepadMode) == gamepadModeXInput {
			hasXInputSelected = true
			xinputSelectedCount++
		}
		if d.IsKeyboard || d.IsMouse {
			hidSelectedCount++
		}
	}
	dropHIDForGamepad := hasXInputSelected && (hidSelectedCount > 0 || hidMountedCount > 0)
	dropGamepadForHID := hidSelectedCount > 0 && xinputMountedCount > 0 && !dropHIDForGamepad

	effectiveMounted := mountedGadgetCount
	effectiveAdding := len(selectedDrives)
	if dropHIDForGamepad {
		effectiveMounted -= hidMountedCount
		effectiveAdding -= hidSelectedCount
	}
	if dropGamepadForHID {
		effectiveMounted -= xinputMountedCount
		effectiveAdding -= xinputSelectedCount
	}
	if effectiveMounted+effectiveAdding > MaxDevicesToMount {
		if dw.window != nil {
			view.ShowInfoDialog(i18n.Current.Information, i18n.Current.MaxDevicesReached, dw.window)
		}
		return
	}

	logrus.Infof("📁 [MOUNT] mounted: %d, adding: %d", mountedGadgetCount, len(selectedDrives))
	for _, d := range selectedDrives {
		bus := ""
		if d.USBPassthrough != nil {
			bus = d.USBPassthrough.BusID
		}
		logrus.Infof("📁 [MOUNT] selected name=%q source=%q usbpass=%v bus=%q",
			d.Name, d.Source, d.IsUSBPassthrough, bus)
	}

	var pass []DriveItem
	var rest []DriveItem
	for _, d := range selectedDrives {
		if d.IsUSBPassthrough {
			pass = append(pass, d)
		} else {
			rest = append(rest, d)
		}
	}
	if len(pass) > 0 {
		dw.mountUSBPassthrough(pass)
	}
	if len(rest) == 0 {
		return
	}
	selectedDrives = rest

	// An XInput gamepad is incompatible with keyboard/mouse in the same
	// composite device: Windows won't initialize the remaining HID
	// interfaces under the Xbox VID/PID. Offer to drop the conflicting
	// side and continue, using the same bottom toast ISO delete uses.
	if dropHIDForGamepad {
		if dw.window == nil {
			return
		}
		selectedCopy := append([]DriveItem(nil), selectedDrives...)
		view.ShowConfirmToast(i18n.Current.GamepadDisconnectHIDConfirm, func(ok bool) {
			if !ok {
				dw.requestDevicesRefresh()
				return
			}
			dw.mountReplacing(selectedCopy, conflictsWithXInputGamepad)
		}, dw.window)
		return
	}
	if dropGamepadForHID {
		if dw.window == nil {
			return
		}
		selectedCopy := append([]DriveItem(nil), selectedDrives...)
		view.ShowConfirmToast(i18n.Current.HIDDisconnectGamepadConfirm, func(ok bool) {
			if !ok {
				dw.requestDevicesRefresh()
				return
			}
			dw.mountReplacing(selectedCopy, func(d DriveItem) bool {
				return d.IsGamepad
			})
		}, dw.window)
		return
	}

	// The agent allows only one mtp:// source. A mounted snapshot occupies
	// that slot, so adding the backup flash with merge=true fails. Offer to
	// unmount the snapshot and continue, same Yes/No toast as the gamepad.
	if selectedIncludesBackupFlash(selectedDrives) && dw.hasConflictingSnapshotMTP() {
		logrus.Infof("📁 [MOUNT] backup flash requested while a snapshot MTP is mounted")
		if dw.window == nil {
			return
		}
		selectedCopy := append([]DriveItem(nil), selectedDrives...)
		view.ShowConfirmToast(i18n.Current.BackupFlashDisconnectSnapshotConfirm, func(ok bool) {
			if !ok {
				dw.requestDevicesRefresh()
				return
			}
			dw.mountReplacingGadget(selectedCopy, func(DriveItem) bool { return false }, false)
		}, dw.window)
		return
	}

	// Progress dialog for files from Google Drive
	var progressDialog dialog.Dialog
	for _, d := range selectedDrives {
		if (d.Source == "local" || d.Source == "user") && d.DiskInfo != nil &&
			strings.Contains(d.DiskInfo.Path, "com.google.android.apps.docs.storage") {
			pb := widget.NewProgressBarInfinite()
			content := container.NewVBox(
				widget.NewLabel(i18n.Current.LoadingFromCloud), widget.NewLabel(""),
				widget.NewLabel(i18n.Current.CloudFilesDetected),
				widget.NewLabel(i18n.Current.AndroidBuffering),
				widget.NewLabel(i18n.Current.MayTake30Seconds),
				widget.NewLabel(""), pb, widget.NewLabel(""),
				widget.NewLabel(i18n.Current.PleaseWait),
			)
			progressDialog = dialog.NewCustomWithoutButtons(i18n.Current.PreparingToMount, content, dw.window)
			progressDialog.Show()
			break
		}
	}

	dw.markSelectedDrivesMounting()
	dw.beginOperation()

	go func() {
		defer func() {
			if progressDialog != nil {
				fyne.Do(progressDialog.Hide)
			}
			dw.endOperation()
		}()

		// Build requests, start NBD servers
		var deviceRequests []models.DeviceStartRequest
		startedMouseMode := ""

		for _, sel := range selectedDrives {
			req, mouseMode, err := dw.buildMountRequest(sel)
			if err != nil {
				dw.showErrorAsync(fmt.Errorf("error preparing %s: %v", sel.Name, err))
				return
			}
			if mouseMode != "" {
				startedMouseMode = mouseMode
			}
			if req != nil {
				deviceRequests = append(deviceRequests, *req)
			}
		}

		if len(deviceRequests) == 0 {
			dw.showErrorAsync(fmt.Errorf("failed to prepare devices for mounting"))
			return
		}

		// Determine NBD export names for the mounting animation
		mountingExportNames := dw.nbdExportNamesForRequests(deviceRequests)

		// Wait for the NBD servers to be ready
		if err := dw.waitForNBDServers(30 * time.Second); err != nil {
			dw.showErrorAsync(err)
			return
		}

		// Show mounting animation and clear selection
		fyne.Do(func() {
			dw.setMountingStateByExportNames(mountingExportNames, true)
			dw.setAPIMountInProgress(true)
			dw.selectedItemsMu.Lock()
			dw.selectedItems = make(map[int]bool)
			dw.selectedItemsMu.Unlock()
			dw.requestDevicesRefresh()
		})

		// Call the API
		logrus.Infof("🚀 [MOUNT-API] Starting %d devices (Merge)", len(deviceRequests))
		for i, req := range deviceRequests {
			logrus.Infof("   📤 [MOUNT-API] [%d] device=%s server=%s port=%d export=%s ro=%v",
				i+1, req.Device, req.Server, req.Port, req.ExportName, req.ReadOnly)
		}
		dw.updateStatusAsync("Starting devices...")
		if resp, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, models.DeviceStartBatchRequest(deviceRequests), true); err != nil {
			logrus.Errorf("❌ [MOUNT-API] Error: %v", err)
			dw.showErrorAsync(fmt.Errorf("error starting devices: %v", err))
			return
		} else {
			logrus.Infof("✅ [MOUNT-API] Success=%v Message=%s", resp.Success, resp.Message)
		}

		// Save the mouse mode
		if dw.onMouseTypeChanged != nil && startedMouseMode != "" {
			dw.preferredMouseMode = startedMouseMode
			dw.onMouseTypeChanged(startedMouseMode)
		}

		logrus.Infof("✅ [MOUNT] Mounting complete, waiting for endOperation()")
	}()
}

func isBackupFlashDrive(d DriveItem) bool {
	return isDashboardBackupDrive(d)
}

func selectedIncludesBackupFlash(drives []DriveItem) bool {
	for _, d := range drives {
		if isBackupFlashDrive(d) {
			return true
		}
	}
	return false
}

func (dw *DiskWidget) hasMountedSnapshotMTP() bool {
	for _, device := range dw.mountedDevices {
		if device == nil || device.Status != "connected" {
			continue
		}
		if !IsMTPGadget(device.Type, device.Device) {
			continue
		}
		if device.Name == backupFlashMTPName {
			continue
		}
		return true
	}
	return false
}

// hasConflictingSnapshotMTP is the Devices-tab check: the snapshot gadget
// often does not show up as a DriveItem, and GetDeviceInfo may omit it, so
// we also trust BackupWidget's last GetSnapshots and a live lookup.
func (dw *DiskWidget) hasConflictingSnapshotMTP() bool {
	if dw.dashboardSnapshotMounted {
		return true
	}
	if dw.hasMountedSnapshotMTP() {
		return true
	}
	if dw.usbClient == nil {
		return false
	}
	resp, err := dw.usbClient.GetSnapshots()
	if err != nil || resp == nil {
		logrus.Debugf("📁 [MOUNT] GetSnapshots for MTP conflict: %v", err)
		return false
	}
	for i := range resp.Snapshots {
		if resp.Snapshots[i].Connected {
			dw.dashboardSnapshotMounted = true
			return true
		}
	}
	return false
}

// conflictsWithXInputGamepad is the agent's exclusive-USB rule: an XInput
// gamepad cannot share the gadget with keyboard, mouse, RNDIS, or drives.
func conflictsWithXInputGamepad(d DriveItem) bool {
	switch {
	case d.IsKeyboard, d.IsMouse, d.IsRNDIS:
		return true
	case d.IsGamepad, d.IsVideo, d.IsAudio, d.IsUSBAudio:
		return false
	default:
		return true
	}
}

// mountReplacing continues a mount after the user agreed to drop
// conflicting gadgets (keyboard/mouse vs XInput gamepad). The agent
// validates exclusive XInput against the currently bound gadget, so a
// single Full Replace while keyboard/mouse are still up returns 400.
// Stop the current USB gadget first, wait for UDC to release, then
// start the replacement set without the dropped devices.
func (dw *DiskWidget) mountReplacing(selectedDrives []DriveItem, drop func(DriveItem) bool) {
	dw.mountReplacingGadget(selectedDrives, drop, true)
}

func (dw *DiskWidget) mountReplacingGadget(selectedDrives []DriveItem, drop func(DriveItem) bool, stopFirst bool) {
	var filtered []DriveItem
	for _, d := range selectedDrives {
		if drop(d) {
			continue
		}
		filtered = append(filtered, d)
	}
	if len(filtered) == 0 {
		return
	}

	var keepMounted []DriveItem
	for _, d := range dw.allDrives {
		if d.IsMounted && !d.IsVideo && !d.IsAudio && !drop(d) {
			keepMounted = append(keepMounted, d)
		}
	}

	dw.selectedItemsMu.Lock()
	for i, d := range dw.allDrives {
		if drop(d) {
			delete(dw.selectedItems, i)
		}
	}
	dw.selectedItemsMu.Unlock()

	dw.markSelectedDrivesMounting()
	dw.beginOperation()

	go func() {
		defer dw.endOperation()

		var deviceRequests []models.DeviceStartRequest
		startedMouseMode := ""

		for _, d := range keepMounted {
			req, err := dw.buildDeviceRequestForDrive(d, true)
			if err != nil || req == nil {
				logrus.Warnf("⚠️ [MOUNT-XINPUT] Skip keep %s: %v", d.Name, err)
				continue
			}
			deviceRequests = append(deviceRequests, *req)
		}

		for _, sel := range filtered {
			req, mouseMode, err := dw.buildMountRequest(sel)
			if err != nil {
				dw.showErrorAsync(fmt.Errorf("error preparing %s: %v", sel.Name, err))
				return
			}
			if mouseMode != "" {
				startedMouseMode = mouseMode
			}
			if req != nil {
				deviceRequests = append(deviceRequests, *req)
			}
		}

		if len(deviceRequests) == 0 {
			dw.showErrorAsync(fmt.Errorf("failed to prepare devices for mounting"))
			return
		}

		mountingExportNames := dw.nbdExportNamesForRequests(deviceRequests)
		if err := dw.waitForNBDServers(30 * time.Second); err != nil {
			dw.showErrorAsync(err)
			return
		}

		fyne.Do(func() {
			dw.setMountingStateByExportNames(mountingExportNames, true)
			dw.setAPIMountInProgress(true)
			dw.selectedItemsMu.Lock()
			dw.selectedItems = make(map[int]bool)
			dw.selectedItemsMu.Unlock()
			dw.requestDevicesRefresh()
		})

		if stopFirst {
			logrus.Infof("🛑 [MOUNT-SWAP] Stopping current USB gadgets before replacement")
			if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, nil, false); err != nil {
				logrus.Errorf("❌ [MOUNT-SWAP] Stop before replace: %v", err)
				dw.showErrorAsync(fmt.Errorf("error disconnecting devices: %v", err))
				return
			}
			time.Sleep(gadgetRebuildDelay)
		}

		logrus.Infof("🚀 [MOUNT-SWAP] Starting %d devices (Full Replace, stopFirst=%v)", len(deviceRequests), stopFirst)
		dw.updateStatusAsync("Starting devices...")
		if resp, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, models.DeviceStartBatchRequest(deviceRequests), false); err != nil {
			logrus.Errorf("❌ [MOUNT-SWAP] Error: %v", err)
			dw.showErrorAsync(fmt.Errorf("error starting devices: %v", err))
			return
		} else {
			logrus.Infof("✅ [MOUNT-SWAP] Success=%v Message=%s", resp.Success, resp.Message)
		}

		if dw.onMouseTypeChanged != nil && startedMouseMode != "" {
			dw.preferredMouseMode = startedMouseMode
			dw.onMouseTypeChanged(startedMouseMode)
		}
	}()
}

// buildMountRequest builds a DeviceStartRequest for the device being mounted.
// Starts an NBD server for NBD devices.
// Returns (request, mouseType, error).
func (dw *DiskWidget) buildMountRequest(sel DriveItem) (*models.DeviceStartRequest, string, error) {
	switch sel.Source {
	case "keyboard":
		req := newKeyboardStartRequest()
		return &req, "", nil
	case "mouse":
		mouseType := normalizeMouseMode(sel.MouseType)
		dispIdx, dispCnt := dw.GetDisplayConfig()
		req := newMouseStartRequestWithDisplay(mouseType, dispIdx, dispCnt)
		return &req, mouseType, nil
	case "rndis":
		rndisMode := normalizeRNDISMode(sel.RNDISMode)
		req := newRNDISStartRequest(rndisMode)
		return &req, "", nil
	case "gamepad":
		req := newGamepadStartRequest(sel.GamepadMode, sel.GamepadVendorID, sel.GamepadProductID)
		return &req, "", nil
	case "usbaudio":
		mode := sel.USBAudioMode
		if mode == "" {
			mode = "uac1"
		}
		req := models.DeviceStartRequest{
			Device: "usbaudio", Type: mode,
		}
		return &req, "", nil
	case "api":
		if sel.LocalDrive == nil {
			return nil, "", fmt.Errorf("LocalDrive == nil for api device: %s", sel.Name)
		}
		if sel.LocalDrive.SourceType == "mtp" {
			return &models.DeviceStartRequest{
				Device: "mtp", Server: sel.LocalDrive.Name,
			}, "", nil
		}
		return &models.DeviceStartRequest{
			Device: "drive", Server: sel.LocalDrive.Name, DriveMode: sel.DriveMode,
		}, "", nil
	case "local", "user":
		if sel.DiskInfo == nil {
			return nil, "", fmt.Errorf("DiskInfo == nil for %s device: %s", sel.Source, sel.Name)
		}
		localIP, err := dw.getLocalIP()
		if err != nil {
			return nil, "", fmt.Errorf("error getting local IP: %v", err)
		}
		nbdPort, err := dw.getAvailablePort()
		if err != nil {
			return nil, "", fmt.Errorf("error getting a free port: %v", err)
		}
		exportName := sel.DiskInfo.Name
		dw.nbdServersMu.Lock()
		if existing, ok := dw.nbdServers[exportName]; ok {
			if existing.IsRunning() {
				_ = existing.Stop()
			}
			delete(dw.nbdServers, exportName)
		}
		dw.nbdServersMu.Unlock()
		// cdrom mode is always read-only
		readOnly := sel.ReadOnly
		if sel.DriveMode == "cdrom" {
			readOnly = true
		}
		nbdServer, err := dw.startNBDServer(sel.DiskInfo, nbdPort, exportName, readOnly)
		if err != nil {
			return nil, "", fmt.Errorf("error starting NBD server: %v", err)
		}
		dw.nbdServersMu.Lock()
		dw.nbdServers[exportName] = nbdServer
		dw.nbdServersMu.Unlock()
		return &models.DeviceStartRequest{
			Device:                  "drive",
			Server:                  localIP,
			Port:                    nbdPort,
			ExportName:              nbdServer.NBDExportNameForAPI(),
			NBDHandshakeEmptyExport: nbdServer.NBDHandshakeEmptyExport(),
			ReadOnly:                readOnly,
			DriveMode:               sel.DriveMode,
		}, "", nil
	}
	return nil, "", fmt.Errorf("unknown device type: %s (source=%s)", sel.Name, sel.Source)
}

// nbdExportNamesForRequests returns the NBD export names for the requests — used for the mounting animation.
func (dw *DiskWidget) nbdExportNamesForRequests(requests []models.DeviceStartRequest) map[string]bool {
	names := make(map[string]bool)
	for _, req := range requests {
		if req.Device != "drive" || req.Port == 0 {
			continue
		}
		dw.nbdServersMu.Lock()
		for name, srv := range dw.nbdServers {
			if !srv.IsRunning() {
				continue
			}
			st := srv.GetServerStatus()
			p, ok := st["server_port"]
			if !ok {
				continue
			}
			var port int
			switch v := p.(type) {
			case int:
				port = v
			case int64:
				port = int(v)
			case float64:
				port = int(v)
			default:
				continue
			}
			if port == req.Port {
				names[name] = true
				break
			}
		}
		dw.nbdServersMu.Unlock()
	}
	return names
}

// waitForNBDServers signals the NBD servers to accept connections and waits for them to be ready.
func (dw *DiskWidget) waitForNBDServers(timeout time.Duration) error {
	dw.nbdServersMu.Lock()
	if len(dw.nbdServers) == 0 {
		dw.nbdServersMu.Unlock()
		return nil
	}

	logrus.Infof("📡 [NBD] Signaling readiness to %d servers", len(dw.nbdServers))
	for name, srv := range dw.nbdServers {
		logrus.Infof("  📡 [NBD] SignalReady: %s", name)
		srv.SignalReady()
	}

	remaining := make(map[string]service.NBDRunner, len(dw.nbdServers))
	for k, v := range dw.nbdServers {
		remaining[k] = v
	}
	dw.nbdServersMu.Unlock()

	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			names := make([]string, 0, len(remaining))
			for n := range remaining {
				names = append(names, n)
			}
			return fmt.Errorf("timed out waiting for NBD servers: %v", names)
		case <-ticker.C:
			for name, srv := range remaining {
				select {
				case <-srv.WaitReady():
					logrus.Infof("✅ [NBD] Server %s ready", name)
					delete(remaining, name)
				default:
				}
			}
			if len(remaining) == 0 {
				logrus.Infof("✅ [NBD] All servers ready")
				return nil
			}
		}
	}
}

// handleUnmount handles the Disconnect button press.
func (dw *DiskWidget) handleUnmount() {
	if dw.controlsLocked() {
		return
	}
	if dw.usbClient == nil {
		if dw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		return
	}

	// Collect mounted non-video/non-audio devices
	var mountedDrives []DriveItem
	var mountedIndices []int
	for i, d := range dw.allDrives {
		if d.IsMounted && !d.IsVideo && !d.IsAudio {
			mountedDrives = append(mountedDrives, d)
			mountedIndices = append(mountedIndices, i)
		}
	}
	videoMounted := false
	audioMounted := false
	for _, d := range dw.allDrives {
		if d.IsVideo && d.IsMounted {
			videoMounted = true
		}
		if d.IsAudio && d.IsMounted {
			audioMounted = true
		}
	}

	if len(mountedDrives) == 0 && !videoMounted && !audioMounted {
		if dw.window != nil {
			view.ShowInfoDialog(i18n.Current.Information, i18n.Current.NoMountedDevices, dw.window)
		}
		return
	}

	// Determine what to disconnect exactly (selection or everything)
	selectedIndices := make(map[int]bool)
	selectedMountedVideo := false
	dw.selectedItemsMu.RLock()
	for id, sel := range dw.selectedItems {
		if sel && id < len(dw.allDrives) && dw.allDrives[id].IsMounted {
			if dw.allDrives[id].IsVideo {
				selectedMountedVideo = true
			} else {
				selectedIndices[id] = true
			}
		}
	}
	dw.selectedItemsMu.RUnlock()

	unmountAll := len(selectedIndices) == 0 && !selectedMountedVideo
	confirmMsg := i18n.Current.UnmountAllConfirm
	if !unmountAll {
		confirmMsg = i18n.Current.UnmountSelectedConfirm
	}

	// Snapshot state for the goroutine
	snapMountedDrives := make([]DriveItem, len(mountedDrives))
	copy(snapMountedDrives, mountedDrives)
	snapMountedIndices := make([]int, len(mountedIndices))
	copy(snapMountedIndices, mountedIndices)
	snapSelected := make(map[int]bool, len(selectedIndices))
	for k, v := range selectedIndices {
		snapSelected[k] = v
	}

	needsConfirm := false
	if unmountAll {
		for _, d := range mountedDrives {
			if driveNeedsUnmountConfirm(d) {
				needsConfirm = true
				break
			}
		}
	} else {
		for i, d := range mountedDrives {
			if snapSelected[mountedIndices[i]] && driveNeedsUnmountConfirm(d) {
				needsConfirm = true
				break
			}
		}
	}

	start := func() {
		dw.startUnmount(unmountAll, snapSelected, snapMountedDrives, snapMountedIndices)
	}
	if !needsConfirm {
		start()
		return
	}

	view.ShowConfirmToast(confirmMsg, func(ok bool) {
		if !ok {
			return
		}
		start()
	}, dw.window)
}

// driveNeedsUnmountConfirm is true only for mass-storage / flash (ISO,
// USB stick, MTP backup). HID, network, video, and audio unmount without
// a confirmation toast -- the toggle twitch on keyboard/mouse was the
// confirm flipping the switch before the gadget actually dropped.
func driveNeedsUnmountConfirm(d DriveItem) bool {
	switch {
	case d.IsKeyboard, d.IsMouse, d.IsGamepad, d.IsRNDIS, d.IsVideo, d.IsAudio, d.IsUSBAudio:
		return false
	default:
		return true
	}
}

func (dw *DiskWidget) startUnmount(unmountAll bool, snapSelected map[int]bool, snapMountedDrives []DriveItem, snapMountedIndices []int) {
	dw.beginOperation()
	dw.selectedItemsMu.Lock()
	if unmountAll {
		dw.selectedItems = make(map[int]bool)
	} else {
		for idx := range snapSelected {
			delete(dw.selectedItems, idx)
		}
	}
	dw.selectedItemsMu.Unlock()
	dw.updateButtons()
	dw.requestDevicesRefresh()

	go dw.doUnmount(unmountAll, snapSelected, snapMountedDrives, snapMountedIndices)
}

// doUnmount performs the unmount in a goroutine.
func (dw *DiskWidget) doUnmount(unmountAll bool, selectedIndices map[int]bool, mountedDrives []DriveItem, mountedIndices []int) {
	defer dw.endOperation() // ALWAYS resets all flags and refreshes the UI

	if unmountAll {
		if dw.onVideoDisconnect != nil {
			dw.onVideoDisconnect()
		}
		if dw.onAudioDisconnect != nil {
			dw.onAudioDisconnect()
		}
		usbpass.StopSession()
		dw.usbPassSessions = nil
		dw.updateStatusAsync(i18n.Current.StoppingAllDevices)
		if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, nil, false); err != nil {
			logrus.Warnf("⚠️ [UNMOUNT-ALL] Stop error: %v", err)
		} else {
			logrus.Infof("✅ [UNMOUNT-ALL] All devices stopped")
		}
		dw.stopNBDAndCleanup(mountedDrives, true)
		dw.updateStatusAsync(i18n.Current.AllDevicesUnmounted)
		return
	}

	// Partial unmount: determine what to keep and what to remove
	keepIndices := make(map[int]bool)
	for _, idx := range mountedIndices {
		if !selectedIndices[idx] {
			keepIndices[idx] = true
		}
	}

	drivesToUnmount := make([]DriveItem, 0, len(selectedIndices))
	stopUSBPass := false
	for idx := range selectedIndices {
		if idx >= len(dw.allDrives) {
			continue
		}
		if dw.allDrives[idx].IsVideo && dw.onVideoDisconnect != nil {
			dw.onVideoDisconnect()
		}
		if dw.allDrives[idx].IsAudio && dw.onAudioDisconnect != nil {
			dw.onAudioDisconnect()
		}
		if dw.allDrives[idx].IsUSBPassthrough {
			stopUSBPass = true
		}
		drivesToUnmount = append(drivesToUnmount, dw.allDrives[idx])
	}
	if stopUSBPass {
		usbpass.StopSession()
		dw.usbPassSessions = nil
	}

	dw.updateStatusAsync(i18n.Current.StoppingAllDevices)

	if len(keepIndices) == 0 {
		// Removing everything — simple stop
		if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, nil, false); err != nil {
			logrus.Warnf("⚠️ [UNMOUNT-SEL] Stop error: %v", err)
		}
	} else {
		// Some devices remain — Full Replace with only the ones being kept
		var keepRequests []models.DeviceStartRequest
		for idx := range keepIndices {
			if idx >= len(dw.allDrives) {
				continue
			}
			req, err := dw.buildDeviceRequestForDrive(dw.allDrives[idx], true)
			if err != nil {
				logrus.Warnf("⚠️ [UNMOUNT-SEL] Skipping %s: %v", dw.allDrives[idx].Name, err)
				continue
			}
			keepRequests = append(keepRequests, *req)
		}
		logrus.Infof("🔄 [UNMOUNT-SEL] Full Replace with %d remaining devices", len(keepRequests))
		if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, models.DeviceStartBatchRequest(keepRequests), false); err != nil {
			logrus.Warnf("⚠️ [UNMOUNT-SEL] Reconnect error: %v", err)
		}
	}

	dw.stopNBDAndCleanup(drivesToUnmount, false)
	dw.updateStatusAsync(i18n.Current.AllDevicesUnmounted)
	logrus.Infof("✅ [UNMOUNT-SEL] Partial unmount complete")
}

// StopAllNBDServers stops all running NBD servers and clears the map.
// Called from the main disconnect flow to release ports even when the disk widget
// unmount button was not used.
func (dw *DiskWidget) StopAllNBDServers() {
	dw.stopNBDAndCleanup(nil, true)
}

// StopUSBPassthrough tears down the local USB/IP export + broker attach and
// clears the green mounted marker. Safe to call when nothing is mounted.
func (dw *DiskWidget) StopUSBPassthrough() {
	usbpass.StopSession()
	fyne.Do(func() {
		dw.usbPassSessions = nil
		for i := range dw.allDrives {
			if dw.allDrives[i].IsUSBPassthrough {
				dw.allDrives[i].IsMounted = false
			}
		}
		dw.updateButtons()
		dw.requestDevicesRefresh()
	})
}

// stopNBDAndCleanup stops NBD servers and releases resources.
func (dw *DiskWidget) stopNBDAndCleanup(drives []DriveItem, stopAll bool) {
	dw.updateStatusAsync(i18n.Current.StoppingNBDServers)
	toStop := make(map[string]bool)
	dw.nbdServersMu.Lock()
	if stopAll {
		for exportName := range dw.nbdServers {
			toStop[exportName] = true
		}
	} else {
		for _, drive := range drives {
			if drive.DiskInfo != nil {
				exportName := drive.DiskInfo.Name
				if _, exists := dw.nbdServers[exportName]; exists {
					toStop[exportName] = true
				}
			}
		}
	}
	dw.nbdServersMu.Unlock()
	for exportName := range toStop {
		dw.nbdServersMu.Lock()
		nbdServer, exists := dw.nbdServers[exportName]
		dw.nbdServersMu.Unlock()
		if exists {
			if nbdServer.IsRunning() {
				if err := nbdServer.Stop(); err != nil {
					logrus.Warnf("⚠️ Error stopping NBD server %s: %v", exportName, err)
				}
			}
			dw.nbdServersMu.Lock()
			delete(dw.nbdServers, exportName)
			dw.nbdServersMu.Unlock()
		}
	}
	if stopAll {
		dw.nbdServersMu.Lock()
		dw.nbdServers = make(map[string]service.NBDRunner)
		dw.nbdServersMu.Unlock()
	}
	if runtime.GOOS == "android" && dw.safHelper != nil {
		for _, drive := range drives {
			if drive.DiskInfo != nil && strings.HasPrefix(drive.DiskInfo.URI, "content://") {
				_ = dw.safHelper.CloseFD(drive.DiskInfo.URI)
			}
		}
	}
}

// buildDeviceRequestForDrive builds a DeviceStartRequest for an already-mounted device
// (uses existing NBD servers).
func (dw *DiskWidget) buildDeviceRequestForDrive(drive DriveItem, useExistingNBD bool) (*models.DeviceStartRequest, error) {
	if drive.Source == "keyboard" {
		req := newKeyboardStartRequest()
		return &req, nil
	}
	if drive.Source == "mouse" {
		dispIdx, dispCnt := dw.GetDisplayConfig()
		req := newMouseStartRequestWithDisplay(drive.MouseType, dispIdx, dispCnt)
		return &req, nil
	}
	if drive.Source == "rndis" {
		req := newRNDISStartRequest(drive.RNDISMode)
		return &req, nil
	}
	if drive.Source == "gamepad" {
		req := newGamepadStartRequest(drive.GamepadMode, drive.GamepadVendorID, drive.GamepadProductID)
		return &req, nil
	}
	if drive.Source == "usbaudio" {
		mode := drive.USBAudioMode
		if mode == "" {
			mode = "uac1"
		}
		req := models.DeviceStartRequest{Device: "usbaudio", Type: mode}
		return &req, nil
	}
	if drive.Source == "api" && drive.LocalDrive != nil {
		if drive.LocalDrive.SourceType == "mtp" {
			return &models.DeviceStartRequest{
				Device: "mtp", Server: drive.LocalDrive.Name,
			}, nil
		}
		return &models.DeviceStartRequest{
			Device: "drive", Server: drive.LocalDrive.Name, DriveMode: drive.DriveMode,
		}, nil
	}
	if (drive.Source == "local" || drive.Source == "user") && drive.DiskInfo != nil && useExistingNBD {
		exportName := drive.DiskInfo.Name
		dw.nbdServersMu.Lock()
		nbdServer, exists := dw.nbdServers[exportName]
		dw.nbdServersMu.Unlock()
		if !exists || !nbdServer.IsRunning() {
			return nil, fmt.Errorf("NBD server for %s not found or not running", exportName)
		}
		status := nbdServer.GetServerStatus()
		portVal, ok := status["server_port"]
		if !ok {
			return nil, fmt.Errorf("port for NBD server %s not found", exportName)
		}
		var port int
		switch p := portVal.(type) {
		case int:
			port = p
		case int64:
			port = int(p)
		case float64:
			port = int(p)
		default:
			return nil, fmt.Errorf("invalid port type for %s", exportName)
		}
		localIP, err := dw.getLocalIP()
		if err != nil {
			return nil, err
		}
		exportNameForAPI := nbdServer.NBDExportNameForAPI()
		return &models.DeviceStartRequest{
			Device:                  "drive",
			Server:                  localIP,
			Port:                    port,
			ExportName:              exportNameForAPI,
			NBDHandshakeEmptyExport: nbdServer.NBDHandshakeEmptyExport(),
			ReadOnly:                drive.ReadOnly,
		}, nil
	}
	return nil, fmt.Errorf("unknown device type: %s", drive.Name)
}

// countSelectedItems returns the number of selected items.
func (dw *DiskWidget) countSelectedItems() int {
	dw.selectedItemsMu.RLock()
	defer dw.selectedItemsMu.RUnlock()
	count := 0
	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) {
			count++
		}
	}
	return count
}

func (dw *DiskWidget) countSelectedGadgetItems() int {
	dw.selectedItemsMu.RLock()
	defer dw.selectedItemsMu.RUnlock()
	count := 0
	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) && !dw.allDrives[id].IsVideo {
			count++
		}
	}
	return count
}

// updateButtons updates the button states.
func (dw *DiskWidget) updateButtons() {
	dw.selectedItemsMu.RLock()
	selectedCount := 0
	selectedNotMountedCount := 0
	selectedGadgetNotMountedCount := 0
	mountedCount := 0

	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) && !dw.allDrives[id].IsVideo {
			selectedCount++
			if !dw.allDrives[id].IsMounted {
				selectedNotMountedCount++
			}
			if !dw.allDrives[id].IsVideo && !dw.allDrives[id].IsMounted {
				selectedGadgetNotMountedCount++
			}
		}
	}
	dw.selectedItemsMu.RUnlock()
	for _, drive := range dw.allDrives {
		if drive.IsMounted && !drive.IsVideo {
			mountedCount++
		}
	}

	hasMountedDevices := mountedCount > 0
	videoMounted := false
	for _, drive := range dw.allDrives {
		if drive.IsVideo && drive.IsMounted {
			videoMounted = true
			break
		}
	}
	hasMountedDevices = hasMountedDevices || videoMounted

	canAdd := selectedNotMountedCount > 0 && (mountedCount+selectedGadgetNotMountedCount) <= MaxDevicesToMount
	controlsLocked := dw.controlsLocked()

	fyne.Do(func() {
		dw.syncDashboardFooter()
		if dw.unmountBtn == nil || dw.mountBtn == nil {
			return
		}

		disconnectLabel := i18n.Current.DisconnectButton
		if selectedCount == 0 && hasMountedDevices {
			disconnectLabel = i18n.Current.DisconnectAllButton
		}
		dw.unmountBtn.SetText(disconnectLabel)
		if dw.compactUnmountBtn != nil {
			dw.compactUnmountBtn.SetLabel(disconnectLabel)
		}

		if selectedNotMountedCount == 0 {
			dw.mountBtn.Hide()
			if dw.compactMountBtn != nil {
				dw.compactMountBtn.Hide()
				dw.compactMountBtn.Disable()
			}
		} else {
			dw.mountBtn.Show()
			if dw.compactMountBtn != nil {
				dw.compactMountBtn.Show()
				if canAdd && !controlsLocked {
					dw.compactMountBtn.Enable()
				} else {
					dw.compactMountBtn.Disable()
				}
			}
		}

		if hasMountedDevices || selectedCount > 0 {
			dw.unmountBtn.Show()
			if controlsLocked {
				dw.unmountBtn.Disable()
			} else {
				dw.unmountBtn.Enable()
			}
			if dw.compactUnmountBtn != nil {
				dw.compactUnmountBtn.Show()
				if controlsLocked {
					dw.compactUnmountBtn.Disable()
				} else {
					dw.compactUnmountBtn.Enable()
				}
			}
		} else {
			dw.unmountBtn.Hide()
			if dw.compactUnmountBtn != nil {
				dw.compactUnmountBtn.Hide()
				dw.compactUnmountBtn.Disable()
			}
		}

		if selectedCount == 0 {
			dw.mountBtn.Disable()
			if dw.onButtonsChanged != nil {
				dw.onButtonsChanged()
			}
			return
		}

		if canAdd && !controlsLocked {
			dw.mountBtn.Enable()
		} else {
			dw.mountBtn.Disable()
		}
		if dw.onButtonsChanged != nil {
			dw.onButtonsChanged()
		}
	})
}

func (dw *DiskWidget) reconfigureMountedDevicesForMouseMode(newMode string) {
	if dw.usbClient == nil {
		dw.showErrorAsync(fmt.Errorf("%s", i18n.Current.ErrorNotConnected))
		return
	}

	dw.beginOperation()

	go func() {
		defer dw.endOperation()

		var deviceRequests []models.DeviceStartRequest
		mouseIncluded := false

		for _, drive := range dw.allDrives {
			if !drive.IsMounted || drive.IsVideo || drive.IsAudio {
				continue
			}
			current := drive
			if current.IsMouse {
				current.MouseType = newMode
			}
			req, err := dw.buildDeviceRequestForDrive(current, true)
			if err != nil {
				dw.showErrorAsync(fmt.Errorf("failed to rebuild config for %s: %w", current.Name, err))
				return
			}
			deviceRequests = append(deviceRequests, *req)
			if current.IsMouse {
				mouseIncluded = true
			}
		}

		if !mouseIncluded && dw.isMouseMountedActual() {
			dispIdx, dispCnt := dw.GetDisplayConfig()
			mouseReq := newMouseStartRequestWithDisplay(newMode, dispIdx, dispCnt)
			deviceRequests = append(deviceRequests, mouseReq)
		}

		if len(deviceRequests) == 0 {
			dw.showErrorAsync(fmt.Errorf("no mounted devices to reconfigure"))
			return
		}

		dw.updateStatusAsync("Reconfiguring USB gadget...")
		if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, models.DeviceStartBatchRequest(deviceRequests), false); err != nil {
			dw.showErrorAsync(fmt.Errorf("error reconfiguring mouse: %w", err))
			return
		}

		if dw.onMouseTypeChanged != nil {
			dw.preferredMouseMode = newMode
			dw.onMouseTypeChanged(newMode)
		}
		if dw.onMouseModeReconfigured != nil {
			dw.onMouseModeReconfigured()
		}
	}()
}

func (dw *DiskWidget) mountUSBPassthrough(items []DriveItem) {
	dw.beginOperation()
	go func() {
		defer dw.endOperation()
		var devices []models.USBPassthroughDevice
		for _, it := range items {
			if it.USBPassthrough == nil {
				continue
			}
			if it.USBPassthrough.Protected {
				dw.showErrorAsync(fmt.Errorf("%s", i18n.Current.USBPassthroughProtected))
				return
			}
			devices = append(devices, *it.USBPassthrough)
		}
		if len(devices) == 0 {
			return
		}
		// Go client owns the USB/IP server (export). rust-shine on the agent
		// is the USB/IP client (win2 VHCI).
		exportPort := 3240
		if _, err := usbpass.StartSession(fmt.Sprintf("0.0.0.0:%d", exportPort), devices); err != nil {
			dw.showErrorAsync(fmt.Errorf("USB/IP export: %w", err))
			return
		}
		if _, err := dw.usbClient.OpenUSBPassthroughSession(); err != nil {
			usbpass.StopSession()
			dw.showErrorAsync(fmt.Errorf("%s: %w", i18n.Current.USBPassthroughEnterpriseHint, err))
			return
		}
		base := dw.usbClient.GetBaseURL()
		u, err := url.Parse(base)
		if err != nil {
			usbpass.StopSession()
			dw.showErrorAsync(err)
			return
		}
		port := 8090
		if dw.config != nil && dw.config.USBPassthroughPort > 0 {
			port = dw.config.USBPassthroughPort
		}
		addr := net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
		secret := string(dw.usbClient.APISecret())
		for _, d := range devices {
			inst := d.InstanceID
			if inst == "" {
				inst = d.BusID
			}
			if err := usbpass.Attach(usbpass.AttachOptions{
				AgentAddr:       addr,
				Secret:          secret,
				InstanceID:      inst,
				USBIPBusID:      d.BusID,
				ExportService:   strconv.Itoa(exportPort),
				AllowUnlicensed: true, // lab; enterprise gate is on the agent broker
			}); err != nil {
				usbpass.StopSession()
				dw.showErrorAsync(err)
				return
			}
		}
	}()
}
