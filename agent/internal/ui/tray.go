package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/godbus/dbus/v5"
	"github.com/sirupsen/logrus"

	"usbridge_agent/assets"
	"usbridge_agent/internal/autostart"
	"usbridge_agent/internal/ui/design"
)

// trayIconState is which composited tray-icon variant is currently applied.
// Tracked so setStatus only calls SetSystemTrayIcon on an actual change,
// rather than re-encoding/re-uploading a PNG on every 2s refresh tick.
type trayIconState int

const (
	trayIconIdle trayIconState = iota
	trayIconActive
	trayIconAttention
)

// trayController owns the tray icon/menu for a Window that has one attached
// -- see Window.attachTray. A nil *trayController (stored as Window.tray)
// means this session has no usable tray host; callers must fall back to the
// pre-tray "X really quits" behavior in that case (see linuxTrayAvailable's
// doc comment for why that fallback matters).
type trayController struct {
	owner *Window
	win   fyne.Window
	desk  desktop.App
	menu  *fyne.Menu

	statusItem    *fyne.MenuItem
	infoItem      *fyne.MenuItem
	openItem      *fyne.MenuItem
	restartItem   *fyne.MenuItem
	autostartItem *fyne.MenuItem
	quitItem      *fyne.MenuItem

	icons       map[trayIconState]fyne.Resource
	currentIcon trayIconState
	hintShown   bool
}

// attachTray wires a system tray icon + menu to win, RustDesk-style: current
// status at a glance, a quick way back to the main window, and Quit -- so
// SetCloseIntercept (see window.go) can hide the window instead of exiting
// the whole agent. Returns nil if this session has no usable tray host, in
// which case the caller must leave the old close-quits-the-app behavior in
// place -- otherwise a user on such a session would be left with an
// invisible, unquittable app.
func (w *Window) attachTray(win fyne.Window, quit func()) *trayController {
	deskApp, ok := w.app.(desktop.App)
	if !ok {
		return nil
	}
	if runtime.GOOS == "linux" && !linuxTrayAvailable() {
		logrus.Info("[ui] no system tray host detected on this session (GNOME needs the 'AppIndicator and KStatusNotifierItem Support' extension) -- close button will quit the app instead of minimizing to tray")
		return nil
	}

	t := &trayController{
		owner: w,
		win:   win,
		desk:  deskApp,
		icons: buildTrayIcons(),
	}

	t.statusItem = fyne.NewMenuItem("USBridge Agent", func() { t.showWindow() })
	t.statusItem.Disabled = true
	t.infoItem = fyne.NewMenuItem("", nil)
	t.infoItem.Disabled = true

	openItem := fyne.NewMenuItem(loc().TrayOpen, func() { t.showWindow() })
	t.openItem = openItem

	restartItem := fyne.NewMenuItem(loc().TrayRestart, func() {
		if w.token == nil {
			return
		}
		go func() {
			if err := w.token.RestartSunshine(); err != nil {
				logrus.WithError(err).Warn("tray: restart streaming failed")
			}
		}()
	})

	t.restartItem = restartItem

	t.autostartItem = fyne.NewMenuItem(autostartMenuLabel(), nil)
	t.autostartItem.Checked = autostart.IsEnabled()
	t.autostartItem.Action = func() { t.toggleAutostart() }

	quitItem := fyne.NewMenuItem(loc().TrayQuit, func() {
		if quit != nil {
			quit()
		}
		win.Close()
		w.app.Quit()
	})
	t.quitItem = quitItem
	quitItem.IsQuit = true

	t.menu = fyne.NewMenu("USBridge Agent",
		t.statusItem,
		t.infoItem,
		fyne.NewMenuItemSeparator(),
		openItem,
		restartItem,
		fyne.NewMenuItemSeparator(),
		t.autostartItem,
		fyne.NewMenuItemSeparator(),
		quitItem,
	)

	// Order matters: SetSystemTrayWindow installs its own
	// SetCloseIntercept(win.Hide) and wires a bare tray-icon click (not just
	// the menu) to reopen the window -- both worth keeping. window.go's
	// ShowAndRun overrides the close intercept again right after this call
	// returns, layering the one-time "still running in the tray" hint on
	// top of the same Hide behavior rather than replacing it.
	deskApp.SetSystemTrayIcon(t.icons[trayIconIdle])
	deskApp.SetSystemTrayMenu(t.menu)
	deskApp.SetSystemTrayWindow(win)

	return t
}

func (t *trayController) showWindow() {
	fyne.Do(func() {
		t.win.Show()
		t.win.RequestFocus()
	})
}

func (t *trayController) toggleAutostart() {
	next := !t.autostartItem.Checked
	go func() {
		var err error
		if next {
			err = autostart.Enable()
		} else {
			err = autostart.Disable()
		}
		if err != nil {
			logrus.WithError(err).Warn("tray: autostart toggle failed")
			return
		}
		fyne.Do(func() {
			if t.owner != nil && t.owner.autostartCheck != nil {
				t.owner.autostartCheck.Checked = next
				t.owner.autostartCheck.Refresh()
			}
			if t.owner != nil {
				t.owner.refreshAutostartChrome()
			} else {
				t.autostartItem.Checked = next
				t.autostartItem.Label = autostartMenuLabel()
				t.refreshMenu()
			}
		})
	}()
}

// refreshMenu re-renders the tray menu from its current *fyne.MenuItem
// field values. Needed (not just cosmetic) for the autostart checkbox: the
// underlying systray library only reads MenuItem.Checked once, at
// (re)build time, to decide whether to render a checkbox at all -- there is
// no live Check()/Uncheck() wiring for a value changed after the fact. A
// full Menu.Refresh() (which rebuilds every item) is what actually reflects
// a state flip.
func (t *trayController) applyLanguage() {
	if t == nil {
		return
	}
	c := loc()
	if t.openItem != nil {
		t.openItem.Label = c.TrayOpen
	}
	if t.restartItem != nil {
		t.restartItem.Label = c.TrayRestart
	}
	if t.autostartItem != nil {
		t.autostartItem.Label = autostartMenuLabel()
	}
	if t.quitItem != nil {
		t.quitItem.Label = c.TrayQuit
	}
	t.refreshMenu()
}

func (t *trayController) refreshMenu() {
	if t == nil || t.menu == nil {
		return
	}
	t.menu.Refresh()
}

// setStatus updates the tray's header/info text and, if it changed, the
// composited status icon. Called from Window.performRefresh so the tray
// reflects the same data already computed for the main window's Status
// panel -- no separate polling loop.
func (t *trayController) setStatus(header, info string, state trayIconState) {
	if t == nil {
		return
	}
	changed := false
	if t.statusItem.Label != header {
		t.statusItem.Label = header
		changed = true
	}
	if t.infoItem.Label != info {
		t.infoItem.Label = info
		changed = true
	}
	if changed {
		t.refreshMenu()
	}
	if state != t.currentIcon {
		t.currentIcon = state
		t.desk.SetSystemTrayIcon(t.icons[state])
	}
}

// notifyHiddenOnce shows a single "still running in the tray" hint the
// first time this run's window is minimized via the close button --
// without it, a user used to the old close-quits-the-app behavior has no
// obvious signal that the window merely disappeared rather than the whole
// agent exiting. Only fires once per process lifetime, not on every
// minimize.
func (t *trayController) notifyHiddenOnce() {
	if t == nil || t.hintShown || t.owner == nil || t.owner.app == nil {
		return
	}
	t.hintShown = true
	t.owner.app.SendNotification(fyne.NewNotification(
		loc().AppTitle,
		loc().TrayStillRunning,
	))
}

// linuxTrayAvailable reports whether a StatusNotifierItem host -- the
// freedesktop tray protocol fyne.io/systray speaks on Linux, which works on
// both X11 and Wayland compositors that implement it (KDE, XFCE,
// Sway+waybar/nwg-panel, and others) -- is actually reachable on this
// session. GNOME (X11 or Wayland) ships no such host by default; it needs
// the "AppIndicator and KStatusNotifierItem Support" (or newer "Tray Icons:
// Reloaded") extension. Without this check, minimizing to a tray icon
// nobody can see would strand the user with no way back into the window
// and no way to quit -- see attachTray's fallback.
func linuxTrayAvailable() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	var hasOwner bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&hasOwner)
	return err == nil && hasOwner
}

// buildTrayIcons composites the three status-dot variants onto
// assets.TrayIconPNG once at attach time, so setStatus only ever swaps
// between pre-rendered resources instead of re-encoding on every refresh.
func buildTrayIcons() map[trayIconState]fyne.Resource {
	base, err := png.Decode(bytes.NewReader(assets.TrayIconPNG))
	if err != nil {
		// Better a tray icon with no status dot than no tray icon at all.
		res := fyne.NewStaticResource("tray.png", assets.TrayIconPNG)
		return map[trayIconState]fyne.Resource{
			trayIconIdle:      res,
			trayIconActive:    res,
			trayIconAttention: res,
		}
	}
	return map[trayIconState]fyne.Resource{
		trayIconIdle:      fyne.NewStaticResource("tray-idle.png", encodeTrayIcon(base, nil)),
		trayIconActive:    fyne.NewStaticResource("tray-active.png", encodeTrayIcon(base, design.ColorBrandAccent)),
		trayIconAttention: fyne.NewStaticResource("tray-attention.png", encodeTrayIcon(base, design.ColorError)),
	}
}

// encodeTrayIcon draws dot (if non-nil) onto a copy of base and PNG-encodes
// the result.
func encodeTrayIcon(base image.Image, dot color.Color) []byte {
	bounds := base.Bounds()
	out := image.NewRGBA(bounds)
	draw.Draw(out, bounds, base, bounds.Min, draw.Src)
	if dot != nil {
		drawStatusDot(out, dot)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, out)
	return buf.Bytes()
}

// drawStatusDot paints a filled circle in the icon's bottom-right corner,
// roughly a third of the icon's shorter side -- big enough to read at the
// ~16-32px a systray host actually renders this at, without covering the
// whole glyph.
func drawStatusDot(img *image.RGBA, dot color.Color) {
	b := img.Bounds()
	size := b.Dx()
	if b.Dy() < size {
		size = b.Dy()
	}
	radius := size / 3
	if radius < 2 {
		radius = 2
	}
	cx := b.Max.X - radius
	cy := b.Max.Y - radius
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= radius*radius {
				img.Set(x, y, dot)
			}
		}
	}
}
