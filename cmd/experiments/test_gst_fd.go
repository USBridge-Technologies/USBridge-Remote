package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
	"github.com/godbus/dbus/v5"
)

func main() {
	conn, err := dbus.SessionBus()
	if err != nil { panic(err) }
	signals := make(chan *dbus.Signal, 100)
	conn.Signal(signals)
	conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.portal.Request"), dbus.WithMatchMember("Response"))
	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	
	waitForResponse := func(reqPath dbus.ObjectPath) (map[string]dbus.Variant, error) {
		for {
			select {
			case sig := <-signals:
				if sig.Path == reqPath {
					if sig.Body[0].(uint32) != 0 { return nil, fmt.Errorf("cancelled") }
					return sig.Body[1].(map[string]dbus.Variant), nil
				}
			case <-time.After(10 * time.Second): return nil, fmt.Errorf("timeout")
			}
		}
	}

	fmt.Println("1. CreateSession")
	var reqCreate dbus.ObjectPath
	obj.Call("org.freedesktop.portal.RemoteDesktop.CreateSession", 0, map[string]dbus.Variant{"session_handle_token": dbus.MakeVariant("t1"), "handle_token": dbus.MakeVariant("r1")}).Store(&reqCreate)
	res, _ := waitForResponse(reqCreate)
	sessionHandle := dbus.ObjectPath(res["session_handle"].Value().(string))

	fmt.Println("2. SelectSources")
	var reqSources dbus.ObjectPath
	obj.Call("org.freedesktop.portal.ScreenCast.SelectSources", 0, sessionHandle, map[string]dbus.Variant{"handle_token": dbus.MakeVariant("r2"), "types": dbus.MakeVariant(uint32(1)), "multiple": dbus.MakeVariant(false)}).Store(&reqSources)
	waitForResponse(reqSources)

	fmt.Println("3. Start")
	var reqStart dbus.ObjectPath
	obj.Call("org.freedesktop.portal.RemoteDesktop.Start", 0, sessionHandle, "", map[string]dbus.Variant{"handle_token": dbus.MakeVariant("r3")}).Store(&reqStart)
	resStart, _ := waitForResponse(reqStart)
	
	var nodeID uint32
	fmt.Sscanf(fmt.Sprintf("%v", resStart["streams"].Value()), "[[%d map[", &nodeID)
	fmt.Printf("Node ID: %d\n", nodeID)

	fmt.Println("4. OpenPipeWireRemote")
	var pipewireFd dbus.UnixFD
	obj.Call("org.freedesktop.portal.ScreenCast.OpenPipeWireRemote", 0, sessionHandle, map[string]dbus.Variant{}).Store(&pipewireFd)
	fmt.Printf("PipeWire FD: %d\n", pipewireFd)

	fmt.Println("5. Testing pw-dump with FD...")
	cmd := exec.Command("pw-dump")
	cmd.Env = append(os.Environ(), fmt.Sprintf("PIPEWIRE_REMOTE=%d", 3))
	cmd.ExtraFiles = append(cmd.ExtraFiles, os.NewFile(uintptr(pipewireFd), "pipewire-fd"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		fmt.Printf("pw-dump failed: %v\n", err)
	}
}
