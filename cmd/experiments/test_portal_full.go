package main

import (
	"os/exec"
	"os"
	"fmt"
	"github.com/godbus/dbus/v5"
	"time"
)

func main() {
	conn, err := dbus.SessionBus()
	if err != nil {
		panic(err)
	}

	signals := make(chan *dbus.Signal, 100)
	conn.Signal(signals)

	err = conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.portal.Request"),
		dbus.WithMatchMember("Response"),
	)
	if err != nil {
		panic(err)
	}

	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	
	waitForResponse := func(reqPath dbus.ObjectPath) (map[string]dbus.Variant, error) {
		for {
			select {
			case sig := <-signals:
				if sig.Path == reqPath {
					fmt.Printf("Got response for %s: %+v\n", reqPath, sig.Body)
					code := sig.Body[0].(uint32)
					if code != 0 {
						return nil, fmt.Errorf("user cancelled or failed (code %d)", code)
					}
					return sig.Body[1].(map[string]dbus.Variant), nil
				}
			case <-time.After(10 * time.Second):
				return nil, fmt.Errorf("timeout waiting for %s", reqPath)
			}
		}
	}

	fmt.Println("1. CreateSession")
	optCreate := map[string]dbus.Variant{
		"session_handle_token": dbus.MakeVariant("test_session"),
		"handle_token":         dbus.MakeVariant("test_req_create"),
	}
	var reqCreate dbus.ObjectPath
	err = obj.Call("org.freedesktop.portal.RemoteDesktop.CreateSession", 0, optCreate).Store(&reqCreate)
	if err != nil {
		panic(err)
	}
	res, err := waitForResponse(reqCreate)
	if err != nil {
		panic(err)
	}
	sessionHandle := dbus.ObjectPath(res["session_handle"].Value().(string))
	fmt.Printf("Session Handle: %s\n", sessionHandle)

	fmt.Println("2. SelectSources")
	optSources := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant("test_req_sources"),
		"types":        dbus.MakeVariant(uint32(1)),
		"multiple":     dbus.MakeVariant(false),
	}
	var reqSources dbus.ObjectPath
	err = obj.Call("org.freedesktop.portal.ScreenCast.SelectSources", 0, sessionHandle, optSources).Store(&reqSources)
	if err != nil {
		panic(err)
	}
	_, err = waitForResponse(reqSources)
	if err != nil {
		panic(err)
	}

	fmt.Println("3. Start")
	optStart := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant("test_req_start"),
	}
	var reqStart dbus.ObjectPath
	err = obj.Call("org.freedesktop.portal.RemoteDesktop.Start", 0, sessionHandle, "", optStart).Store(&reqStart)
	if err != nil {
		panic(err)
	}
	resStart, err := waitForResponse(reqStart)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Start Response: %+v\n", resStart)
	
	fmt.Println("4. OpenPipeWireRemote via ScreenCast")
	optOpen := map[string]dbus.Variant{}
	var pipewireFd dbus.UnixFD
	err = obj.Call("org.freedesktop.portal.ScreenCast.OpenPipeWireRemote", 0, sessionHandle, optOpen).Store(&pipewireFd)
	if err != nil {
		fmt.Printf("OpenPipeWireRemote failed: %v\n", err)
		return
	}
	fmt.Printf("PipeWire FD: %d\n", pipewireFd)
	
	// Test the FD with gstreamer via os/exec to see if "target not found" happens natively
	fmt.Println("5. Testing GStreamer pipeline with FD")
	
	
	
	cmd := exec.Command("gst-launch-1.0", "pipewiresrc", "path=89", "!", "fakesink", "num-buffers=10")
	cmd.ExtraFiles = append(cmd.ExtraFiles, os.NewFile(uintptr(pipewireFd), "pipewire-fd"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	err = cmd.Run()
	if err != nil {
		fmt.Printf("GStreamer test failed: %v\n", err)
	} else {
		fmt.Println("GStreamer test succeeded!")
	}
}
