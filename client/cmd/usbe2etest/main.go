//go:build usbpass_gousb

// usbe2etest drives a real end-to-end USB passthrough cycle against a live
// rust-shine agent: local USB/IP export (usbpass.StartSession) + the real
// AES attach handshake (usbpass.Attach) to the Windows box, exactly what the
// GUI's mountUSBPassthrough does — just without a human clicking anything,
// so the whole client/agent path can be burned in unattended.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"usbridge-client/internal/models"
	"usbridge-client/internal/usbpass"
)

func main() {
	agentAddr := flag.String("agent", "192.168.200.254:8090", "agent AES control-plane address")
	secret := flag.String("secret", "", "agent pairing master_key")
	vid := flag.String("vid", "24a9", "vendor id (hex)")
	pid := flag.String("pid", "205a", "product id (hex)")
	busid := flag.String("busid", "2-3", "Linux USB/IP busid")
	hold := flag.Duration("hold", 20*time.Second, "how long to keep the session attached")
	flag.Parse()

	if *secret == "" {
		fmt.Fprintln(os.Stderr, "-secret is required (agent's paired master_key)")
		os.Exit(2)
	}

	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true, TimestampFormat: "15:04:05.000"})

	devs := []models.USBPassthroughDevice{{
		BusID:       *busid,
		InstanceID:  *busid,
		VID:         *vid,
		PID:         *pid,
		Description: "usbe2etest",
	}}

	fmt.Println(">> StartSession (local USB/IP export on :3240)")
	if _, err := usbpass.StartSession("0.0.0.0:3240", devs); err != nil {
		fmt.Fprintln(os.Stderr, "!! StartSession failed:", err)
		os.Exit(1)
	}
	defer usbpass.StopSession()

	fmt.Printf(">> Attach to agent %s (VID:PID=%s:%s busid=%s)\n", *agentAddr, *vid, *pid, *busid)
	if err := usbpass.Attach(usbpass.AttachOptions{
		AgentAddr:     *agentAddr,
		Secret:        *secret,
		InstanceID:    *busid,
		USBIPBusID:    *busid,
		VID:           *vid,
		PID:           *pid,
		ExportService: "3240",
	}); err != nil {
		fmt.Fprintln(os.Stderr, "!! Attach failed:", err)
		os.Exit(1)
	}
	defer usbpass.StopAttach()

	fmt.Printf(">> attached, holding for %s (Windows should now be enumerating the disk)...\n", *hold)
	time.Sleep(*hold)
	fmt.Println(">> done holding, tearing down")
}
