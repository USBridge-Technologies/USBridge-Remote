//go:build darwin

package main

import (
	"context"
	"fmt"
	"time"

	"usbridge-client/internal/usbpass"
)

func main() {
	ed := usbpass.NewExportedFromVIDPID("", 0x056a, 0x0374)
	if err := usbpass.TryClaimGousb(ed); err != nil {
		fmt.Println("claim error:", err)
		return
	}
	defer ed.Backend.Close()

	fmt.Printf("device desc: % x\n", ed.DeviceDesc)
	fmt.Printf("config desc: % x\n", ed.ConfigDesc)

	// Simulate what VHCI's interrupt-IN polling would do: repeatedly submit
	// a read on the endpoint and print whatever comes back within a short
	// window. Touch/hover the pen now.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		status, data := ed.Backend.HandleBulk(ctx, 0x81, true, 64, nil)
		cancel()
		if status == 0 && len(data) > 0 {
			fmt.Printf("report: % x\n", data)
		}
	}
}
