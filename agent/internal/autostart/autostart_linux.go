//go:build linux

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"usbridge_agent/internal/permissions"
)

// unitName/unitPath: a system-wide (not --user) systemd unit. This is
// deliberate: KMS screen capture already requires one-time root elevation
// via pkexec (see internal/permissions.RequestKMSCapture) to grant
// CAP_SYS_ADMIN to sunshine_capexec, so the user has already agreed to a
// polkit prompt for this app — reusing that same pkexec path to install a
// system unit costs nothing extra, and unlike a `systemd --user` unit (which
// only starts once systemd's user instance for that UID is running —
// normally at login, or earlier only if `loginctl enable-linger` was set) a
// system unit starts at boot unconditionally, before any graphical session
// exists. That matters for KMS capture specifically: it reads frames
// straight from DRM/KMS, no compositor or portal required, so it's the one
// capture mode that actually can come up before a display manager does.
const (
	unitName = "usbridge-agent.service"
	unitPath = "/etc/systemd/system/" + unitName
)

// systemdQuote quotes a single ExecStart= argument per systemd's unit-file
// quoting rules (C-style double-quoted string) so a path containing spaces
// still parses as one argument.
func systemdQuote(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	return `"` + replacer.Replace(s) + `"`
}

// detectWaylandDisplay returns the compositor socket name (e.g. "wayland-0")
// to bake into the unit's WAYLAND_DISPLAY=. Sunshine's KMS capture backend
// uses this to open a Wayland connection (see wl::init()/correlate_to_wayland
// in kmsgrab.cpp) and correct each monitor's DRM viewport with the
// compositor's real logical position via xdg-output — without it, every
// monitor's offset falls back to its raw CRTC x/y, which on this hardware is
// 0 for every connector independently (each output has its own CRTC with no
// concept of the others' placement), so a second monitor's absolute-mouse
// input silently gets the wrong (zero) offset instead of its real position
// next to the first one. Prefers the value from our own environment (Enable
// runs as the logged-in user's own process, typically while a graphical
// session is active) and falls back to scanning the runtime dir, then to the
// near-universal "wayland-0" default so the unit still has *something*
// rather than nothing on the rare chance neither found a live session.
func detectWaylandDisplay(uid string) string {
	if v := strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")); v != "" {
		return v
	}
	if matches, err := filepath.Glob(fmt.Sprintf("/run/user/%s/wayland-*", uid)); err == nil {
		for _, m := range matches {
			if strings.HasSuffix(m, ".lock") {
				continue
			}
			return filepath.Base(m)
		}
	}
	return "wayland-0"
}

// sddmXsetupPath is SDDM's root-run "before the login dialog appears" hook
// script. Only present on systems actually using SDDM as their display
// manager; xsetupHookScript below checks for it and no-ops otherwise.
const sddmXsetupPath = "/usr/share/sddm/scripts/Xsetup"

// xsetupHookMarker delimits the block xsetupHookScript appends to Xsetup, so
// re-running Enable() (any later call, not just a fresh install) detects an
// already-installed hook and skips re-appending a duplicate.
const xsetupHookMarker = "# --- USBridge greeter-capture hook ---"

// xsetupHookScript returns a shell fragment that appends a block to Xsetup
// exposing the login greeter's own X11 auth cookie and DISPLAY to
// username, via /run/usbridge-agent (root-owned tmpfs, recreated every
// boot). No-ops if Xsetup doesn't exist (a display manager other than
// SDDM) or the block is already present.
//
// Why this exists at all: KMS/DRM screen capture (see the render-group
// comment above) reads connector/CRTC state directly from the kernel, which
// works everywhere *except* on the nvidia proprietary driver once DRM
// master is held by a session that isn't this agent's own — exactly the
// case while the login greeter (a separate session, a separate user) is
// showing and nobody's logged in yet. nvidia refuses to report that state
// to any client that isn't the current master, capability grants
// notwithstanding (confirmed live: CAP_SYS_ADMIN present and correct,
// connector query still empty) — a limitation open-source drivers
// (i915/amdgpu) don't share. Capture falls back to grabbing the greeter's
// own X11 root window instead when this happens (see
// rustshineBackend.greeterX11Env), which needs that session's auth cookie
// to connect — Xsetup already has it in hand naturally (it runs as root,
// inside that exact X session, before the login dialog appears), so
// copying it to a location username can read is the only step this can't
// do from inside the already-running (and, at exactly this moment,
// KMS-blind) agent process itself.
//
// Ownership/perms: chown to username + chmod 0600 rather than leaving it
// root-owned or world-readable — this is a real X11 auth cookie (control
// over that X session), so it should be exactly as reachable as the rest of
// what this agent already has access to (real root, CAP_SYS_ADMIN over the
// framebuffer, uinput) and no more.
func xsetupHookBlock(username string) string {
	return fmt.Sprintf(`
%s
mkdir -p /run/usbridge-agent
cp "$XAUTHORITY" /run/usbridge-agent/greeter-xauth 2>/dev/null
echo "$DISPLAY" > /run/usbridge-agent/greeter-display
chown %[2]s:%[2]s /run/usbridge-agent/greeter-xauth /run/usbridge-agent/greeter-display 2>/dev/null
chmod 0600 /run/usbridge-agent/greeter-xauth /run/usbridge-agent/greeter-display 2>/dev/null
# --- end USBridge greeter-capture hook ---
`, xsetupHookMarker, systemdQuote(username))
}

// writeXsetupHookScript writes xsetupHookBlock's content to a temp file (same
// pattern Enable() uses for the systemd unit itself) and returns a shell
// fragment that, run as root, appends that file to Xsetup exactly once —
// guarded on both "Xsetup exists at all" (no-ops on non-SDDM systems) and
// "marker not already present" (idempotent across repeat Enable() calls).
// The temp file is intentionally not cleaned up by the caller before the
// pkexec script below has run; Enable() removes it after.
func writeXsetupHookScript(username string) (script, tmpPath string, err error) {
	tmp, err := os.CreateTemp("", "usbridge-xsetup-hook-*")
	if err != nil {
		return "", "", err
	}
	defer tmp.Close()
	if _, err := tmp.WriteString(xsetupHookBlock(username)); err != nil {
		return "", "", err
	}
	script = fmt.Sprintf(
		`if [ -f %[1]s ] && ! grep -qF %[2]s %[1]s; then cat %[3]s >> %[1]s; fi`,
		sddmXsetupPath, systemdQuote(xsetupHookMarker), tmp.Name(),
	)
	return script, tmp.Name(), nil
}

// pkexecAuthAgentProcessPattern matches any already-running standalone
// polkit authentication agent's process name, across desktop environments —
// used by ensurePolkitAuthAgent to tell "nothing registered yet" apart from
// "one's already up, do nothing". A var (not a const) purely so tests can
// point it at a known-running process instead of a real polkit agent.
var pkexecAuthAgentProcessPattern = `polkit-(gnome|mate|kde)-authentication-agent|polkit-kde-agent|lxqt-policykit-agent|xfce-polkit|lxpolkit`

// pkexecAuthAgentCandidates lists known standalone polkit authentication
// agent binaries across desktop environments and common distro path
// layouts. Tried in a fixed order rather than picked by $XDG_CURRENT_DESKTOP
// (more than one is often installed side by side — e.g. Mint's Cinnamon
// ships both mate-polkit and GNOME's agent — and polkit only needs *one*
// registered for the session, so the first candidate that exists on disk is
// enough).
var pkexecAuthAgentCandidates = []string{
	"/usr/lib/policykit-1-gnome/polkit-gnome-authentication-agent-1",
	"/usr/libexec/polkit-gnome-authentication-agent-1",
	"/usr/lib/x86_64-linux-gnu/libexec/polkit-gnome-authentication-agent-1",
	"/usr/libexec/polkit-mate-authentication-agent-1",
	"/usr/lib/x86_64-linux-gnu/polkit-mate/polkit-mate-authentication-agent-1",
	"/usr/lib/x86_64-linux-gnu/libexec/polkit-kde-authentication-agent-1",
	"/usr/lib/polkit-kde-authentication-agent-1",
	"/usr/bin/lxqt-policykit-agent",
	"/usr/lib/x86_64-linux-gnu/xfce-polkit/xfce-polkit",
	"/usr/lib/xfce-polkit/xfce-polkit",
	"/usr/bin/lxpolkit",
	"/usr/lib/lxpolkit/lxpolkit",
}

// ensurePolkitAuthAgent makes a best-effort attempt to have a graphical
// polkit authentication agent registered for this session before Enable/
// Disable call pkexec, working around a gap confirmed live on Linux Mint
// (Cinnamon): its MATE polkit agent is restricted to MATE only
// (OnlyShowIn=MATE; in polkit-mate-authentication-agent-1.desktop) and the
// GNOME one that does list Cinnamon (OnlyShowIn=...;X-Cinnamon;) isn't
// always actually running for a given login. Without any agent registered,
// pkexec falls back to text-mode authentication, which needs a controlling
// terminal — something this GUI process never has — and fails outright with
// a cryptic "Error opening current controlling terminal for the process
// (`/dev/tty')" instead of ever showing a password prompt.
//
// No-ops whenever an agent is already running — confirmed by every desktop
// environment this already works fine on (GNOME, KDE, XFCE, ...) never
// matching the spawn branch below. Best-effort throughout: any failure here
// is silently ignored and the caller proceeds to invoke pkexec exactly as
// before, so a distro missing every candidate binary (or missing pgrep) sees
// no behavior change at all — this can only ever help, never regress an
// already-working setup.
func ensurePolkitAuthAgent() {
	if out, err := exec.Command("pgrep", "-f", pkexecAuthAgentProcessPattern).Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		return
	}
	for _, candidate := range pkexecAuthAgentCandidates {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		cmd := exec.Command(candidate)
		// New session, detached from this process's own lifetime: the agent
		// needs to keep running and registered for as long as the desktop
		// session lasts, not just for this one pkexec call.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			continue
		}
		// Give it a moment to register with the session bus before pkexec
		// asks polkitd who's listening — registration is one async D-Bus
		// call, so this is generous headroom, not a real readiness wait.
		time.Sleep(300 * time.Millisecond)
		return
	}
}

// friendlyPkexecError turns pkexec's own stderr/exit-status into an
// actionable message for the two failure modes actually seen live, instead
// of surfacing raw polkit internals — same convention as
// permissions.Service's pkexec error handling (service_linux.go).
func friendlyPkexecError(err error, out []byte) string {
	text := string(out)
	switch {
	case strings.Contains(text, "/dev/tty") || strings.Contains(text, "No authentication agent found"):
		return "no polkit authentication agent is running for this session " +
			"(pkexec needs one to prompt for the password). Log into a full desktop " +
			"session and make sure its polkit agent is running, then retry."
	case strings.Contains(err.Error(), "exit status 126"):
		return "authentication was cancelled or dismissed. Try again and approve the prompt."
	default:
		return fmt.Sprintf("%v (%s)", err, strings.TrimSpace(text))
	}
}

func IsEnabled() bool {
	out, err := exec.Command("systemctl", "is-enabled", unitName).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "enabled"
}

func Enable() error {
	exe, args, err := LaunchTarget()
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("resolve current user: %w", err)
	}

	execParts := make([]string, 0, len(args)+1)
	execParts = append(execParts, systemdQuote(exe))
	for _, a := range args {
		execParts = append(execParts, systemdQuote(a))
	}

	// XDG_RUNTIME_DIR/DBUS_SESSION_BUS_ADDRESS are normally set by pam_systemd
	// on interactive login. A system unit (see the comment above on why this
	// is a system rather than --user unit) gets neither, so without setting
	// them explicitly here, every child process (this agent's own `pactl`
	// calls in internal/audio, and Sunshine itself, which inherits this
	// process's environment — see sunshine.Process.Start) can never reach the
	// user's PipeWire/PulseAudio session: not just "before login", but
	// permanently, since a unit's Environment= is fixed at service start and
	// never picks up a session that comes up later. loginctl enable-linger
	// is what actually gets PipeWire/WirePlumber running at boot without a
	// physical login in the first place; the two together make audio_sink
	// enumeration and Sunshine's own audio capture work headlessly.
	//
	// WAYLAND_DISPLAY (see detectWaylandDisplay) fixes a related problem for
	// Sunshine's KMS capture: without it, Sunshine can't open a Wayland
	// connection, so it never runs its xdg-output correlation step and each
	// monitor's absolute-mouse offset falls back to its raw CRTC x/y — 0 for
	// every connector independently on hardware where each output has its
	// own CRTC — silently breaking multi-monitor absolute mouse input. Like
	// audio, this only helps when the unit is (re-)enabled while a real
	// compositor is already running: there's no enable-linger equivalent
	// that brings up a Wayland compositor from nothing on a true headless
	// boot before any login, since unlike PipeWire it needs actual display
	// hardware and a logged-in session.
	//
	// After/Wants=dev-uinput.device: without this, the unit's own
	// After=network.target ordering says nothing about /dev/uinput, so this
	// service can start (and probe AccessibilityGranted/open the device) well
	// before udev has finished creating and applying permissions to it --
	// especially right after boot, when the real "uinput" kernel module
	// (forced to load by permissions.uinputModulesLoadPath, see
	// service_linux.go) is still racing against every other sysinit unit.
	// systemd only marks a device unit active once udev has fully processed
	// its rules (our GROUP=usbridge-input, MODE=0660 rule included -- see
	// permissions.uinputRuleContent), so waiting on it here is what
	// actually makes the granted permission deterministic across a reboot
	// instead of a race the agent sometimes loses. Wants= (not Requires=) so
	// a machine where the module fails to load for some unrelated reason
	// still starts the agent after systemd's device timeout, same as today,
	// rather than being blocked forever.
	//
	// Deliberately After=network.target, not network-online.target: the
	// latter blocks on NetworkManager-wait-online, which in turn blocks on
	// every autoconnect profile settling — including Wi-Fi profiles whose
	// secrets live in kwallet and aren't available until the desktop
	// session's secret agent registers post-login. On a wired-plus-saved-
	// Wi-Fi machine that race can eat 60+ seconds and made this unit look
	// like it never started. Nothing here actually needs "online" at
	// process start: Run() launches tsnet in a goroutine (see
	// initTailscale/startTailscaleHTTP in app.go) that connects and retries
	// on its own whenever the network shows up, so network.target (reached
	// as soon as the stack exists, independent of DHCP/Wi-Fi/secrets) is
	// sufficient.
	unit := fmt.Sprintf(`[Unit]
Description=USBridge Agent
After=network.target dev-uinput.device
Wants=dev-uinput.device

[Service]
Type=simple
User=%s
Environment=HOME=%s
Environment=APPIMAGE_EXTRACT_AND_RUN=1
Environment=XDG_RUNTIME_DIR=/run/user/%s
Environment=DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%s/bus
Environment=WAYLAND_DISPLAY=%s
ExecStart=%s
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
`, u.Username, u.HomeDir, u.Uid, u.Uid, detectWaylandDisplay(u.Uid), strings.Join(execParts, " "))

	tmp, err := os.CreateTemp("", "usbridge-agent-*.service")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(unit); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	// tmp.Name() and unitPath are both Go/constant-generated paths with no
	// spaces or shell metacharacters, so no extra shell-quoting is needed
	// here — only the ExecStart= line inside the unit itself (systemdQuote,
	// above) handles a path that might contain them.
	// enable-linger starts the user's own systemd instance (and with it
	// PipeWire/WirePlumber, which the audio_sink lookups and Sunshine's audio
	// capture both depend on) at boot, without requiring an interactive
	// login — otherwise XDG_RUNTIME_DIR above points at a session that never
	// comes up on a headless boot.
	//
	// usermod -aG render: /dev/dri/renderD128's static group is "render",
	// but logind additionally grants it to whichever user's session is
	// currently *active* on the seat via a dynamic "uaccess" ACL entry —
	// and that entry names only one user at a time. This unit runs as
	// u.Username unconditionally, including while a *different* session
	// (SDDM's greeter, or another user via fast-user-switching) owns the
	// active seat, at which point logind reassigns that ACL entry away
	// from u.Username and the render node's group bit is the only
	// remaining path in. Without static "render" membership, Sunshine's/
	// rust-shine's Vulkan and GBM zero-copy capture paths can't open the
	// render node at all during that window — confirmed live: every
	// captured frame gets dropped ("not raster-valid ... zero-copy isn't
	// supported") for as long as the seat's active session isn't
	// u.Username, which is exactly why capture only starts working right
	// after logging in (that's when the uaccess ACL — or now, this static
	// group membership — actually grants access). Group changes only apply
	// to *new* logins/processes, so a service that was already running
	// under the old group list needs an explicit restart to pick it up —
	// but only when the group was actually just added: `enable --now`
	// alone is enough on every other (overwhelmingly common) re-Enable,
	// and unconditionally restarting here would drop an in-progress stream
	// every time the user merely re-toggles the autostart setting.
	// `getent group render` guards systems with no DRM render node (no
	// GPU, or a driver that doesn't expose one) where the group doesn't
	// exist at all.
	xsetupScript, xsetupTmp, err := writeXsetupHookScript(u.Username)
	if err != nil {
		return fmt.Errorf("prepare greeter-capture hook: %w", err)
	}
	defer os.Remove(xsetupTmp)

	// The greeter-capture hook append (xsetupScript) runs unconditionally
	// after the main install chain regardless of that chain's outcome — it's
	// an independent, best-effort step (and a no-op on non-SDDM systems) —
	// but its own exit status must not clobber the real install chain's
	// result, so that's captured into $STATUS first and re-asserted via the
	// trailing `exit` rather than left as whatever ran last.
	// ADDED_UINPUT mirrors ADDED_RENDER exactly, for the same reason: this
	// unit runs as u.Username unconditionally, including at boot before any
	// login and while a different session (SDDM's greeter) owns the active
	// seat, so /dev/uinput's dynamic uaccess ACL can't be relied on -- static
	// membership in permissions.UinputGroupName (the group the udev rule in
	// internal/permissions/service_linux.go actually grants) is what makes
	// input injection work reliably from this unit. Unlike "render", that
	// group isn't provided by the kernel/distro, so it's created here too
	// (idempotently) rather than just checked for, in case autostart is
	// enabled before RequestAccessibility has ever run.
	script := fmt.Sprintf(
		`ADDED_RENDER=0; if getent group render >/dev/null && ! id -nG %[4]s | grep -qw render; then usermod -aG render %[4]s && ADDED_RENDER=1; fi; `+
			`ADDED_UINPUT=0; getent group %[6]s >/dev/null || groupadd %[6]s; if ! id -nG %[4]s | grep -qw %[6]s; then usermod -aG %[6]s %[4]s && ADDED_UINPUT=1; fi; `+
			`install -m 0644 %[1]s %[2]s && systemctl daemon-reload && systemctl enable --now %[3]s && loginctl enable-linger %[4]s && `+
			`if [ "$ADDED_RENDER" = 1 ] || [ "$ADDED_UINPUT" = 1 ]; then systemctl restart %[3]s; fi; `+
			`STATUS=$?; %[5]s; exit $STATUS`,
		tmp.Name(), unitPath, unitName, systemdQuote(u.Username), xsetupScript, permissions.UinputGroupName,
	)
	ensurePolkitAuthAgent()
	cmd := exec.Command("pkexec", "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install systemd service: %s", friendlyPkexecError(err, out))
	}
	return nil
}

func Disable() error {
	script := fmt.Sprintf(
		"systemctl disable --now %s >/dev/null 2>&1; rm -f %s && systemctl daemon-reload",
		unitName, unitPath,
	)
	ensurePolkitAuthAgent()
	cmd := exec.Command("pkexec", "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove systemd service: %s", friendlyPkexecError(err, out))
	}
	return nil
}
