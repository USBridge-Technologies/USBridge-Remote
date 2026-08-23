//go:build linux

package permissions

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"usbridge_agent/internal/capture"
)

const uinputRulePath = "/etc/udev/rules.d/99-usbridge-input.rules"

// MODE="0666" is what actually makes this survive a reboot. Relying only on
// TAG+="uaccess" (systemd-logind's *dynamic* per-session ACL, granted only
// to whichever user's session is currently active on the seat) is what used
// to make uinput access disappear after every restart: usbridge-agent.service
// is a system unit that starts at boot unconditionally, independent of any
// login (see autostart_linux.go's comment on why -- KMS capture needs to
// come up before a display manager does), so it doesn't reliably have, or
// keep, a session logind considers "active" the way an interactive desktop
// process does -- and while SDDM's greeter (a different user) owns the
// active seat, the uaccess ACL belongs to the greeter, not this agent's
// user. A static on-disk MODE grant applies unconditionally every time the
// kernel (re)creates the device node, with no session/login dependency at
// all -- the same fix already applied to the render group in
// autostart_linux.go for the identical class of problem. uaccess is kept
// alongside as a harmless extra (and slightly tighter, while it's live).
const uinputRuleContent = "KERNEL==\"uinput\", SUBSYSTEM==\"misc\", MODE=\"0666\", TAG+=\"uaccess\"\n"

// Even with the MODE=0666 rule above, /dev/uinput's permissions after a
// reboot depend on *how* the node comes into existence. Without this file,
// nothing forces the real "uinput" kernel module to load at boot: the node
// that shows up early is typically just udev's "static_node" stand-in
// (created by systemd-tmpfiles-setup-dev.service straight from the
// distro's own default rule, e.g. /usr/lib/udev/rules.d/50-udev-default.rules
// -- 0660, root:root there, not ours) — a full re-application of our own
// rule only happens once the module actually registers for real and emits
// a genuine uevent, which otherwise only happens lazily (whenever the
// kernel first autoloads it, racing against whatever tries to open the
// device first). /etc/modules-load.d makes systemd-modules-load.service
// modprobe uinput unconditionally, early in sysinit, so that real
// registration -- and with it, full udev rule processing -- always
// happens well before anything (including this agent's own systemd unit)
// tries to touch the device. See autostart_linux.go's dev-uinput.device
// ordering for the other half of this: the agent unit not even starting
// until udev has finished applying the rule to that real device.
const uinputModulesLoadPath = "/etc/modules-load.d/usbridge-uinput.conf"
const uinputModulesLoadContent = "uinput\n"

type Service struct {
	lastAccessErr string
}

func New() *Service { return &Service{} }

// LastAccessibilityError returns a human-readable reason the last
// RequestAccessibility call failed, or "" if it succeeded (or hasn't run
// yet). Debian's default install neither adds the user to the sudo group
// nor pulls in pkexec (it was split into its own package from policykit-1
// around trixie), unlike Ubuntu where both are present out of the box --
// so the pkexec-based flow below silently does nothing there unless we
// surface why.
func (s *Service) LastAccessibilityError() string { return s.lastAccessErr }

func (s *Service) AccessibilityGranted() bool {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return uinputRuleUpToDate()
}

// uinputRuleUpToDate reports whether the persistent udev rule on disk
// already grants the current (MODE=0666) content, not just the older,
// uaccess-only version this project shipped before. The /dev/uinput open()
// probe above can succeed right now purely because the *caller's own*
// interactive session happens to hold a live uaccess ACL, even on a machine
// whose on-disk rule -- the one usbridge-agent.service actually depends on
// after the next reboot -- is still the old, fragile one. Checking the file
// too makes RequestAccessibility keep firing (and upgrading the rule on
// disk) for anyone who granted access before this fix, instead of only for
// machines where access is visibly broken right this second.
func uinputRuleUpToDate() bool {
	data, err := os.ReadFile(uinputRulePath)
	if err != nil {
		return false
	}
	if string(data) != uinputRuleContent {
		return false
	}
	modules, err := os.ReadFile(uinputModulesLoadPath)
	if err != nil {
		return false
	}
	return string(modules) == uinputModulesLoadContent
}

func (s *Service) ScreenRecordingGranted() bool {
	if capture.GetLinuxEnv() == "Wayland" {
		return capture.GetPortalSession() != ""
	}
	return true
}

func (s *Service) RequestAccessibility() bool {
	s.lastAccessErr = ""
	log.Printf("[permissions] RequestAccessibility called, granted=%v", s.AccessibilityGranted())
	if s.AccessibilityGranted() {
		return true
	}

	if _, err := exec.LookPath("pkexec"); err != nil {
		s.lastAccessErr = "pkexec is not installed. Install it and try again:\n" +
			"  su -c 'apt install pkexec'\n" +
			"(on Debian, pkexec ships in its own package and the default user\n" +
			"isn't in the sudo group, so plain \"sudo apt install\" may also fail)"
		log.Printf("[permissions] %s", s.lastAccessErr)
		return false
	}

	tmp, err := os.CreateTemp("", "usbridge-udev-*.rules")
	if err != nil {
		s.lastAccessErr = fmt.Sprintf("could not create temp udev rule: %v", err)
		log.Printf("[permissions] create temp udev rule: %v", err)
		return false
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(uinputRuleContent); err != nil {
		tmp.Close()
		s.lastAccessErr = fmt.Sprintf("could not write temp udev rule: %v", err)
		log.Printf("[permissions] write temp udev rule: %v", err)
		return false
	}
	tmp.Close()

	modulesTmp, err := os.CreateTemp("", "usbridge-modules-load-*.conf")
	if err != nil {
		s.lastAccessErr = fmt.Sprintf("could not create temp modules-load file: %v", err)
		log.Printf("[permissions] create temp modules-load file: %v", err)
		return false
	}
	defer os.Remove(modulesTmp.Name())

	if _, err := modulesTmp.WriteString(uinputModulesLoadContent); err != nil {
		modulesTmp.Close()
		s.lastAccessErr = fmt.Sprintf("could not write temp modules-load file: %v", err)
		log.Printf("[permissions] write temp modules-load file: %v", err)
		return false
	}
	modulesTmp.Close()
	log.Printf("[permissions] temp rule at %s, temp modules-load at %s, running pkexec...", tmp.Name(), modulesTmp.Name())

	// Install persistent udev rule AND immediately apply chmod for current session.
	// install -m 0644 (not cp): cp preserves the source file's mode, and the
	// tmp file above was created by os.CreateTemp as 0600 owned by this
	// (non-root) agent user, so a plain cp left the installed rule file
	// unreadable by anyone but root. udevd itself runs as root so the rule
	// still applied fine either way, but uinputRuleUpToDate() below reads
	// this same path as the agent's own unprivileged user -- with a 0600
	// file it always got EACCES and returned false, so AccessibilityGranted
	// reported "broken" forever even on machines where /dev/uinput was
	// already 0666 and working. Rule files under /etc/udev/rules.d are
	// world-readable everywhere else on the system; match that.
	//
	// Also install /etc/modules-load.d/usbridge-uinput.conf and modprobe
	// uinput right now: this is the piece that actually makes the whole
	// thing survive a reboot (see uinputModulesLoadPath's comment) -- without
	// forcing a real module load, the udev rule above only gets applied to
	// whatever bare-bones stand-in device node happens to exist at the
	// moment the module finally, lazily, loads on its own.
	script := fmt.Sprintf(
		"install -m 0644 %s %s && install -m 0644 %s %s && modprobe uinput && chmod 0666 /dev/uinput && udevadm control --reload-rules && udevadm trigger --subsystem-match=misc",
		tmp.Name(), uinputRulePath, modulesTmp.Name(), uinputModulesLoadPath,
	)
	cmd := exec.Command("pkexec", "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	log.Printf("[permissions] pkexec exit=%v output=%q", err, string(out))
	if err != nil {
		switch {
		case strings.Contains(string(out), "No authentication agent found"):
			s.lastAccessErr = "no polkit authentication agent is running for this session " +
				"(pkexec needs one to prompt for the password). Log into a full desktop " +
				"session and make sure its polkit agent is running, then retry."
		case strings.Contains(err.Error(), "exit status 126"):
			s.lastAccessErr = "authentication was cancelled or dismissed. Click Request again and approve the prompt."
		default:
			s.lastAccessErr = fmt.Sprintf("pkexec failed: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return false
	}

	time.Sleep(300 * time.Millisecond)
	granted := s.AccessibilityGranted()
	log.Printf("[permissions] after pkexec granted=%v", granted)
	if !granted {
		s.lastAccessErr = "udev rule was installed but /dev/uinput is still inaccessible; " +
			"try unplugging/replugging, or log out and back in."
	}
	return granted
}

func (s *Service) RequestScreenRecording() bool {
	if capture.GetLinuxEnv() == "Wayland" {
		err := capture.InitPortalSession()
		if err != nil {
			logrus.Errorf("Failed to initiate Wayland portal: %v", err)
		}
		return true
	}
	return true
}

func (s *Service) RequestMissing()                    {}
func (s *Service) OpenPrivacySettings() error         { return nil }
func (s *Service) OpenScreenRecordingSettings() error { return nil }

// clipboardToolFound reports whether a CLI clipboard helper
// internal/clipboard's Linux backend knows how to drive is installed --
// mirrors detect()'s preference order there (wl-clipboard needs both
// halves present; xclip and xsel are single binaries).
func clipboardToolFound() bool {
	if _, err := exec.LookPath("wl-copy"); err == nil {
		if _, err := exec.LookPath("wl-paste"); err == nil {
			return true
		}
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		return true
	}
	if _, err := exec.LookPath("xsel"); err == nil {
		return true
	}
	return false
}

// ClipboardToolAvailable reports whether clipboard sync has a working CLI
// helper to shell out to. Wayland sessions typically ship wl-clipboard
// preinstalled, but plenty of X11/XWayland desktops -- this project's own
// Debian test machine included -- ship neither xclip nor xsel by default,
// which silently and permanently disables clipboard sync (both directions)
// until one is installed: confirmed live via
// "clipboard: no clipboard tool available" on every apply.
func (s *Service) ClipboardToolAvailable() bool {
	return clipboardToolFound()
}

// pkgManager describes one Linux package manager capable of installing
// xclip -- the package is named "xclip" identically across every one of
// these, unlike e.g. wl-clipboard (wl-clipboard vs wl-clipboard-x11 vs
// clipboard depending on distro), which is why RequestClipboardTool always
// installs xclip specifically rather than trying to pick per-distro.
type pkgManager struct {
	name string
	// probe is the binary whose presence on PATH identifies this manager.
	probe string
	// script is run as root (via pkexec /bin/sh -c) to install xclip
	// non-interactively. Checked/ordered so a system with more than one
	// manager installed (e.g. Debian derivatives sometimes carry a leftover
	// snap/flatpak-only pacman shim) still picks its *actual* one first.
	script string
}

// pkgManagers covers every package manager on the popular Linux desktop
// distro families: apt (Debian/Ubuntu/Mint/Pop!_OS), dnf (Fedora/RHEL 8+/
// Rocky/Alma), yum (older RHEL/CentOS 7), pacman (Arch/Manjaro/EndeavourOS),
// zypper (openSUSE), apk (Alpine).
var pkgManagers = []pkgManager{
	{
		name:  "apt",
		probe: "apt-get",
		// apt-get update first: a fresh install/container image commonly
		// has an empty or stale package index, which makes a bare
		// "apt-get install" fail with "Unable to locate package" even
		// though xclip is really available -- confirmed the failure mode
		// this project's own fresh Debian netinst hits. -qq keeps the
		// pkexec output free of the (harmless but noisy) progress bars.
		script: "DEBIAN_FRONTEND=noninteractive apt-get update -qq && " +
			"DEBIAN_FRONTEND=noninteractive apt-get install -y xclip",
	},
	{name: "dnf", probe: "dnf", script: "dnf install -y xclip"},
	{name: "yum", probe: "yum", script: "yum install -y xclip"},
	// -Sy (not plain -S): like apt above, pacman needs its local sync
	// database refreshed first or a perfectly valid package name can come
	// back "target not found" on a system that hasn't run pacman -Sy
	// recently (containers, minimal installs).
	{name: "pacman", probe: "pacman", script: "pacman -Sy --noconfirm xclip"},
	{name: "zypper", probe: "zypper", script: "zypper --non-interactive install xclip"},
	{name: "apk", probe: "apk", script: "apk add --no-cache xclip"},
}

// detectPkgManager returns the first package manager from pkgManagers whose
// probe binary is on PATH, or nil if none of them are (an unsupported/
// exotic distro, or a minimal container image with no package manager at
// all).
func detectPkgManager() *pkgManager {
	return detectPkgManagerWith(exec.LookPath)
}

// detectPkgManagerWith is detectPkgManager with its PATH lookup injected, so
// the distro-selection logic (order, first-match) is testable without
// depending on which package managers happen to be installed on whatever
// machine runs `go test`.
func detectPkgManagerWith(lookPath func(string) (string, error)) *pkgManager {
	for i := range pkgManagers {
		if _, err := lookPath(pkgManagers[i].probe); err == nil {
			return &pkgManagers[i]
		}
	}
	return nil
}

// RequestClipboardTool installs xclip via pkexec, using whichever package
// manager this distro actually has (see pkgManagers) -- the same pkexec
// pattern as RequestAccessibility. xclip specifically (not wl-clipboard):
// it talks to the X11 clipboard selection directly, which XWayland bridges
// to the native Wayland clipboard on every compositor this app realistically
// runs under, so it's the one no-extra-config choice that fixes clipboard
// sync regardless of which desktop this button gets clicked on.
func (s *Service) RequestClipboardTool() bool {
	s.lastAccessErr = ""
	if clipboardToolFound() {
		return true
	}
	if _, err := exec.LookPath("pkexec"); err != nil {
		s.lastAccessErr = "pkexec is not installed. Install xclip manually instead, e.g.:\n" +
			"  su -c 'apt install xclip -y'   (Debian/Ubuntu)\n" +
			"  su -c 'dnf install -y xclip'   (Fedora/RHEL)\n" +
			"  su -c 'pacman -S xclip'        (Arch)"
		log.Printf("[permissions] %s", s.lastAccessErr)
		return false
	}

	pm := detectPkgManager()
	if pm == nil {
		s.lastAccessErr = "no supported package manager found (looked for apt, dnf, yum, pacman, " +
			"zypper, apk). Install xclip manually for this distro."
		log.Printf("[permissions] %s", s.lastAccessErr)
		return false
	}

	cmd := exec.Command("pkexec", "/bin/sh", "-c", pm.script)
	out, err := cmd.CombinedOutput()
	log.Printf("[permissions] install xclip via %s pkexec exit=%v output=%q", pm.name, err, string(out))
	if err != nil {
		switch {
		case strings.Contains(string(out), "No authentication agent found"):
			s.lastAccessErr = "no polkit authentication agent is running for this session " +
				"(pkexec needs one to prompt for the password). Log into a full desktop " +
				"session and make sure its polkit agent is running, then retry."
		case strings.Contains(err.Error(), "exit status 126"):
			s.lastAccessErr = "authentication was cancelled or dismissed. Click Install again and approve the prompt."
		default:
			s.lastAccessErr = fmt.Sprintf("xclip install via %s failed: %v (%s)", pm.name, err, strings.TrimSpace(string(out)))
		}
		return false
	}

	granted := clipboardToolFound()
	if !granted {
		s.lastAccessErr = fmt.Sprintf("%s reported success but xclip still isn't on PATH; try again or install manually.", pm.name)
	}
	return granted
}

// findCapTool resolves getcap/setcap to an absolute path. Both live in
// /usr/sbin (libcap2-bin), which many non-login shells -- and pkexec's own
// sanitized environment -- don't include in PATH, so a bare exec.LookPath
// (or handing the bare name to pkexec) can fail with "not found" even
// though the binary is installed.
func findCapTool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range []string{"/usr/sbin", "/sbin"} {
		p := dir + "/" + name
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return name
}

// KMSCaptureGranted reports whether the bundled sunshine_capexec launcher
// has the CAP_SYS_ADMIN capability needed for Sunshine's direct KMS screen
// capture (root-level, no compositor/portal involved).
//
// capexecPath is the path to sunshine_capexec, NOT to sunshine itself — see
// RequestKMSCapture for why the capability lives on a separate launcher.
func (s *Service) KMSCaptureGranted(capexecPath string) bool {
	if strings.TrimSpace(capexecPath) == "" {
		return false
	}
	out, err := exec.Command(findCapTool("getcap"), capexecPath).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "cap_sys_admin")
}

// RequestKMSCapture grants CAP_SYS_ADMIN to the bundled sunshine_capexec
// launcher via pkexec setcap, so Sunshine can use its KMS capture backend
// without running as root outright.
//
// This deliberately targets sunshine_capexec, a tiny statically-linked
// (zero dynamic deps) launcher — never the sunshine binary itself. Setting a
// file capability puts the dynamic linker into "secure execution" mode for
// that binary (same as setuid): glibc ignores RPATH/RUNPATH and
// LD_LIBRARY_PATH entirely, the same protection that stops a setuid binary
// from being tricked into loading an attacker-controlled library. Since
// Sunshine resolves its bundled dependencies (e.g. libminiupnpc.so.17) via
// RPATH=$ORIGIN/../lib, setting the capability directly on it would break
// that resolution the moment it's granted. sunshine_capexec instead raises
// CAP_SYS_ADMIN into its own ambient capability set and execs the real,
// perfectly ordinary (no file capability of its own) sunshine binary —
// ambient capabilities are preserved across exec of a non-privileged binary
// without ever placing it into secure-execution mode, so its RPATH keeps
// resolving normally. See cmd/sunshine_capexec.
func (s *Service) RequestKMSCapture(capexecPath string) bool {
	if strings.TrimSpace(capexecPath) == "" {
		return false
	}
	if s.KMSCaptureGranted(capexecPath) {
		return true
	}
	cmd := exec.Command("pkexec", findCapTool("setcap"), "cap_sys_admin=eip", capexecPath)
	out, err := cmd.CombinedOutput()
	log.Printf("[permissions] setcap pkexec exit=%v output=%q", err, string(out))
	if err != nil {
		return false
	}
	return s.KMSCaptureGranted(capexecPath)
}

// GPU clock locking is Windows-only (NVML clock lock via an elevated
// gamestream-server --gpu-clock-lock-daemon helper -- see
// service_windows.go's own docs); not applicable on Linux.
func (s *Service) GPUClockLockSupported() bool                            { return false }
func (s *Service) GPUClockLockElevated() bool                             { return false }
func (s *Service) RequestGPUClockLock(binPath string, watchPID int) error { return nil }
