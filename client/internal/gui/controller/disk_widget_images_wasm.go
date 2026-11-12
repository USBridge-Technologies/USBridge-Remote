//go:build js && wasm

package controller

import (
	"fmt"
	"sync"
	"syscall/js"
	"time"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
)

var (
	wasmUploadMu    sync.Mutex
	wasmUploadFiles = make(map[string]js.Value)
)

func (dw *DiskWidget) handleAddImageWasm() {
	doc := js.Global().Get("document")
	if doc.IsUndefined() || doc.IsNull() {
		dw.imagePickerInFlight.Store(false)
		return
	}

	input := doc.Call("createElement", "input")
	input.Set("type", "file")
	input.Set("accept", ".iso,.img,.bin,.vmdk,.vdi,.qcow,.qcow2,.raw,.vmi")
	input.Get("style").Set("display", "none")
	doc.Get("body").Call("appendChild", input)

	var onChange js.Func
	onChange = js.FuncOf(func(this js.Value, args []js.Value) any {
		defer func() {
			doc.Get("body").Call("removeChild", input)
			onChange.Release()
			dw.imagePickerInFlight.Store(false)
		}()

		files := input.Get("files")
		if files.IsUndefined() || files.IsNull() || files.Get("length").Int() == 0 {
			return nil
		}

		file := files.Index(0)
		fileName := file.Get("name").String()
		fileSize := int64(file.Get("size").Float())

		wasmUploadMu.Lock()
		wasmUploadFiles[fileName] = file
		wasmUploadMu.Unlock()

		logrus.Infof("📂 [WASM] Selected file: %s (%.2f MB)", fileName, float64(fileSize)/(1024*1024))

		fyne.Do(func() {
			dw.handleSelectedImage(selectedImage{
				FileName: fileName,
				URI:      "wasm://" + fileName,
				Size:     fileSize,
			})
		})

		return nil
	})

	input.Call("addEventListener", "change", onChange)
	input.Call("click")
}

func (dw *DiskWidget) uploadImageToDeviceWasm(drive DriveItem) {
	wasmUploadMu.Lock()
	file, ok := wasmUploadFiles[drive.DiskInfo.Name]
	wasmUploadMu.Unlock()

	if !ok || file.IsUndefined() || file.IsNull() {
		dw.showErrorAsync(fmt.Errorf("File reference not found in browser. Please click '+ Add Image' to select the file again."))
		return
	}

	client := dw.usbClient
	if client == nil {
		dw.showErrorAsync(fmt.Errorf("USB client not connected"))
		return
	}

	uploadPath := drive.DiskInfo.Path
	fileName := drive.DiskInfo.Name
	fileSize := drive.DiskInfo.Size

	dw.updateUIAsync(func() {
		dw.setUploadStateByPath(uploadPath, true, 0, 0)
		dw.refreshDriveItemByPath(uploadPath)
	})

	baseURL := client.BaseURL()
	uploadEndpoint := "/api/iso/upload"
	fullURL := baseURL + uploadEndpoint
	ts, sig := client.ComputeUploadHMAC(uploadEndpoint)
	apiKey := client.APIKey()

	doneCh := make(chan error, 1)

	var lastLoaded float64
	lastTime := time.Now()

	xhr := js.Global().Get("XMLHttpRequest").New()
	formData := js.Global().Get("FormData").New()
	formData.Call("append", "file", file, fileName)

	xhr.Call("open", "POST", fullURL, true)
	if sig != "" {
		xhr.Call("setRequestHeader", "X-Auth-Signature", sig)
		xhr.Call("setRequestHeader", "X-Auth-Timestamp", ts)
	}
	if apiKey != "" {
		xhr.Call("setRequestHeader", "Authorization", "Bearer "+apiKey)
	}
	if fileSize > 0 {
		xhr.Call("setRequestHeader", "X-File-Size", fmt.Sprintf("%d", fileSize))
	}

	onProgress := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			e := args[0]
			if e.Get("lengthComputable").Bool() {
				loaded := e.Get("loaded").Float()
				total := e.Get("total").Float()
				now := time.Now()
				dt := now.Sub(lastTime).Seconds()
				var speed float64
				if dt >= 0.5 {
					speed = (loaded - lastLoaded) / dt
					lastLoaded = loaded
					lastTime = now
				}
				percent := 0.0
				if total > 0 {
					percent = (loaded / total) * 100.0
				}
				dw.updateUIAsync(func() {
					dw.setUploadStateByPath(uploadPath, true, percent, speed)
					dw.refreshDriveItemByPath(uploadPath)
				})
			}
		}
		return nil
	})
	defer onProgress.Release()

	onLoad := js.FuncOf(func(this js.Value, args []js.Value) any {
		status := xhr.Get("status").Int()
		if status >= 200 && status < 300 {
			doneCh <- nil
		} else {
			text := xhr.Get("responseText").String()
			doneCh <- fmt.Errorf("HTTP %d: %s", status, text)
		}
		return nil
	})
	defer onLoad.Release()

	onError := js.FuncOf(func(this js.Value, args []js.Value) any {
		doneCh <- fmt.Errorf("network error during ISO upload")
		return nil
	})
	defer onError.Release()

	xhr.Get("upload").Call("addEventListener", "progress", onProgress)
	xhr.Call("addEventListener", "load", onLoad)
	xhr.Call("addEventListener", "error", onError)

	logrus.Infof("📤 [WASM] Starting XHR upload of %s to %s", fileName, fullURL)
	xhr.Call("send", formData)

	go func() {
		err := <-doneCh

		dw.updateUIAsync(func() {
			dw.setUploadStateByPath(uploadPath, false, 0, 0)
			dw.refreshDriveItemByPath(uploadPath)
		})

		if err != nil {
			logrus.Errorf("❌ [WASM] Upload failed: %v", err)
			dw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorUploadingImage, err))
			return
		}

		wasmUploadMu.Lock()
		delete(wasmUploadFiles, fileName)
		wasmUploadMu.Unlock()

		logrus.Infof("✅ [WASM] Upload succeeded: %s", fileName)
		dw.updateUIAsync(func() {
			if dw.window != nil {
				view.ShowInfoDialog(i18n.Current.Success, fmt.Sprintf(i18n.Current.ImageUploadedSuccess, drive.Name), dw.window)
			}
		})

		dw.loadLocalDrives()
	}()
}
