// Command usbpasslab drives the USB passthrough session from the terminal,
// without the GUI: claim -> USB/IP export -> stop, optionally in a loop.
// Build with -tags usbpass_gousb, otherwise the claim is a no-op stub.
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"usbridge-client/internal/models"
	"usbridge-client/internal/usbpass"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:3240", "USB/IP export listen address")
	vid := flag.String("vid", "24a9", "vendor id (hex)")
	pid := flag.String("pid", "205a", "product id (hex)")
	busid := flag.String("busid", "2-3", "Linux USB/IP busid")
	hold := flag.Duration("hold", 0, "stop the session after this delay (0 = wait for SIGINT)")
	cycles := flag.Int("cycles", 1, "start/stop cycles to run")
	selftest := flag.Bool("selftest", false, "run a SCSI INQUIRY through the live backend right after claim")
	flag.Parse()

	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true, TimestampFormat: "15:04:05.000"})

	devs := []models.USBPassthroughDevice{{
		BusID:       *busid,
		InstanceID:  *busid,
		VID:         *vid,
		PID:         *pid,
		Description: "usbpasslab",
	}}

	for c := 1; c <= *cycles; c++ {
		fmt.Printf("\n===== cycle %d/%d =====\n", c, *cycles)
		report(*busid, "before start")
		start := time.Now()
		sess, err := usbpass.StartSession(*addr, devs)
		if err != nil {
			fmt.Printf("!! StartSession failed after %s: %v\n", time.Since(start).Round(time.Millisecond), err)
			report(*busid, "after failed start")
			os.Exit(1)
		}
		fmt.Printf(">> exported on %s in %s\n", *addr, time.Since(start).Round(time.Millisecond))
		report(*busid, "after start")

		if *selftest {
			inquiry(sess)
		}

		if *hold > 0 {
			time.Sleep(*hold)
		} else {
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
			fmt.Println(">> holding session, Ctrl-C to stop")
			<-sig
		}

		stop := time.Now()
		usbpass.StopSession()
		fmt.Printf(">> StopSession returned in %s\n", time.Since(stop).Round(time.Millisecond))
		report(*busid, "after stop")
	}
}

// inquiry runs one Bulk-Only Transport SCSI INQUIRY straight against the live
// backend: CBW out, 36 bytes in, CSW in. It separates a broken libusb claim
// from a broken USB/IP conversation with the Windows side.
func inquiry(sess *usbpass.Session) {
	devs := sess.Devices()
	if len(devs) == 0 || devs[0].Backend == nil {
		fmt.Println("!! selftest: no live backend")
		return
	}
	d := devs[0]
	epIn, epOut := bulkEndpoints(d.ConfigDesc)
	fmt.Printf("-- selftest endpoints: IN=%#02x OUT=%#02x\n", epIn, epOut)
	if epIn == 0 || epOut == 0 {
		fmt.Println("!! selftest: no bulk endpoints in config descriptor")
		return
	}

	cbw := make([]byte, 31)
	copy(cbw, "USBC")
	binary.LittleEndian.PutUint32(cbw[4:], 0x11223344)
	binary.LittleEndian.PutUint32(cbw[8:], 36)
	cbw[12] = 0x80 // data direction: device to host
	cbw[14] = 6    // CDB length
	copy(cbw[15:], []byte{0x12, 0x00, 0x00, 0x00, 36, 0x00})

	ctx := context.Background()
	st, _ := d.Backend.HandleBulk(ctx, epOut, false, 0, cbw)
	fmt.Printf("-- selftest CBW  out: status=%d\n", st)
	st, data := d.Backend.HandleBulk(ctx, epIn, true, 36, nil)
	fmt.Printf("-- selftest data in : status=%d len=%d %q\n", st, len(data), printable(data))
	st, csw := d.Backend.HandleBulk(ctx, epIn, true, 13, nil)
	fmt.Printf("-- selftest CSW  in : status=%d len=%d raw=% x\n", st, len(csw), csw)

	writeReadBack(ctx, d, epIn, epOut)
}

// writeReadBack round-trips one 512-byte sector through WRITE(10)/READ(10)
// straight against the live backend — no filesystem, no Windows, no
// USB/IP, isolating whether this client+device combination can do a
// reliable write at all. LBA 5,000,000 is deep into the data area (disk is
// tens of millions of sectors), nowhere near the MBR or a FAT32 volume's
// boot sector / FAT tables, so this is safe to run against an already
// partitioned/formatted stick without corrupting it.
func writeReadBack(ctx context.Context, d *usbpass.ExportedDevice, epIn, epOut uint8) {
	const lba = 5000000
	pattern := make([]byte, 512)
	for i := range pattern {
		pattern[i] = byte(i)
	}

	doCmd := func(label string, cdb []byte, dataLen uint32, dirIn bool, out []byte) (ok bool, in []byte) {
		cbw := make([]byte, 31)
		copy(cbw, "USBC")
		binary.LittleEndian.PutUint32(cbw[4:], 0xaabbccdd)
		binary.LittleEndian.PutUint32(cbw[8:], dataLen)
		if dirIn {
			cbw[12] = 0x80
		}
		cbw[14] = byte(len(cdb))
		copy(cbw[15:], cdb)

		st, _ := d.Backend.HandleBulk(ctx, epOut, false, 0, cbw)
		if st != 0 {
			fmt.Printf("-- %s: CBW out failed status=%d\n", label, st)
			return false, nil
		}
		if dirIn {
			st, in = d.Backend.HandleBulk(ctx, epIn, true, int(dataLen), nil)
			if st != 0 {
				fmt.Printf("-- %s: data in failed status=%d\n", label, st)
				return false, nil
			}
		} else {
			st, _ = d.Backend.HandleBulk(ctx, epOut, false, len(out), out)
			if st != 0 {
				fmt.Printf("-- %s: data out failed status=%d\n", label, st)
				return false, nil
			}
		}
		st, csw := d.Backend.HandleBulk(ctx, epIn, true, 13, nil)
		if st != 0 || len(csw) != 13 || csw[12] != 0 {
			fmt.Printf("-- %s: CSW failed status=%d csw=% x\n", label, st, csw)
			return false, in
		}
		fmt.Printf("-- %s: OK\n", label)
		return true, in
	}

	lbaU := uint32(lba)
	write10 := []byte{0x2a, 0, byte(lbaU >> 24), byte(lbaU >> 16), byte(lbaU >> 8), byte(lbaU), 0, 0, 1, 0}
	if ok, _ := doCmd("WRITE(10) lba=5000000", write10, 512, false, pattern); !ok {
		fmt.Println("!! write-readback: WRITE failed, stopping")
		return
	}
	read10 := []byte{0x28, 0, byte(lbaU >> 24), byte(lbaU >> 16), byte(lbaU >> 8), byte(lbaU), 0, 0, 1, 0}
	ok, back := doCmd("READ(10)  lba=5000000", read10, 512, true, nil)
	if !ok {
		fmt.Println("!! write-readback: READ failed, stopping")
		return
	}
	if len(back) == 512 && string(back) == string(pattern) {
		fmt.Println("== write-readback: PASS (data matches byte-for-byte)")
	} else {
		fmt.Printf("== write-readback: MISMATCH (got %d bytes)\n", len(back))
	}
}

func bulkEndpoints(cfg []byte) (in, out uint8) {
	for i := 0; i+2 <= len(cfg); {
		l := int(cfg[i])
		if l < 2 || i+l > len(cfg) {
			break
		}
		if cfg[i+1] == 0x05 && l >= 6 && cfg[i+3]&0x03 == 0x02 { // ENDPOINT, bulk
			if addr := cfg[i+2]; addr&0x80 != 0 {
				if in == 0 {
					in = addr
				}
			} else if out == 0 {
				out = addr
			}
		}
		i += l
	}
	return in, out
}

func printable(b []byte) string {
	out := make([]rune, 0, len(b))
	for _, c := range b {
		if c >= 0x20 && c < 0x7f {
			out = append(out, rune(c))
		} else {
			out = append(out, '.')
		}
	}
	return string(out)
}

// report prints the bus-level state that tells a healthy stick from a wedged
// one: which driver owns the interface and whether it still answers control
// transfers at all.
func report(busid, when string) {
	drv := "none"
	if l, err := os.Readlink(filepath.Join("/sys/bus/usb/devices", busid+":1.0", "driver")); err == nil {
		drv = filepath.Base(l)
	}
	fmt.Printf("-- %-18s driver=%-11s descriptors=%s block=%s\n", when, drv, answers(), blockDev())
}

func answers() string {
	cmd := exec.Command("timeout", "6", "lsusb", "-v", "-d", "24a9:205a")
	if err := cmd.Run(); err != nil {
		return "NO(wedged)"
	}
	return "ok"
}

func blockDev() string {
	out, err := exec.Command("lsblk", "-ndo", "NAME,MODEL").Output()
	if err != nil {
		return "?"
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(strings.ToUpper(line), "PHILIPS") {
			return strings.Fields(line)[0]
		}
	}
	return "none"
}
