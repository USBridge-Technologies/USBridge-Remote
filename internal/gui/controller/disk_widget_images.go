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

// setUploadStateByPath устанавливает состояние загрузки по пути файла.
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

// refreshDriveItemByPath обновляет отображение элемента в списке по пути.
func (dw *DiskWidget) refreshDriveItemByPath(path string) {
	for i := range dw.allDrives {
		if dw.allDrives[i].DiskInfo != nil && dw.allDrives[i].DiskInfo.Path == path {
			dw.requestDevicesRefresh()
			return
		}
	}
	dw.requestDevicesRefresh()
}

// handleAddImage обрабатывает добавление образа диска из файловой системы.
func (dw *DiskWidget) handleAddImage() {
	if dw.window == nil {
		logrus.Warn("⚠️ Окно не установлено")
		return
	}

	if !dw.checkStoragePermission() {
		fyne.Do(func() {
			dialog.ShowInformation(
				i18n.Current.StoragePermissionRequired,
				i18n.Current.StoragePermissionMessage+"\n\n"+i18n.Current.StoragePermissionSteps,
				dw.window,
			)
		})
		return
	}

	fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			logrus.Errorf("❌ [ADD-IMAGE-ERROR] Ошибка при выборе файла: %v", err)
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf(i18n.Current.ErrorSelectingFile, err), dw.window)
			})
			return
		}

		if reader == nil {
			logrus.Info("ℹ️ [ADD-IMAGE] Выбор файла отменен")
			return
		}

		uri := reader.URI()
		reader.Close()

		fileName := uri.Name()
		uriString := uri.String()

		logrus.Infof("📂 [ADD-IMAGE-1] Выбран файл: %s (URI: %s)", fileName, uriString)
		logrus.Infof("📂 [ADD-IMAGE-1] Runtime GOOS: %s", runtime.GOOS)

		var filePath string
		var fileSize int64

		if runtime.GOOS == "android" && strings.HasPrefix(uriString, "content://") {
			logrus.Infof("📍 [ADD-IMAGE-ANDROID-2] Обнаружен Android content:// URI")
			logrus.Infof("📍 [ADD-IMAGE-ANDROID-3] Вызов TakePersistableUriPermission для URI: %s", uriString)
			if err := dw.safHelper.TakePersistableUriPermission(uriString); err != nil {
				logrus.Errorf("❌ [ADD-IMAGE-ANDROID-3-ERROR] Не удалось сохранить разрешение для URI: %v", err)
			} else {
				logrus.Infof("✅ [ADD-IMAGE-ANDROID-3-SUCCESS] Разрешение для URI сохранено")
			}

			filePath = uriString
			logrus.Infof("📍 [ADD-IMAGE-ANDROID-4] Используем content:// URI напрямую: %s", filePath)

			isGoogleDrive := strings.Contains(uriString, "com.google.android.apps.docs.storage")
			if isGoogleDrive {
				logrus.Warnf("⚠️  [ADD-IMAGE-ANDROID-5-GDRIVE] Файл из Google Drive! Размер будет получен асинхронно при первом монтировании")
				fileSize = 0
			} else {
				logrus.Infof("📍 [ADD-IMAGE-ANDROID-5] Попытка получить размер файла через SAF")
				file, err := dw.safHelper.OpenFileDescriptor(uriString, "rw")
				if err == nil && file != nil {
					stat, err := file.Stat()
					if err == nil {
						fileSize = stat.Size()
						logrus.Infof("✅ [ADD-IMAGE-ANDROID-5-SUCCESS] Размер файла через SAF FD: %d байт", fileSize)
					} else {
						logrus.Warnf("⚠️ [ADD-IMAGE-ANDROID-5-ERROR] Не удалось получить размер: %v", err)
					}
					logrus.Infof("📍 [ADD-IMAGE-ANDROID-5] Файл остается открытым в кэше SAFHelper")
				} else {
					logrus.Warnf("⚠️ [ADD-IMAGE-ANDROID-5-ERROR] Не удалось открыть файл через SAF: %v", err)
				}
			}
		} else {
			logrus.Infof("📍 [ADD-IMAGE-DESKTOP-2] Десктоп режим или file:// URI")
			filePath = dw.convertAndroidURIToPath(uriString, fileName)
			logrus.Infof("📁 [ADD-IMAGE-DESKTOP-3] Путь к файлу: %s", filePath)

			if info, err := os.Stat(filePath); err == nil {
				fileSize = info.Size()
				logrus.Infof("✅ [ADD-IMAGE-DESKTOP-4] Размер файла: %d байт", fileSize)
			} else {
				logrus.Warnf("⚠️ [ADD-IMAGE-DESKTOP-4-ERROR] Не удалось получить размер файла: %v", err)
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

		if !supported {
			logrus.Warnf("⚠️ Неподдерживаемый формат файла: %s", fileName)
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf(i18n.Current.UnsupportedFileFormat, strings.Join(dw.supportedTypes, ", ")), dw.window)
			})
			return
		}

		diskInfo := &models.DiskInfo{
			Name:        fileName,
			Path:        filePath,
			URI:         uriString,
			Size:        fileSize,
			Type:        strings.TrimPrefix(ext, "."),
			Description: fmt.Sprintf("Пользовательский образ: %s", fileName),
			IsActive:    false,
		}

		logrus.Infof("📍 [ADD-IMAGE-6] Создан DiskInfo: Name=%s, Path=%s, URI=%s, Size=%d",
			diskInfo.Name, diskInfo.Path, diskInfo.URI, diskInfo.Size)

		for _, existingImage := range dw.userImages {
			if existingImage.URI == uriString || existingImage.Path == filePath {
				logrus.Warnf("⚠️ [ADD-IMAGE-6-WARN] Файл уже добавлен: %s (URI: %s)", filePath, uriString)
				fyne.Do(func() {
					dialog.ShowInformation(i18n.Current.Information, i18n.Current.FileAlreadyAdded, dw.window)
				})
				return
			}
		}

		dw.userImages = append(dw.userImages, diskInfo)
		logrus.Infof("✅ [ADD-IMAGE-7] Образ добавлен в userImages: %s (всего: %d)", diskInfo.Name, len(dw.userImages))

		logrus.Infof("📍 [ADD-IMAGE-8] Сохранение в preferences...")
		dw.saveUserImagesToPreferences()
		logrus.Infof("✅ [ADD-IMAGE-8-SUCCESS] Preferences сохранены")

		logrus.Infof("📍 [ADD-IMAGE-9] Обновление UI...")
		dw.updateUIAsync(func() {
			dw.combineDrives()
			dw.requestDevicesRefresh()
			logrus.Infof("✅ [ADD-IMAGE-9-SUCCESS] UI обновлен, образ отображен в списке")
		})
	}, dw.window)

	if runtime.GOOS != "android" {
		fileDialog.SetView(dialog.ListView)
		fileDialog.SetTitleText(i18n.Current.SelectDiskImage)
		fileDialog.SetFilter(storage.NewExtensionFileFilter(dw.supportedTypes))
	}

	fileDialog.Show()

	if runtime.GOOS != "android" {
		go func() {
			time.Sleep(50 * time.Millisecond)
			fyne.Do(func() {
				fileDialog.Resize(fyne.NewSize(900, 650))
			})
		}()
	}
}

// checkStoragePermission проверяет доступ к хранилищу (только для Android).
func (dw *DiskWidget) checkStoragePermission() bool {
	testPaths := []string{
		"/storage/emulated/0",
		"/sdcard",
	}

	hasAndroidPaths := false
	for _, path := range testPaths {
		if _, err := os.Stat(path); err == nil {
			hasAndroidPaths = true
			if _, err := os.ReadDir(path); err == nil {
				logrus.Infof("✅ Доступ к хранилищу есть: %s", path)
				return true
			}
		}
	}

	if !hasAndroidPaths {
		return true
	}

	logrus.Warn("⚠️ Нет доступа к хранилищу на Android")
	return false
}

// convertAndroidURIToPath конвертирует Android document URI в реальный путь файла.
func (dw *DiskWidget) convertAndroidURIToPath(uriString string, fileName string) string {
	if strings.HasPrefix(uriString, "content://com.android.externalstorage.documents/document/primary") {
		parts := strings.Split(uriString, "primary")
		if len(parts) >= 2 {
			relativePath := strings.TrimPrefix(parts[1], "%3A")
			relativePath = strings.TrimPrefix(relativePath, ":")
			relativePath = strings.ReplaceAll(relativePath, "%20", " ")
			relativePath = strings.ReplaceAll(relativePath, "%2F", "/")

			possiblePaths := []string{
				filepath.Join("/storage/emulated/0", relativePath),
				filepath.Join("/sdcard", relativePath),
				filepath.Join("/mnt/sdcard", relativePath),
				filepath.Join(os.Getenv("EXTERNAL_STORAGE"), relativePath),
			}

			for _, path := range possiblePaths {
				if path == "" {
					continue
				}
				if _, err := os.Stat(path); err == nil {
					logrus.Infof("✅ Найден файл по пути: %s", path)
					return path
				}
			}

			return possiblePaths[0]
		}
	}

	if strings.HasPrefix(uriString, "content://") && strings.Contains(uriString, "/document/") {
		searchPaths := []string{
			"/storage/emulated/0",
			"/sdcard",
			"/mnt/sdcard",
			os.Getenv("EXTERNAL_STORAGE"),
		}

		for _, basePath := range searchPaths {
			if basePath == "" {
				continue
			}

			fullPath := filepath.Join(basePath, fileName)
			if _, err := os.Stat(fullPath); err == nil {
				logrus.Infof("✅ Найден файл в корне: %s", fullPath)
				return fullPath
			}

			fullPath = filepath.Join(basePath, "Download", fileName)
			if _, err := os.Stat(fullPath); err == nil {
				logrus.Infof("✅ Найден файл в Download: %s", fullPath)
				return fullPath
			}
		}
	}

	if strings.HasPrefix(uriString, "file://") {
		return strings.TrimPrefix(uriString, "file://")
	}

	if strings.HasPrefix(uriString, "/") {
		return uriString
	}

	logrus.Warnf("⚠️ Не удалось конвертировать URI, используем как есть: %s", uriString)
	return uriString
}

// saveUserImagesToPreferences сохраняет список пользовательских образов в preferences.
func (dw *DiskWidget) saveUserImagesToPreferences() {
	if dw.app == nil {
		logrus.Warn("⚠️ App не установлен, не можем сохранить образы")
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

	logrus.Infof("💾 Сохранено %d пользовательских образов в preferences", len(dw.userImages))
}

// loadUserImagesFromPreferences загружает список пользовательских образов из preferences.
func (dw *DiskWidget) loadUserImagesFromPreferences() {
	if dw.app == nil {
		logrus.Warn("⚠️ App не установлен, не можем загрузить образы")
		return
	}

	prefs := dw.app.Preferences()
	count := prefs.IntWithFallback("user_images_count", 0)
	if count == 0 {
		logrus.Info("ℹ️ Нет сохраненных пользовательских образов")
		return
	}

	dw.userImages = make([]*models.DiskInfo, 0, count)

	for i := 0; i < count; i++ {
		prefix := fmt.Sprintf("user_image_%d_", i)
		name := prefs.StringWithFallback(prefix+"name", "")
		path := prefs.StringWithFallback(prefix+"path", "")

		if name == "" || path == "" {
			logrus.Warnf("⚠️ Пропускаем образ %d: пустое имя или путь", i)
			continue
		}

		uri := prefs.StringWithFallback(prefix+"uri", "")
		fileExists := false

		if runtime.GOOS == "android" && strings.HasPrefix(path, "content://") {
			isGoogleDrive := strings.Contains(path, "com.google.android.apps.docs.storage")
			if isGoogleDrive {
				logrus.Warnf("⚠️ Файл из Google Drive при загрузке из preferences: %s - добавляем без проверки", path)
				fileExists = true
			} else if dw.safHelper != nil {
				file, err := dw.safHelper.OpenFileDescriptor(path, "r")
				if err == nil && file != nil {
					file.Close()
					fileExists = true
					logrus.Debugf("✅ Файл найден через SAF: %s", path)
				} else {
					logrus.Debugf("⚠️ Файл не найден через SAF: %s (err: %v)", path, err)
				}
			}
		} else if _, err := os.Stat(path); err == nil {
			fileExists = true
			logrus.Debugf("✅ Файл найден через os.Stat: %s", path)
		} else {
			logrus.Debugf("⚠️ Файл не найден через os.Stat: %s (err: %v)", path, err)
		}

		if !fileExists {
			logrus.Warnf("⚠️ Пропускаем образ %s: файл не существует по пути %s", name, path)
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

	logrus.Infof("📂 Загружено %d пользовательских образов из preferences", len(dw.userImages))
}

// removeUserImage удаляет пользовательский образ из списка.
func (dw *DiskWidget) removeUserImage(driveIndex int) {
	if driveIndex < 0 || driveIndex >= len(dw.allDrives) {
		logrus.Warnf("⚠️ Неверный индекс образа: %d", driveIndex)
		return
	}

	drive := dw.allDrives[driveIndex]
	if drive.Source != "user" || drive.DiskInfo == nil {
		logrus.Warnf("⚠️ Попытка удалить не пользовательский образ: %s", drive.Name)
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
		logrus.Warnf("⚠️ Образ не найден в списке пользовательских образов: %s", drive.Name)
		return
	}

	if dw.window != nil {
		fyne.Do(func() {
			view.ShowConfirmYesLeft(
				i18n.Current.DeleteImageTitle,
				fmt.Sprintf(i18n.Current.DeleteImageConfirm, drive.Name),
				func(confirmed bool) {
					if confirmed {
						dw.userImages = append(dw.userImages[:userImageIndex], dw.userImages[userImageIndex+1:]...)
						logrus.Infof("🗑️ Образ удален: %s", drive.Name)
						dw.saveUserImagesToPreferences()
						dw.updateUIAsync(func() {
							dw.combineDrives()
							dw.requestDevicesRefresh()
						})
					}
				},
				dw.window,
			)
		})
	}
}

// formatPathForDisplay форматирует путь для красивого отображения.
func (dw *DiskWidget) formatPathForDisplay(path, fileName string) string {
	if strings.HasPrefix(path, "content://") {
		logrus.Debugf("🔍 formatPathForDisplay: исходный path=%s, fileName=%s", path, fileName)

		if strings.Contains(path, "downloads.documents/document") {
			displayPath := strings.TrimPrefix(path, "content://")
			if idx := strings.Index(displayPath, "/document/"); idx != -1 {
				displayPath = displayPath[idx+len("/document/"):]
			}
			displayPath = strings.ReplaceAll(displayPath, "%3A", ":")
			displayPath = strings.ReplaceAll(displayPath, "%2F", "/")

			if strings.Contains(displayPath, ":") && strings.Contains(displayPath, "/") {
				if idx := strings.Index(displayPath, ":"); idx != -1 {
					displayPath = displayPath[idx+1:]
				}
				logrus.Debugf("🔍 formatPathForDisplay (downloads с путем): результат=%s", displayPath)
				return displayPath
			}

			logrus.Debugf("🔍 formatPathForDisplay (downloads без пути): результат=%s", fileName)
			return fileName
		}

		displayPath := strings.TrimPrefix(path, "content://")
		if idx := strings.Index(displayPath, "/document/"); idx != -1 {
			displayPath = displayPath[idx+len("/document/"):]
		}

		logrus.Debugf("🔍 formatPathForDisplay: после удаления document=%s", displayPath)

		displayPath = strings.ReplaceAll(displayPath, "%3A", ":")
		displayPath = strings.ReplaceAll(displayPath, "%2F", "/")
		displayPath = strings.ReplaceAll(displayPath, "%20", " ")
		displayPath = strings.ReplaceAll(displayPath, "%2B", "+")
		displayPath = strings.ReplaceAll(displayPath, "%40", "@")

		logrus.Debugf("🔍 formatPathForDisplay: после декодирования=%s", displayPath)

		if idx := strings.Index(displayPath, ":"); idx != -1 {
			displayPath = displayPath[idx+1:]
		}

		if displayPath == "" || !strings.Contains(displayPath, "/") {
			logrus.Debugf("🔍 formatPathForDisplay: файл в корне, результат=%s", fileName)
			return fileName
		}

		logrus.Debugf("🔍 formatPathForDisplay: результат=%s", displayPath)
		return displayPath
	}

	return path
}

// handleUploadImage обрабатывает загрузку образа на устройство.
func (dw *DiskWidget) handleUploadImage(driveIndex int) {
	if driveIndex < 0 || driveIndex >= len(dw.allDrives) {
		logrus.Warnf("⚠️ Неверный индекс образа: %d", driveIndex)
		return
	}

	drive := dw.allDrives[driveIndex]
	if drive.Source != "user" || drive.DiskInfo == nil {
		logrus.Warnf("⚠️ Попытка загрузить не пользовательский образ: %s", drive.Name)
		return
	}

	if dw.usbClient == nil {
		logrus.Warn("⚠️ USB клиент не инициализирован")
		if dw.window != nil {
			dialog.ShowError(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		return
	}

	if dw.window != nil {
		fyne.Do(func() {
			view.ShowConfirmYesLeft(
				i18n.Current.UploadImageTitle,
				fmt.Sprintf(i18n.Current.UploadImageConfirm, drive.Name),
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

// uploadImageToDevice выполняет загрузку образа на устройство.
func (dw *DiskWidget) uploadImageToDevice(drive DriveItem) {
	logrus.Infof("📤 Начало загрузки образа на устройство: %s", drive.Name)

	var fileReader *os.File
	var err error

	if runtime.GOOS == "android" && strings.HasPrefix(drive.DiskInfo.Path, "content://") {
		fileReader, err = dw.safHelper.OpenFileDescriptor(drive.DiskInfo.Path, "r")
		if err != nil {
			dw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorOpeningFileForUpload, err))
			return
		}
	} else {
		fileReader, err = os.Open(drive.DiskInfo.Path)
		if err != nil {
			dw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorOpeningFileForUpload, err))
			return
		}
	}
	defer fileReader.Close()

	uploadPath := drive.DiskInfo.Path

	dw.updateUIAsync(func() {
		dw.setUploadStateByPath(uploadPath, true, 0, 0)
		dw.refreshDriveItemByPath(uploadPath)
	})

	progressCallback := func(percent float64, current, total int64, speed float64, eta time.Duration) {
		logrus.Debugf("🔄 UI callback: %.1f%%, скорость: %.2f МБ/с", percent, speed)
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
			dialog.ShowInformation(i18n.Current.Success, fmt.Sprintf(i18n.Current.ImageUploadedSuccess, drive.Name), dw.window)
		}
	})

	dw.loadLocalDrives()
	logrus.Infof("✅ Образ успешно загружен на устройство: %s", drive.Name)
}

// handleDeleteImageFromDevice обрабатывает удаление образа с устройства.
func (dw *DiskWidget) handleDeleteImageFromDevice(driveIndex int) {
	if driveIndex < 0 || driveIndex >= len(dw.allDrives) {
		logrus.Warnf("⚠️ Неверный индекс образа: %d", driveIndex)
		return
	}

	drive := dw.allDrives[driveIndex]
	if drive.Source != "api" && drive.Source != "local" {
		logrus.Warnf("⚠️ Попытка удалить образ неподдерживаемого типа: %s", drive.Name)
		return
	}

	if dw.usbClient == nil {
		logrus.Warn("⚠️ USB клиент не инициализирован")
		if dw.window != nil {
			dialog.ShowError(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		return
	}

	var filename string
	if drive.LocalDrive != nil {
		filename = drive.LocalDrive.Name
	} else if drive.DiskInfo != nil {
		filename = drive.DiskInfo.Name
	} else {
		filename = drive.Name
	}

	if dw.window != nil {
		fyne.Do(func() {
			view.ShowConfirmYesLeft(
				i18n.Current.DeleteImageTitle,
				fmt.Sprintf(i18n.Current.DeleteImageFromDeviceConfirm, drive.Name),
				func(confirmed bool) {
					if confirmed {
						go dw.deleteImageFromDevice(filename, drive.Name)
					}
				},
				dw.window,
			)
		})
	}
}

// deleteImageFromDevice выполняет удаление образа с устройства.
func (dw *DiskWidget) deleteImageFromDevice(filename string, displayName string) {
	logrus.Infof("🗑️ Начало удаления образа с устройства: %s", filename)

	err := dw.usbClient.DeleteISO(filename)
	if err != nil {
		dw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorDeletingImage, err))
		return
	}

	dw.updateUIAsync(func() {
		if dw.window != nil {
			dialog.ShowInformation(i18n.Current.Success, fmt.Sprintf(i18n.Current.ImageDeletedSuccess, displayName), dw.window)
		}
	})

	dw.loadLocalDrives()
	logrus.Infof("✅ Образ успешно удален с устройства: %s", filename)
}
