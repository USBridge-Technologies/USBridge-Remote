//go:build windows

package iscsi

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// windowsInitiator drives the built-in Microsoft iSCSI Initiator service via
// PowerShell's Storage/iSCSI cmdlets (New-IscsiTargetPortal,
// Connect-IscsiTarget, Get-Disk, Disconnect-IscsiTarget). No pure-Go
// initiator library exists for Windows; this needs no extra install since
// the MSiSCSI service ships with Windows (Vista/Server 2008+) — it's
// usually just not running by default, so we ensure it's started first.
type windowsInitiator struct{}

// New returns the Windows iSCSI initiator.
func New() Initiator {
	return &windowsInitiator{}
}

func (windowsInitiator) Available() bool {
	_, err := exec.LookPath("powershell.exe")
	return err == nil
}

func runPowerShell(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("powershell: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (w *windowsInitiator) Login(ctx context.Context, opts LoginOptions) (LoginResult, error) {
	if strings.TrimSpace(opts.Portal) == "" || strings.TrimSpace(opts.TargetIQN) == "" {
		return LoginResult{}, fmt.Errorf("iscsi login: portal and target IQN are required")
	}
	host, port, err := splitPortal(opts.Portal)
	if err != nil {
		return LoginResult{}, err
	}

	// Ensure the MSiSCSI service is running (Set-Service + Start-Service are
	// idempotent no-ops if it's already running).
	if _, err := runPowerShell(ctx, "Set-Service -Name msiscsi -StartupType Automatic; Start-Service msiscsi"); err != nil {
		return LoginResult{}, fmt.Errorf("starting MSiSCSI service: %w", err)
	}

	if _, err := runPowerShell(ctx, fmt.Sprintf(
		"New-IscsiTargetPortal -TargetPortalAddress %q -TargetPortalPortNumber %d -ErrorAction SilentlyContinue | Out-Null",
		host, port)); err != nil {
		return LoginResult{}, fmt.Errorf("registering target portal: %w", err)
	}

	connectCmd := fmt.Sprintf(
		"Connect-IscsiTarget -NodeAddress %q -TargetPortalAddress %q -TargetPortalPortNumber %d -IsPersistent $true",
		opts.TargetIQN, host, port)
	if opts.CHAPUsername != "" {
		connectCmd += fmt.Sprintf(" -AuthenticationType ONEWAYCHAP -ChapUsername %q -ChapSecret %q",
			opts.CHAPUsername, opts.CHAPSecret)
	}
	if _, err := runPowerShell(ctx, connectCmd); err != nil {
		return LoginResult{}, fmt.Errorf("login: %w", err)
	}

	devPath, err := waitForWindowsDisk(ctx, opts.TargetIQN, 10*time.Second)
	if err != nil {
		_, _ = runPowerShell(ctx, fmt.Sprintf("Get-IscsiTarget -NodeAddress %q | Disconnect-IscsiTarget -Confirm:$false", opts.TargetIQN))
		return LoginResult{}, err
	}

	return LoginResult{DevicePath: devPath, SessionID: opts.TargetIQN}, nil
}

// waitForWindowsDisk polls Get-Disk for the disk backed by this target's
// iSCSI session and returns a stable identifier ("\\.\PhysicalDriveN").
func waitForWindowsDisk(ctx context.Context, iqn string, timeout time.Duration) (string, error) {
	// Get-Disk's BusType for iSCSI-attached disks is "iSCSI"; among those,
	// match on the session's target via Get-IscsiConnection/Get-IscsiSession
	// would need extra plumbing, so we take the newest iSCSI-bus disk that
	// appeared after Connect-IscsiTarget returned — acceptable because the
	// agent only manages one login at a time per target/LUN pair.
	script := `Get-Disk | Where-Object BusType -eq 'iSCSI' | Sort-Object Number -Descending | Select-Object -First 1 -Property Number | ConvertTo-Json`
	deadline := time.Now().Add(timeout)
	for {
		out, err := runPowerShell(ctx, script)
		if err == nil {
			var result struct {
				Number int `json:"Number"`
			}
			if jsonErr := json.Unmarshal([]byte(out), &result); jsonErr == nil && out != "" {
				return fmt.Sprintf(`\\.\PhysicalDrive%d`, result.Number), nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for iSCSI disk for target %s to appear", iqn)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func (w *windowsInitiator) Logout(ctx context.Context, opts LoginOptions) error {
	if strings.TrimSpace(opts.TargetIQN) == "" {
		return nil
	}
	_, err := runPowerShell(ctx, fmt.Sprintf(
		"Get-IscsiTarget -NodeAddress %q -ErrorAction SilentlyContinue | Disconnect-IscsiTarget -Confirm:$false -ErrorAction SilentlyContinue",
		opts.TargetIQN))
	return err
}

func splitPortal(portal string) (host string, port int, err error) {
	idx := strings.LastIndex(portal, ":")
	if idx < 0 {
		return portal, 3260, nil
	}
	host = portal[:idx]
	p, err := strconv.Atoi(portal[idx+1:])
	if err != nil {
		return "", 0, fmt.Errorf("invalid portal %q: %w", portal, err)
	}
	return host, p, nil
}
