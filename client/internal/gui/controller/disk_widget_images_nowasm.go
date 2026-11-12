//go:build !js || !wasm

package controller

func (dw *DiskWidget) handleAddImageWasm() {}

func (dw *DiskWidget) uploadImageToDeviceWasm(drive DriveItem) {}
