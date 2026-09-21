//go:build usbpass_gousb && linux

// gipprobe checks whether an Xbox One (GIP) pad announces itself again after a
// USB reset. A pad that was already initialised by another host driver (the
// Linux kernel's xpad) never sends its Announce (cmd 0x02) again, so a Windows
// importer's xboxgip driver waits for it forever; a reset restarts the pad's
// GIP state machine.
//
//	go run -tags usbpass_gousb ./cmd/gipprobe 1532:0a29
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/google/gousb"
)

func readFor(in *gousb.InEndpoint, d time.Duration, label string) (announce bool) {
	fmt.Printf("== %s: reading %s\n", label, d)
	deadline := time.Now().Add(d)
	buf := make([]byte, 64)
	n := 0
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		got, err := in.ReadContext(ctx, buf)
		cancel()
		if got > 0 {
			n++
			if n <= 12 {
				fmt.Printf("   IN %s\n", hex.EncodeToString(buf[:got]))
			}
			if buf[0] == 0x02 {
				announce = true
			}
		} else if err != nil && ctx.Err() == nil {
			fmt.Printf("   read error: %v\n", err)
			break
		}
	}
	fmt.Printf("   %d packets, announce(0x02)=%v\n", n, announce)
	return announce
}

func main() {
	var vid, pid uint16
	if len(os.Args) < 2 {
		vid, pid = 0x1532, 0x0a29
	} else if _, err := fmt.Sscanf(os.Args[1], "%x:%x", &vid, &pid); err != nil {
		fmt.Println("usage: gipprobe VID:PID")
		os.Exit(2)
	}
	ctx := gousb.NewContext()
	defer ctx.Close()
	dev, err := ctx.OpenDeviceWithVIDPID(gousb.ID(vid), gousb.ID(pid))
	if err != nil || dev == nil {
		fmt.Println("open:", err, "(is the pad free? unmount it in the client first)")
		os.Exit(1)
	}
	defer dev.Close()
	_ = dev.SetAutoDetach(true)

	claim := func() (*gousb.InEndpoint, *gousb.OutEndpoint, func()) {
		cfg, err := dev.Config(1)
		if err != nil {
			fmt.Println("config:", err)
			os.Exit(1)
		}
		intf, err := cfg.Interface(0, 0)
		if err != nil {
			fmt.Println("interface:", err)
			os.Exit(1)
		}
		in, err := intf.InEndpoint(1)
		if err != nil {
			fmt.Println("in ep:", err)
			os.Exit(1)
		}
		out, _ := intf.OutEndpoint(1)
		return in, out, func() { intf.Close(); cfg.Close() }
	}

	in, _, closeAll := claim()
	readFor(in, 3*time.Second, "A baseline (pad state as handed over)")

	// gousb refuses to reset while a configuration is open.
	closeAll()
	fmt.Println("== Reset()")
	t := time.Now()
	if err := dev.Reset(); err != nil {
		fmt.Printf("   reset error: %v\n", err)
	} else {
		fmt.Printf("   reset ok in %s\n", time.Since(t).Round(time.Millisecond))
	}
	in, _, closeAll = claim()
	readFor(in, 8*time.Second, "B after Reset + re-claim")
	closeAll()
}
