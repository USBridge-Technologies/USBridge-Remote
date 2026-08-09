//go:build linux

package iscsi

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// linuxInitiator shells out to iscsiadm (open-iscsi). Chosen over the
// pure-Go github.com/u-root/iscsinl netlink initiator after a Phase-0 spike
// showed iscsinl's login handshake is incompatible with the client's gotgt
// target (connection closed with "read BHS failed: unexpected EOF"
// immediately after login); iscsiadm was validated end-to-end against the
// same target and is the proven path.
type linuxInitiator struct{}

// New returns the Linux iSCSI initiator.
func New() Initiator {
	return &linuxInitiator{}
}

func (linuxInitiator) Available() bool {
	_, err := exec.LookPath("iscsiadm")
	return err == nil
}

// diskMountNotGrantedHint is appended to the first error caused by lacking
// root (iscsiadm needs it — writing /run/lock/iscsi/lock and talking to
// the kernel's netlink iSCSI transport are both root-only). Deliberately
// NOT auto-elevated here (no pkexec/sudo password prompt mid-mount) — the
// user grants this once, upfront, via the "Request" button in the agent's
// Permissions panel (permissions.Service.RequestDiskMount), same pattern
// as every other privileged capability this agent needs.
const diskMountNotGrantedHint = " — grant disk-mount permission in the agent's Permissions panel first"

// ensureIscsidRunning best-effort starts the open-iscsi userspace daemon
// via the same sudo grant iscsiadm itself uses (see runPrivileged).
// Without iscsid running (e.g. a minimal/container host, or a systemd unit
// that's installed but not enabled), the kernel iSCSI session can be
// negotiated but isn't kept alive/scanned properly — it gets logged out
// again within seconds of login, and the LUN's block device never appears.
// Errors are intentionally ignored: many real hosts already have iscsid
// running via systemd socket activation (started transparently by the
// first `iscsiadm -m discovery` call) and don't need this at all; and this
// must never itself trigger an interactive password prompt (see
// runPrivileged) — if the grant isn't there yet, the discovery call right
// after this will fail with a clear, actionable error instead.
func ensureIscsidRunning(parent context.Context) {
	// Bounded to a short timeout of its own, independent of the caller's
	// overall Login() deadline: `sudo systemctl start iscsid` occasionally
	// hangs past its context's cancellation without actually dying —
	// exec.CommandContext only SIGKILLs the direct child (sudo itself),
	// not whatever it spawned, so a stuck grandchild can keep our own
	// CombinedOutput() call blocked reading its pipe well past the
	// context deadline. This is best-effort anyway (see doc below); it's
	// not worth blocking the whole login flow over.
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	if _, err := exec.LookPath("systemctl"); err == nil {
		done := make(chan struct{})
		go func() {
			defer close(done)
			runPrivileged(ctx, "systemctl", "start", "iscsid")
		}()
		select {
		case <-done:
		case <-ctx.Done():
		}
		return
	}
	// No systemd — start iscsid directly if it isn't already running.
	// (Deliberately not through sudo: this path is for non-systemd hosts
	// like a bare container, where the process typically already has the
	// privilege it needs, e.g. our own Docker-based integration test.)
	if out, err := exec.CommandContext(ctx, "pgrep", "-x", "iscsid").CombinedOutput(); err != nil || len(out) == 0 {
		if bin, err := exec.LookPath("iscsid"); err == nil {
			cmd := exec.Command(bin)
			_ = cmd.Start() // detached, best-effort; we don't wait on it
		}
	}
}

// runPrivileged runs name with args as root: directly if this process is
// already root (e.g. inside the Docker-based integration test), otherwise
// via `sudo -n` against the sudoers grant permissions.Service.RequestDiskMount
// installs — "-n" (non-interactive) is the crucial bit: if that grant
// isn't there yet, this fails immediately with a clear error instead of
// hanging on (or silently swallowing) a password prompt with no TTY/polkit
// agent attached to it.
func runPrivileged(ctx context.Context, name string, args ...string) (string, error) {
	bin, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found in PATH", name)
	}
	var cmd *exec.Cmd
	if os.Geteuid() == 0 {
		cmd = exec.CommandContext(ctx, bin, args...)
	} else {
		cmd = exec.CommandContext(ctx, "sudo", append([]string{"-n", bin}, args...)...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		if os.Geteuid() != 0 && (strings.Contains(string(out), "a password is required") ||
			strings.Contains(string(out), "sudo: a terminal is required") ||
			strings.Contains(string(out), "lock file")) {
			return string(out), fmt.Errorf("%w%s", msg, diskMountNotGrantedHint)
		}
		return string(out), msg
	}
	return string(out), nil
}

func runIscsiadm(ctx context.Context, args ...string) (string, error) {
	return runPrivileged(ctx, "iscsiadm", args...)
}

func (l *linuxInitiator) Login(ctx context.Context, opts LoginOptions) (LoginResult, error) {
	if strings.TrimSpace(opts.Portal) == "" || strings.TrimSpace(opts.TargetIQN) == "" {
		return LoginResult{}, fmt.Errorf("iscsi login: portal and target IQN are required")
	}

	ensureIscsidRunning(ctx)

	// Deliberately skip `-m discovery -t sendtargets`: SendTargets discovery
	// registers its node record keyed by whatever portal address the
	// TARGET advertises in its own response, not the address iscsiadm
	// actually dialed to reach it — normally the same thing, but not when
	// the client is reached through the agent's tsnet dial proxy (see
	// TsnetDialProxy's doc comment): the target's advertised portal is
	// still its own real bind address (e.g. "0.0.0.0:3260"), while we
	// dialed a local proxy port instead, so the discovered record and the
	// portal we can actually use never match ("iscsiadm: No records
	// found" on --login). We already know the target IQN and portal from
	// this agent's own protocol (the client tells us directly) — no
	// discovery step is needed at all. `--op=new` creates the node record
	// directly, keyed by exactly the portal we're about to log into;
	// idempotent, safe to repeat on every mount.
	if _, err := runIscsiadm(ctx, "-m", "node", "-T", opts.TargetIQN, "-p", opts.Portal, "--op=new"); err != nil {
		return LoginResult{}, fmt.Errorf("creating node record: %w", err)
	}

	if opts.CHAPUsername != "" {
		for _, kv := range [][2]string{
			{"node.session.auth.authmethod", "CHAP"},
			{"node.session.auth.username", opts.CHAPUsername},
			{"node.session.auth.password", opts.CHAPSecret},
		} {
			if _, err := runIscsiadm(ctx, "-m", "node", "-T", opts.TargetIQN, "-p", opts.Portal,
				"--op=update", "-n", kv[0], "-v", kv[1]); err != nil {
				return LoginResult{}, fmt.Errorf("configuring CHAP: %w", err)
			}
		}
	}

	if _, err := runIscsiadm(ctx, "-m", "node", "-T", opts.TargetIQN, "-p", opts.Portal, "--login"); err != nil {
		// Login failures (bad target, CHAP mismatch, target not ready) are
		// not transient in the general case — surface as-is, no retry here.
		// The caller (client-side startDevicesWithRetry) decides on retry
		// policy for the transport-level errors it can actually recover
		// from (connection reset/refused).
		return LoginResult{}, fmt.Errorf("login: %w", err)
	}

	devPath, err := waitForDevice(opts.Portal, opts.TargetIQN, opts.LUN, 10*time.Second)
	if err != nil {
		// Best-effort cleanup: don't leave a logged-in session with no
		// discoverable device.
		_, _ = runIscsiadm(ctx, "-m", "node", "-T", opts.TargetIQN, "-p", opts.Portal, "--logout")
		return LoginResult{}, err
	}

	return LoginResult{DevicePath: devPath, SessionID: opts.TargetIQN}, nil
}

// waitForDevice resolves the local block device for a just-logged-in LUN.
// Tries the udev by-path symlink first (fast, and readable at a glance in
// logs), then falls back to walking /sys directly — the by-path symlink is
// created by udev rules, which aren't running on minimal/container hosts
// (confirmed: this fallback was needed to make the agent's own Docker-based
// integration tests pass at all), so relying on it alone would leave a real
// gap on such hosts too, not just in tests.
func waitForDevice(portal, iqn string, lun int, timeout time.Duration) (string, error) {
	link := fmt.Sprintf("/dev/disk/by-path/ip-%s-iscsi-%s-lun-%d", portal, iqn, lun)
	deadline := time.Now().Add(timeout)
	for {
		if target, err := filepath.EvalSymlinks(link); err == nil {
			return target, nil
		}
		if dev, ok := findDeviceViaSysfs(iqn, lun); ok {
			return dev, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for the block device for target %s (lun %d) to appear after login", iqn, lun)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

var sysfsHostRe = regexp.MustCompile(`/host(\d+)/`)

// findDeviceViaSysfs locates the block device for (iqn, lun) without relying
// on udev: match iqn against /sys/class/iscsi_session/session*/targetname,
// resolve the owning SCSI host number from the session's real sysfs path,
// then look up /sys/bus/scsi/devices/<host>:0:0:<lun>/block/*.
func findDeviceViaSysfs(iqn string, lun int) (string, bool) {
	sessions, _ := filepath.Glob("/sys/class/iscsi_session/session*")
	for _, session := range sessions {
		data, err := os.ReadFile(filepath.Join(session, "targetname"))
		if err != nil || strings.TrimSpace(string(data)) != iqn {
			continue
		}
		real, err := filepath.EvalSymlinks(session)
		if err != nil {
			continue
		}
		m := sysfsHostRe.FindStringSubmatch(real)
		if m == nil {
			continue
		}
		host := m[1]
		blockDirs, _ := filepath.Glob(fmt.Sprintf("/sys/bus/scsi/devices/%s:0:0:%d/block/*", host, lun))
		if len(blockDirs) == 0 {
			continue
		}
		return "/dev/" + filepath.Base(blockDirs[0]), true
	}
	return "", false
}

func (l *linuxInitiator) Logout(ctx context.Context, opts LoginOptions) error {
	if strings.TrimSpace(opts.TargetIQN) == "" {
		return nil
	}
	// --logout / -o delete are idempotent: iscsiadm returns a non-zero exit
	// (and "No matching sessions"/"No records found") if there's nothing to
	// do, which we treat as success rather than propagate.
	_, logoutErr := runIscsiadm(ctx, "-m", "node", "-T", opts.TargetIQN, "-p", opts.Portal, "--logout")
	_, _ = runIscsiadm(ctx, "-m", "node", "-T", opts.TargetIQN, "-p", opts.Portal, "-o", "delete")
	if logoutErr != nil && !strings.Contains(logoutErr.Error(), "No matching sessions") &&
		!strings.Contains(logoutErr.Error(), "No records found") {
		return logoutErr
	}
	return nil
}
