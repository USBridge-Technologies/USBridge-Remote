//go:build windows

// padsmoke streams from a paired GameStream host and drives one virtual
// gamepad over the stream's input channel (ENet), the way the client does,
// then checks on this machine through XInput that the host built the pad:
// with the host on the same machine, a USBridge-patched Sunshine or
// RustShine hands the pad to the USB broker, which plugs an Xbox 360
// controller into usbip-win2. It holds A and pushes the left stick, then
// releases them, and reports what XInput saw at each step.
//
//	go run -tags usbpass_gousb ./cmd/padsmoke -host 127.0.0.1
package main

import (
	"flag"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"

	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
)

// Moonlight's controller constants (moonlight-common-c Limelight.h).
const (
	ctypeXbox   = 0x01
	buttonA     = 0x1000
	allButtons  = 0x000FFFFF
	xinputA     = 0x1000
	maxXinputs  = 4
	errNotConn  = 1167
	stickPushed = 30000
)

type xinputGamepad struct {
	Buttons      uint16
	LeftTrigger  uint8
	RightTrigger uint8
	ThumbLX      int16
	ThumbLY      int16
	ThumbRX      int16
	ThumbRY      int16
}

type xinputState struct {
	PacketNumber uint32
	Gamepad      xinputGamepad
}

var xinputGetState = syscall.NewLazyDLL("xinput1_4.dll").NewProc("XInputGetState")

// pads reads every XInput slot; a missing slot is nil.
func pads() [maxXinputs]*xinputGamepad {
	var out [maxXinputs]*xinputGamepad
	for i := range maxXinputs {
		var st xinputState
		r, _, _ := xinputGetState.Call(uintptr(i), uintptr(unsafe.Pointer(&st)))
		if r == 0 {
			g := st.Gamepad
			out[i] = &g
		} else if r != errNotConn {
			logrus.Warnf("XInputGetState(%d) = %d", i, r)
		}
	}
	return out
}

func describe(p [maxXinputs]*xinputGamepad) string {
	s := ""
	for i, g := range p {
		if g == nil {
			s += fmt.Sprintf(" [%d] -", i)
		} else {
			s += fmt.Sprintf(" [%d] buttons=%#04x lx=%d", i, g.Buttons, g.ThumbLX)
		}
	}
	return s
}

// waitFor polls XInput until ok holds for some slot or the timeout runs out.
func waitFor(what string, timeout time.Duration, ok func(i int, g *xinputGamepad) bool) (int, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		p := pads()
		for i, g := range p {
			if g != nil && ok(i, g) {
				fmt.Printf("ok: %s (slot %d)\n", what, i)
				return i, true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Printf("FAIL: %s; XInput:%s\n", what, describe(pads()))
	return -1, false
}

func main() {
	host := flag.String("host", "127.0.0.1", "GameStream host")
	timeout := flag.Duration("timeout", 15*time.Second, "how long to wait for each step")
	flag.Parse()
	logrus.SetLevel(logrus.InfoLevel)

	before := pads()
	fmt.Printf("XInput before:%s\n", describe(before))

	m := service.NewMoonlightService(models.DefaultConfig())
	m.UpdateHost(*host)
	m.SetVideoMode(models.VideoModeH264)
	m.SetExpectedVideoSize(1280, 720)
	m.SetFPS(30)
	m.SetBitrate(5000)
	m.SetOnError(func(err error) { logrus.Errorf("stream error: %v", err) })
	if err := m.ConnectToMoonlight(); err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer m.Disconnect()
	time.Sleep(2 * time.Second)

	fail := false
	// The host builds the pad on arrival or with the first state.
	m.SendMoonlightControllerArrival(0, 1, ctypeXbox, allButtons, 0)
	m.SendMoonlightControllerEvent(0, 1, 0, 0, 0, 0, 0, 0, 0)
	slot, ok := waitFor("a new Xbox pad appeared", *timeout, func(i int, _ *xinputGamepad) bool { return before[i] == nil })
	if !ok {
		os.Exit(2)
	}

	press := func() { m.SendMoonlightControllerEvent(0, 1, buttonA, 0, 0, stickPushed, 0, 0, 0) }
	stop := make(chan struct{})
	go func() { // keep the state flowing, as a real client does
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
				press()
			}
		}
	}()
	if _, ok := waitFor("A held and left stick pushed", *timeout, func(i int, g *xinputGamepad) bool {
		return i == slot && g.Buttons&xinputA != 0 && g.ThumbLX > stickPushed/2
	}); !ok {
		fail = true
	}
	close(stop)

	m.SendMoonlightControllerEvent(0, 1, 0, 0, 0, 0, 0, 0, 0)
	if _, ok := waitFor("A released and stick centred", *timeout, func(i int, g *xinputGamepad) bool {
		return i == slot && g.Buttons == 0 && g.ThumbLX == 0
	}); !ok {
		fail = true
	}

	// Unplug: activeGamepadMask without the pad.
	m.SendMoonlightControllerEvent(0, 0, 0, 0, 0, 0, 0, 0, 0)
	gone := false
	for deadline := time.Now().Add(*timeout); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if pads()[slot] == nil {
			gone = true
			break
		}
	}
	if gone {
		fmt.Println("ok: pad unplugged")
	} else {
		fmt.Printf("FAIL: pad unplugged; XInput:%s\n", describe(pads()))
		fail = true
	}

	if fail {
		os.Exit(2)
	}
	fmt.Println("PASS")
}
