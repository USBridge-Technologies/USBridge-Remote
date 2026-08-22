//go:build windows

package main

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "USBridgeAgent"

func runMain(headless bool) {
	isSvc, err := svc.IsWindowsService()
	if err != nil {
		log.Printf("failed to determine if we are running in an interactive session: %v", err)
		isSvc = false
	}
	if isSvc {
		err = svc.Run(serviceName, &agentService{})
		if err != nil {
			log.Fatalf("service execution failed: %v", err)
		}
		return
	}
	doStart(headless)
}

type agentService struct{}

func (m *agentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	// Start headless mode in background
	go doStart(true)

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
loop:
	for {
		c := <-r
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			break loop
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	return
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
			// already exists, maybe update it?
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

		// also start it right away
		_ = s.Start()
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
