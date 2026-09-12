package main

import (
	"fmt"
	"os"
	"time"

	"usbridge-client/internal/models"
	"usbridge-client/internal/usbpass"
)

func main() {
	devs, err := usbpass.ListLocal()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var pick *models.USBPassthroughDevice
	for i := range devs {
		d := &devs[i]
		if d.VID == "1532" && d.PID == "007B" {
			pick = d
			break
		}
	}
	if pick == nil {
		fmt.Fprintln(os.Stderr, "Razer mouse 1532:007B not found")
		os.Exit(1)
	}
	fmt.Printf("export %s %s:%s %s\n", pick.BusID, pick.VID, pick.PID, pick.Description)
	if _, err := usbpass.StartSession("127.0.0.1:3240", []models.USBPassthroughDevice{*pick}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer usbpass.StopSession()
	fmt.Println("USB/IP export up 127.0.0.1:3240 (descriptor-only, no claim)")
	time.Sleep(20 * time.Second)
}
