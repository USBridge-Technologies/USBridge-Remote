package controller

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

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
		// Transient network conditions only — deliberately NOT retrying on
		// login/auth failures (e.g. CHAP mismatch), which won't self-resolve.
		isRetryable := strings.Contains(errStr, "EOF") ||
			strings.Contains(errStr, "connection reset") || strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "timeout") || strings.Contains(errStr, "timed out")
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
		// Reset all mounting animations
		for i := range dw.allDrives {
			dw.allDrives[i].IsMounting = false
		}
		// Unconditionally reset the flags BEFORE calling updateDevicesStatus/updateButtons,
		// so controlsLocked() returns false and buttons are guaranteed to re-enable.
		dw.userOperationInFlight.Store(false)
		dw.apiMountInProgress.Store(false)
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
	for _, d := range dw.allDrives {
		if d.IsMounted && !d.IsVideo {
			mountedGadgetCount++
		}
	}
	if mountedGadgetCount+len(selectedDrives) > MaxDevicesToMount {
		if dw.window != nil {
			view.ShowInfoDialog(i18n.Current.Information, i18n.Current.MaxDevicesReached, dw.window)
		}
		return
	}

	logrus.Infof("📁 [MOUNT] mounted: %d, adding: %d", mountedGadgetCount, len(selectedDrives))

	// An XInput gamepad is incompatible with keyboard/mouse in the same composite device:
	// Windows won't initialize the remaining HID interfaces under the Xbox VID/PID.
	// Check both the new selection and the already-mounted devices.
	if dw.window != nil {
		hasXInputSelected := false
		hasHIDSelected := false
		for _, d := range selectedDrives {
			if d.IsGamepad && normalizeGamepadMode(d.GamepadMode) == gamepadModeXInput {
				hasXInputSelected = true
			}
			if d.IsKeyboard || d.IsMouse {
				hasHIDSelected = true
			}
		}
		hasHIDMounted := false
		for _, d := range dw.allDrives {
			if d.IsMounted && (d.IsKeyboard || d.IsMouse) {
				hasHIDMounted = true
			}
		}
		if hasXInputSelected && (hasHIDSelected || hasHIDMounted) {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.XInputIncompatibleWithHID), dw.window)
			return
		}
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

	dw.beginOperation()

	go func() {
		defer func() {
			if progressDialog != nil {
				fyne.Do(progressDialog.Hide)
			}
			dw.endOperation()
		}()

		// Build requests, start iSCSI target servers
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

		// Determine export names for the mounting animation
		mountingExportNames := dw.exportNamesForRequests(deviceRequests)

		// Wait for the iSCSI target servers to be ready
		if err := dw.waitForExportServers(30 * time.Second); err != nil {
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

// buildMountRequest builds a DeviceStartRequest for the device being mounted.
// Starts an iSCSI target server for disk devices.
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
		portalPort, err := dw.getAvailablePort()
		if err != nil {
			return nil, "", fmt.Errorf("error getting a free port: %v", err)
		}
		exportName := sel.DiskInfo.Name
		dw.exportServersMu.Lock()
		if existing, ok := dw.exportServers[exportName]; ok {
			if existing.IsRunning() {
				_ = existing.Stop()
			}
			delete(dw.exportServers, exportName)
		}
		dw.exportServersMu.Unlock()
		// cdrom mode is always read-only
		readOnly := sel.ReadOnly
		if sel.DriveMode == "cdrom" {
			readOnly = true
		}
		exportServer, err := dw.startExportServer(sel.DiskInfo, portalPort, exportName, readOnly)
		if err != nil {
			return nil, "", fmt.Errorf("error starting iSCSI target server: %v", err)
		}
		dw.exportServersMu.Lock()
		dw.exportServers[exportName] = exportServer
		dw.exportServersMu.Unlock()
		iqn := exportServer.ExportNameForAPI()
		return &models.DeviceStartRequest{
			Device:     "drive",
			Server:     localIP,
			Port:       portalPort,
			ExportName: iqn,
			ReadOnly:   readOnly,
			DriveMode:  sel.DriveMode,
			Transport:  "iscsi",
			TargetIQN:  iqn,
			LUN:        0,
		}, "", nil
	}
	return nil, "", fmt.Errorf("unknown device type: %s (source=%s)", sel.Name, sel.Source)
}

// exportNamesForRequests returns the export names for the requests — used for the mounting animation.
func (dw *DiskWidget) exportNamesForRequests(requests []models.DeviceStartRequest) map[string]bool {
	names := make(map[string]bool)
	for _, req := range requests {
		if req.Device != "drive" || req.Port == 0 {
			continue
		}
		dw.exportServersMu.Lock()
		for name, srv := range dw.exportServers {
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
		dw.exportServersMu.Unlock()
	}
	return names
}

// waitForExportServers signals the iSCSI target servers to accept connections and waits for them to be ready.
func (dw *DiskWidget) waitForExportServers(timeout time.Duration) error {
	dw.exportServersMu.Lock()
	if len(dw.exportServers) == 0 {
		dw.exportServersMu.Unlock()
		return nil
	}

	logrus.Infof("📡 [ISCSI] Signaling readiness to %d servers", len(dw.exportServers))
	for name, srv := range dw.exportServers {
		logrus.Infof("  📡 [ISCSI] SignalReady: %s", name)
		srv.SignalReady()
	}

	remaining := make(map[string]service.BlockExportRunner, len(dw.exportServers))
	for k, v := range dw.exportServers {
		remaining[k] = v
	}
	dw.exportServersMu.Unlock()

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
			return fmt.Errorf("timed out waiting for iSCSI target servers: %v", names)
		case <-ticker.C:
			for name, srv := range remaining {
				select {
				case <-srv.WaitReady():
					logrus.Infof("✅ [ISCSI] Server %s ready", name)
					delete(remaining, name)
				default:
				}
			}
			if len(remaining) == 0 {
				logrus.Infof("✅ [ISCSI] All servers ready")
				return nil
			}
		}
	}
}

// handleUnmount handles the Disconnect button press.
func (dw *DiskWidget) handleUnmount() {
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

	view.ShowConfirmYesLeft(i18n.Current.Confirmation, confirmMsg, func(ok bool) {
		if !ok {
			return
		}
		dw.beginOperation()
		// Immediately clear selection for visual feedback
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
	}, dw.window)
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
		dw.updateStatusAsync(i18n.Current.StoppingAllDevices)
		if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, nil, false); err != nil {
			logrus.Warnf("⚠️ [UNMOUNT-ALL] Stop error: %v", err)
		} else {
			logrus.Infof("✅ [UNMOUNT-ALL] All devices stopped")
		}
		dw.stopExportsAndCleanup(mountedDrives, true)
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
		drivesToUnmount = append(drivesToUnmount, dw.allDrives[idx])
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

	dw.stopExportsAndCleanup(drivesToUnmount, false)
	dw.updateStatusAsync(i18n.Current.AllDevicesUnmounted)
	logrus.Infof("✅ [UNMOUNT-SEL] Partial unmount complete")
}

// StopAllExportServers stops all running iSCSI target servers and clears the map.
// Called from the main disconnect flow to release ports even when the disk widget
// unmount button was not used.
func (dw *DiskWidget) StopAllExportServers() {
	dw.stopExportsAndCleanup(nil, true)
}

// stopExportsAndCleanup stops iSCSI target servers and releases resources.
func (dw *DiskWidget) stopExportsAndCleanup(drives []DriveItem, stopAll bool) {
	dw.updateStatusAsync(i18n.Current.StoppingExportServers)
	toStop := make(map[string]bool)
	dw.exportServersMu.Lock()
	if stopAll {
		for exportName := range dw.exportServers {
			toStop[exportName] = true
		}
	} else {
		for _, drive := range drives {
			if drive.DiskInfo != nil {
				exportName := drive.DiskInfo.Name
				if _, exists := dw.exportServers[exportName]; exists {
					toStop[exportName] = true
				}
			}
		}
	}
	dw.exportServersMu.Unlock()
	for exportName := range toStop {
		dw.exportServersMu.Lock()
		exportServer, exists := dw.exportServers[exportName]
		dw.exportServersMu.Unlock()
		if exists {
			if exportServer.IsRunning() {
				if err := exportServer.Stop(); err != nil {
					logrus.Warnf("⚠️ Error stopping iSCSI target server %s: %v", exportName, err)
				}
			}
			dw.exportServersMu.Lock()
			delete(dw.exportServers, exportName)
			dw.exportServersMu.Unlock()
		}
	}
	if stopAll {
		dw.exportServersMu.Lock()
		dw.exportServers = make(map[string]service.BlockExportRunner)
		dw.exportServersMu.Unlock()
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
// (uses existing iSCSI target servers).
func (dw *DiskWidget) buildDeviceRequestForDrive(drive DriveItem, useExistingExport bool) (*models.DeviceStartRequest, error) {
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
	if (drive.Source == "local" || drive.Source == "user") && drive.DiskInfo != nil && useExistingExport {
		exportName := drive.DiskInfo.Name
		dw.exportServersMu.Lock()
		exportServer, exists := dw.exportServers[exportName]
		dw.exportServersMu.Unlock()
		if !exists || !exportServer.IsRunning() {
			return nil, fmt.Errorf("iSCSI target server for %s not found or not running", exportName)
		}
		status := exportServer.GetServerStatus()
		portVal, ok := status["server_port"]
		if !ok {
			return nil, fmt.Errorf("port for iSCSI target server %s not found", exportName)
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
		iqn := exportServer.ExportNameForAPI()
		return &models.DeviceStartRequest{
			Device:     "drive",
			Server:     localIP,
			Port:       port,
			ExportName: iqn,
			ReadOnly:   drive.ReadOnly,
			Transport:  "iscsi",
			TargetIQN:  iqn,
			LUN:        0,
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
