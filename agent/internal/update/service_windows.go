//go:build windows

package update

import (
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// serviceName is the service the setup .exe registers the agent as (see
// scripts/windows-installer/USBridgeAgent.iss).
const serviceName = "USBridgeAgent"

// serviceRecovery matches what the installer sets with "sc failure": come
// back by itself after a crash, 5 s, 5 s, then every 30 s, counter reset
// after a day.
var serviceRecovery = []mgr.RecoveryAction{
	{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
	{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
	{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
}

const serviceRecoveryResetSeconds = 24 * 60 * 60

func withOwnService(f func(*mgr.Service) error) error {
	if isSvc, err := svc.IsWindowsService(); err != nil || !isSvc {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()
	return f(s)
}

// suspendServiceRecovery turns the service's restart-on-failure off right
// before the agent exits for a self-update. The exit looks like a crash to
// the service manager, which then restarted the old exe within 5 s: that
// copy held the files the helper was replacing and, still being the old
// version, downloaded and started the same update again. Where the swap kept
// failing, this repeated every half minute, each round leaving a 60 MB
// archive in the temp folder. The helper starts the service again itself,
// and the agent turns recovery back on when it starts (EnsureServiceRecovery).
// No-op outside the service.
func suspendServiceRecovery() {
	if err := withOwnService(func(s *mgr.Service) error { return s.ResetRecoveryActions() }); err != nil {
		logrus.WithField("component", "update").WithError(err).Warn("could not pause service recovery for the update")
	}
}

// EnsureServiceRecovery restores the restart-on-failure actions that
// suspendServiceRecovery turned off for an update. The service calls it on
// every start. No-op outside the service.
func EnsureServiceRecovery() {
	if err := withOwnService(func(s *mgr.Service) error {
		return s.SetRecoveryActions(serviceRecovery, serviceRecoveryResetSeconds)
	}); err != nil {
		logrus.WithField("component", "update").WithError(err).Warn("could not set service recovery actions")
	}
}
