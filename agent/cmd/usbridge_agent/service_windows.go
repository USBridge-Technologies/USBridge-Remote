//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"usbridge_agent/internal/app"
	"usbridge_agent/internal/sasinput"
)

const serviceName = "USBridgeAgent"

func runMain(headless, tray bool, attach string) {
	isSvc, err := svc.IsWindowsService()
	if err != nil {
		log.Printf("failed to determine if we are running in an interactive session: %v", err)
		isSvc = false
	}
	if isSvc {
		// Best-effort: SendSAS (app.SendSAS -> sasinput) needs this policy
		// bit to do anything at all -- see EnsureServicesCanGenerateSAS's
		// doc comment. A failure here (e.g. registry access somehow
		// denied) shouldn't block the service from starting; it just means
		// a later SendSAS call silently does nothing, same as it already
		// does when the policy is unset.
		if err := sasinput.EnsureServicesCanGenerateSAS(); err != nil {
			log.Printf("could not enable SoftwareSASGeneration policy (Ctrl+Alt+Del injection on the lock screen may not work): %v", err)
		}
		err = svc.Run(serviceName, &agentService{})
		if err != nil {
			log.Fatalf("service execution failed: %v", err)
		}
		return
	}
	doStart(headless, tray, attach)
}

type agentService struct{}

func (m *agentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	// AcceptSessionChange is what lets Windows notify this service of
	// WTS_SESSION_LOGON/WTS_CONSOLE_CONNECT below -- see the SessionChange
	// case and app.NotifySessionChange's doc comment for why a LocalSystem
	// service needs this at all: it never automatically re-homes an
	// already-running gamestream-server child into a session that only
	// becomes interactive after this service itself started.
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptSessionChange
	changes <- svc.Status{State: svc.StartPending}

	// Start headless mode in background
	go doStart(true, false, "")

	// Covers "service (re)started while a user is already logged in" -- the
	// one case the SessionChange handling below can't catch on its own,
	// since WTS_SESSION_LOGON/WTS_CONSOLE_CONNECT only fire on a *future*
	// login/session switch. Polls briefly for doStart's engine to finish
	// enough of its startup to have an admin socket at all (see
	// app.AdminSocketPath) before attempting the launch once.
	go launchTrayHelperWhenReady()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
loop:
	for {
		c := <-r
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.SessionChange:
			// EventType is one of the WTS_* constants (windows package);
			// only react to "a session just became the active console
			// session" -- WTS_SESSION_LOGOFF/WTS_CONSOLE_DISCONNECT need no
			// handling here since the process that was running in that
			// session dies on its own, which rustshineBackend's own
			// watchProcessExit -> onExit -> startSunshine chain already
			// notices and reacts to; WTS_SESSION_UNLOCK is deliberately
			// excluded too -- a locked (not logged-off) session is still a
			// real interactive session DXGI capture already works fine
			// against, so restarting on unlock would just interrupt an
			// otherwise-fine stream for no reason.
			if c.EventType == windows.WTS_SESSION_LOGON || c.EventType == windows.WTS_CONSOLE_CONNECT {
				go app.NotifySessionChange()
				go launchTrayHelperWhenReady()
			}
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			break loop
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	return
}

// launchTrayHelperWhenReady polls for app.AdminSocketPath to become
// available (the engine started by doStart above hasn't necessarily
// finished enough of its own startup yet to have one) and, once it does,
// launches the tray helper into whatever session is currently active --
// see app.LaunchTrayHelperInActiveSession's doc comment for why the
// service has to do this itself rather than relying on a normal autostart
// entry. Gives up silently after ~30s: LaunchTrayHelperInActiveSession is
// also called on every subsequent session-change event, so a session that
// only appears later still gets a helper then.
func launchTrayHelperWhenReady() {
	for i := 0; i < 60; i++ {
		if socketPath, ok := app.AdminSocketReady(); ok {
			app.LaunchTrayHelperInActiveSession(socketPath)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func manageService(action string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %v", err)
	}
	defer m.Disconnect()

	if action == "install" {
		exePath, err := os.Executable()
		if err != nil {
			return err
		}

		s, err := m.OpenService(serviceName)
		if err == nil {
			// Already registered. Keep it AUTO_START so the next boot
			// launches the engine, but do not Start() it from this
			// elevated helper: Enable() is only reachable from a live
			// GUI/tray that already owns a tray icon. Starting the
			// service now would LaunchTrayHelperInActiveSession into
			// this same session and spawn a second (then third, …)
			// tray process beside the one already running.
			cfg, cfgErr := s.Config()
			if cfgErr == nil && (cfg.StartType != mgr.StartAutomatic || cfg.DelayedAutoStart) {
				cfg.StartType = mgr.StartAutomatic
				cfg.DelayedAutoStart = false
				if err := s.UpdateConfig(cfg); err != nil {
					s.Close()
					return fmt.Errorf("update service: %v", err)
				}
			}
			s.Close()
			return nil
		}

		s, err = m.CreateService(serviceName, exePath, mgr.Config{
			StartType:        mgr.StartAutomatic,
			DisplayName:      "USBridge Agent",
			Description:      "USBridge Remote Access Service",
			DelayedAutoStart: false,
		}, "--headless")
		if err != nil {
			return fmt.Errorf("create service: %v", err)
		}
		defer s.Close()

		// Do not s.Start() here — see the OpenService branch above.
		// Autostart at Boot is supposed to take effect on the next
		// Windows reboot, when SCM launches this AUTO_START service
		// into session 0 and the tray helper can appear in the then-
		// interactive logon session.
		return nil
	} else if action == "uninstall" {
		s, err := m.OpenService(serviceName)
		if err != nil {
			return nil // already doesn't exist
		}
		defer s.Close()
		_, _ = s.Control(svc.Stop)
		if err := s.Delete(); err != nil {
			return fmt.Errorf("delete service: %v", err)
		}
		return nil
	}
	return fmt.Errorf("unknown service action: %s", action)
}
