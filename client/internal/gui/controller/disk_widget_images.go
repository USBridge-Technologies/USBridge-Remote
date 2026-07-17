package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"github.com/sirupsen/logrus"
)

type selectedImage struct {
	FileName string
	URI      string
}

// setUploadStateByPath sets the upload state for a file by its path.
func (dw *DiskWidget) setUploadStateByPath(path string, uploading bool, progress, speed float64) {
	for i := range dw.allDrives {
		if dw.allDrives[i].DiskInfo != nil && dw.allDrives[i].DiskInfo.Path == path {
			dw.allDrives[i].IsUploading = uploading
			dw.allDrives[i].UploadProgress = progress
			dw.allDrives[i].UploadSpeed = speed
			return
		}
	}
}

// refreshDriveItemByPath refreshes the list item's display by its path.
func (dw *DiskWidget) refreshDriveItemByPath(path string) {
	for i := range dw.allDrives {
		if dw.allDrives[i].DiskInfo != nil && dw.allDrives[i].DiskInfo.Path == path {
			dw.requestDevicesRefresh()
			return
		}
	}
	dw.requestDevicesRefresh()
}

// handleAddImage handles adding a disk image from the file system.
func (dw *DiskWidget) handleAddImage() {
	if !dw.imagePickerInFlight.CompareAndSwap(false, true) {
		logrus.Debug("image picker already in flight, skipping overlapping request")
		return
	}
	defer func() {
		if runtime.GOOS != "windows" {
			go func() {
				time.Sleep(1 * time.Second)
				dw.imagePickerInFlight.Store(false)
			}()
		}
	}()

	if dw.window == nil {
		logrus.Warn("⚠️ Window not set")
		dw.imagePickerInFlight.Store(false)
		return
	}

	if runtime.GOOS == "android" {
		dw.pickImageForDiskList()
		return
	}

	if !dw.checkStoragePermission() {
		fyne.Do(func() {
			view.ShowInfoDialog(
				i18n.Current.StoragePermissionRequired,
				i18n.Current.StoragePermissionMessage+"\n\n"+i18n.Current.StoragePermissionSteps,
				dw.window,
			)
		})
		return
	}

	isDesktop := runtime.GOOS == "windows" || runtime.GOOS == "darwin" || runtime.GOOS == "linux"

	if isDesktop {
		dw.setUserOperationInFlight(true)

		go func() {
			defer dw.imagePickerInFlight.Store(false)
			selected, ok := dw.showPlatformNativeImagePicker()
			fyne.Do(func() {
				dw.setUserOperationInFlight(false)
				if ok {
					dw.handleSelectedImage(selected)
				}
			})
		}()
		return
	}

	fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			logrus.Errorf("❌ [ADD-IMAGE-ERROR] Error selecting file: %v", err)
			fyne.Do(func() {
				view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorSelectingFile, err), dw.window)
			})
			return
		}

		if reader == nil {
			return
		}

		uri := reader.URI()
		reader.Close()

		dw.handleSelectedImage(selectedImage{
			FileName: uri.Name(),
			URI:      uri.String(),
		})
	}, dw.window)

	fileDialog.SetView(dialog.ListView)
	fileDialog.SetTitleText(i18n.Current.SelectDiskImage)
	fileDialog.SetFilter(storage.NewExtensionFileFilter(dw.supportedTypes))
	fileDialog.Show()

	go func() {
		time.Sleep(50 * time.Millisecond)
		fyne.Do(func() {
			fileDialog.Resize(fyne.NewSize(900, 650))
		})
	}()
}

func (dw *DiskWidget) handleDeleteImageFromDevice(driveIndex int, filename string) {
	if driveIndex < 0 || driveIndex >= len(dw.allDrives) {
		return
	}
	drive := dw.allDrives[driveIndex]
	if dw.window != nil {
		view.ShowDeleteImageConfirm(
			drive.Name,
			false, // deletes the file from the server — irreversible
			func(confirmed bool) {
				if confirmed {
					go dw.deleteImageFromDevice(filename, drive.Name)
				}
			},
			dw.window,
		)
	}
}

func (dw *DiskWidget) handleSelectedImage(selected selectedImage) {
	fileName := strings.TrimSpace(selected.FileName)
	uriString := strings.TrimSpace(selected.URI)
	if fileName == "" || uriString == "" {
		return
	}

	var filePath string
	var fileSize int64

	if runtime.GOOS == "android" && strings.HasPrefix(uriString, "content://") {
		if err := dw.safHelper.TakePersistableUriPermission(uriString); err != nil {
			logrus.Errorf("❌ Failed to persist permission for URI: %v", err)
		}

		filePath = uriString
		isGoogleDrive := strings.Contains(uriString, "com.google.android.apps.docs.storage")
		if isGoogleDrive {
			fileSize = 0
		} else {
			file, err := dw.safHelper.OpenFileDescriptor(uriString, "r")
			if err == nil && file != nil {
				if stat, err := file.Stat(); err == nil {
					fileSize = stat.Size()
				}
				file.Close()
			}
		}
	} else {
		filePath = dw.convertAndroidURIToPath(uriString, fileName)
		if info, err := os.Stat(filePath); err == nil {
			fileSize = info.Size()
		}
	}

	ext := strings.ToLower(filepath.Ext(fileName))
	supported := false
	for _, supportedType := range dw.supportedTypes {
		if strings.ToLower(supportedType) == ext {
			supported = true
			break
		}
	}

	if !supported && runtime.GOOS == "android" && strings.HasPrefix(uriString, "content://") {
		supported = true
		if ext == "" {
			ext = ".img"
		}
	}

	if !supported {
		fyne.Do(func() {
			view.ShowErrorDialog(fmt.Errorf(i18n.Current.UnsupportedFileFormat, strings.Join(dw.supportedTypes, ", ")), dw.window)
		})
		return
	}

	diskInfo := &models.DiskInfo{
		Name:        fileName,
		Path:        filePath,
		URI:         uriString,
		Size:        fileSize,
		Type:        strings.TrimPrefix(ext, "."),
		Description: fmt.Sprintf("User image: %s", fileName),
		IsActive:    false,
	}

	for _, existingImage := range dw.userImages {
		if existingImage.URI == uriString || existingImage.Path == filePath {
			fyne.Do(func() {
				view.ShowInfoDialog(i18n.Current.Information, i18n.Current.FileAlreadyAdded, dw.window)
			})
			return
		}
	}

	dw.userImages = append(dw.userImages, diskInfo)
	dw.saveUserImagesToPreferences()
	dw.updateUIAsync(func() {
		dw.scheduleCombine()
	})
}

func (dw *DiskWidget) pickerTitle() string {
	if i18n.Current != nil && strings.TrimSpace(i18n.Current.SelectDiskImage) != "" {
		return i18n.Current.SelectDiskImage
	}
	return "Select disk image"
}

// saveUserImagesToPreferences saves the list of user images to preferences.
func (dw *DiskWidget) saveUserImagesToPreferences() {
	if dw.app == nil {
		return
	}

	prefs := dw.app.Preferences()
	prefs.SetInt("user_images_count", len(dw.userImages))

	for i, img := range dw.userImages {
		prefix := fmt.Sprintf("user_image_%d_", i)
		prefs.SetString(prefix+"name", img.Name)
		prefs.SetString(prefix+"path", img.Path)
		prefs.SetString(prefix+"uri", img.URI)
		prefs.SetString(prefix+"type", img.Type)
		prefs.SetString(prefix+"description", img.Description)
		prefs.SetString(prefix+"size", fmt.Sprintf("%d", img.Size))
	}
}

// loadUserImagesFromPreferences loads the list of user images from preferences.
func (dw *DiskWidget) loadUserImagesFromPreferences() {
	if dw.app == nil {
		return
	}

	prefs := dw.app.Preferences()
	count := prefs.IntWithFallback("user_images_count", 0)
	if count == 0 {
		return
	}

	dw.userImages = make([]*models.DiskInfo, 0, count)

	for i := 0; i < count; i++ {
		prefix := fmt.Sprintf("user_image_%d_", i)
		name := prefs.StringWithFallback(prefix+"name", "")
		path := prefs.StringWithFallback(prefix+"path", "")

		if name == "" || path == "" {
			continue
		}

		uri := prefs.StringWithFallback(prefix+"uri", "")
		if !dw.checkFileExists(path) {
			continue
		}

		imgType := prefs.StringWithFallback(prefix+"type", "")
		description := prefs.StringWithFallback(prefix+"description", "")
		sizeStr := prefs.StringWithFallback(prefix+"size", "0")

		var size int64
		fmt.Sscanf(sizeStr, "%d", &size)

		diskInfo := &models.DiskInfo{
			Name:        name,
			Path:        path,
			URI:         uri,
			Size:        size,
			Type:        imgType,
			Description: description,
			IsActive:    false,
		}

		dw.userImages = append(dw.userImages, diskInfo)
	}
}

// removeUserImage removes a user image from the list.
func (dw *DiskWidget) removeUserImage(driveIndex int) {
	if driveIndex < 0 || driveIndex >= len(dw.allDrives) {
		return
	}

	drive := dw.allDrives[driveIndex]
	if drive.Source != "user" || drive.DiskInfo == nil {
		return
	}

	userImageIndex := -1
	for i, img := range dw.userImages {
		if img.Path == drive.DiskInfo.Path {
			userImageIndex = i
			break
		}
	}

	if userImageIndex == -1 {
		return
	}

	if dw.window != nil {
		view.ShowDeleteImageConfirm(
			drive.Name,
			true, // removes from list only — file on disk is not affected
			func(confirmed bool) {
				if confirmed {
					dw.userImages = append(dw.userImages[:userImageIndex], dw.userImages[userImageIndex+1:]...)
					dw.saveUserImagesToPreferences()
					dw.updateUIAsync(func() {
						dw.scheduleCombine()
					})
				}
			},
			dw.window,
		)
	}
}

// formatPathForDisplay formats a path for nicer display.
func (dw *DiskWidget) formatPathForDisplay(path, fileName string) string {
	if strings.HasPrefix(path, "content://") {
		displayPath := strings.TrimPrefix(path, "content://")
		if idx := strings.Index(displayPath, "/document/"); idx != -1 {
			displayPath = displayPath[idx+len("/document/"):]
		}

		displayPath = strings.ReplaceAll(displayPath, "%3A", ":")
		displayPath = strings.ReplaceAll(displayPath, "%2F", "/")
		displayPath = strings.ReplaceAll(displayPath, "%20", " ")

		if idx := strings.Index(displayPath, ":"); idx != -1 {
			displayPath = displayPath[idx+1:]
		}

		if displayPath == "" || !strings.Contains(displayPath, "/") {
			return fileName
		}

		return displayPath
	}

	return path
}

// handleUploadImage handles uploading an image to the device.
func (dw *DiskWidget) handleUploadImage(driveIndex int) {
	if driveIndex < 0 || driveIndex >= len(dw.allDrives) {
		return
	}

	drive := dw.allDrives[driveIndex]
	if drive.Source != "user" || drive.DiskInfo == nil {
		return
	}

	if dw.usbClient == nil {
		if dw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		return
	}

	if dw.window != nil {
		fyne.Do(func() {
			view.ShowUploadImageConfirm(
				drive.Name,
				func(confirmed bool) {
					if confirmed {
						go dw.uploadImageToDevice(drive)
					}
				},
				dw.window,
			)
		})
	}
}

// uploadImageToDevice uploads an image to the device.
func (dw *DiskWidget) uploadImageToDevice(drive DriveItem) {
	var fileReader *os.File
	var err error

	if runtime.GOOS == "android" && strings.HasPrefix(drive.DiskInfo.Path, "content://") {
		fileReader, err = dw.safHelper.OpenFileDescriptor(drive.DiskInfo.Path, "r")
	} else {
		fileReader, err = os.Open(drive.DiskInfo.Path)
	}

	if err != nil {
		dw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorOpeningFileForUpload, err))
		return
	}
	defer fileReader.Close()

	uploadPath := drive.DiskInfo.Path

	dw.updateUIAsync(func() {
		dw.setUploadStateByPath(uploadPath, true, 0, 0)
		dw.refreshDriveItemByPath(uploadPath)
	})

	progressCallback := func(percent float64, current, total int64, speed float64, eta time.Duration) {
		dw.updateUIAsync(func() {
			dw.setUploadStateByPath(uploadPath, true, percent, speed)
			dw.refreshDriveItemByPath(uploadPath)
		})
	}

	err = dw.usbClient.UploadISOWithProgress(drive.DiskInfo.Name, fileReader, progressCallback)

	dw.updateUIAsync(func() {
		dw.setUploadStateByPath(uploadPath, false, 0, 0)
		dw.refreshDriveItemByPath(uploadPath)
	})

	if err != nil {
		dw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorUploadingImage, err))
		return
	}

	dw.updateUIAsync(func() {
		if dw.window != nil {
			view.ShowInfoDialog(i18n.Current.Success, fmt.Sprintf(i18n.Current.ImageUploadedSuccess, drive.Name), dw.window)
		}
	})

	dw.loadLocalDrives()
}

// deleteImageFromDevice deletes an image from the device.
func (dw *DiskWidget) deleteImageFromDevice(filename string, displayName string) {
	err := dw.usbClient.DeleteISO(filename)
	if err != nil {
		dw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorDeletingImage, err))
		return
	}

	dw.updateUIAsync(func() {
		if dw.window != nil {
			view.ShowInfoDialog(i18n.Current.Success, fmt.Sprintf(i18n.Current.ImageDeletedSuccess, displayName), dw.window)
		}
	})

	dw.loadLocalDrives()
}
