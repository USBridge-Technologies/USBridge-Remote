package main

import (
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
	
	optCreate := map[string]dbus.Variant{
		"session_handle_token": dbus.MakeVariant("usbridge_session"),
		"handle_token":         dbus.MakeVariant("usbridge_req_create"),
	}
	var reqCreate dbus.ObjectPath
	err = obj.Call("org.freedesktop.portal.RemoteDesktop.CreateSession", 0, optCreate).Store(&reqCreate)
	if err != nil {
		panic(err)
	}
	
	fmt.Printf("Request path: %s\n", reqCreate)

	select {
	case sig := <-signals:
		fmt.Printf("Got signal: %+v\n", sig)
	case <-time.After(5 * time.Second):
		fmt.Println("Timeout waiting for Response")
	}
}
