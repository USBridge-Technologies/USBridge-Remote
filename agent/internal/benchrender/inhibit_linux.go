//go:build linux

package benchrender

import (
	"log"

	"github.com/godbus/dbus/v5"
)

// inhibitIdle keeps the desktop from dimming or blanking the screen while the
// test window is up: nobody touches a benchmark's host, and KDE's idle dimming
// scales the very signal the stream captures. Held until the process exits.
func inhibitIdle() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("[bench-render] idle inhibit: %v", err)
		return
	}
	var cookie uint32
	err = conn.Object("org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver").
		Call("org.freedesktop.ScreenSaver.Inhibit", 0, WindowTitle, "streamer benchmark test window").Store(&cookie)
	if err != nil {
		log.Printf("[bench-render] idle inhibit: %v", err)
		conn.Close()
		return
	}
	// The inhibition lasts as long as this connection: keep it open.
}
