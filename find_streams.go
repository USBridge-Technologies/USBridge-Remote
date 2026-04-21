package main

import (
	"fmt"
	"github.com/godbus/dbus/v5"
	"time"
	"os"
)

func waitForResponse(signals chan *dbus.Signal, reqPath dbus.ObjectPath) (map[string]dbus.Variant, error) {
	for {
		select {
		case sig := <-signals:
			if sig.Path == reqPath {
				code := sig.Body[0].(uint32)
				if code != 0 {
					return nil, fmt.Errorf("error code %d", code)
				}
				return sig.Body[1].(map[string]dbus.Variant), nil
			}
		case <-time.After(15 * time.Second):
			return nil, fmt.Errorf("timeout waiting for %s", reqPath)
		}
	}
}

func main() {
	conn, _ := dbus.SessionBus()
	signals := make(chan *dbus.Signal, 100)
	conn.Signal(signals)
	conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.portal.Request"), dbus.WithMatchMember("Response"))

	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	
	fmt.Println("🔍 Experimenting with Portal to find multiple streams...")

	// 1. CreateSession
	var reqCreate dbus.ObjectPath
	obj.Call("org.freedesktop.portal.RemoteDesktop.CreateSession", 0, map[string]dbus.Variant{
		"session_handle_token": dbus.MakeVariant("stream_test"),
		"handle_token":         dbus.MakeVariant("req_create"),
	}).Store(&reqCreate)
	res, _ := waitForResponse(signals, reqCreate)
	sessionHandle := dbus.ObjectPath(res["session_handle"].Value().(string))

	// 2. SelectSources with MULTIPLE=TRUE
	fmt.Println("📡 Calling SelectSources with multiple=true and cursor_mode=4...")
	var reqSources dbus.ObjectPath
	obj.Call("org.freedesktop.portal.ScreenCast.SelectSources", 0, sessionHandle, map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant("req_sources"),
		"types":        dbus.MakeVariant(uint32(1)), // Monitor
		"multiple":     dbus.MakeVariant(true),      // ПОПРОБУЕМ TRUE
		"cursor_mode":  dbus.MakeVariant(uint32(4)), // Metadata
	}).Store(&reqSources)
	waitForResponse(signals, reqSources)

	// 3. Start
	fmt.Println("🚀 Calling Start... PLEASE ALLOW ACCESS IN DIALOG!")
	var reqStart dbus.ObjectPath
	obj.Call("org.freedesktop.portal.RemoteDesktop.Start", 0, sessionHandle, "", map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant("req_start"),
	}).Store(&reqStart)
	
	startRes, err := waitForResponse(signals, reqStart)
	if err != nil {
		fmt.Printf("❌ Start failed: %v\n", err)
		return
	}

	if streams, ok := startRes["streams"]; ok {
		fmt.Printf("\n✅ PORTAL RETURNED STREAMS: %+v\n", streams.Value())
		
		// Пытаемся понять, что это за ноды
		// Обычно это массив структур (u a{sv})
	} else {
		fmt.Println("❌ No streams in response")
	}
}
