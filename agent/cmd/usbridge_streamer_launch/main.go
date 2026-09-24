//go:build linux

// usbridge_streamer_launch gives usbridge-streamer (RustShine) CAP_SYS_ADMIN
// for KMS capture without the capability ever living on the streamer
// binary, so re-downloading the streamer on update no longer drops the
// grant. See internal/streamerlaunch's package doc for the full rationale.
//
// Installed once, root-owned, by permissions.InstallStreamerLauncher at
// streamerlaunch.InstallPath with `setcap cap_sys_admin=ep`. Built fully
// static (CGO_ENABLED=0) so it has no dynamic loader exposure of its own.
//
// Usage:
//
//	usbridge-streamer-launch --version
//	usbridge-streamer-launch --verify <bundle-dir>
//	usbridge-streamer-launch --run <bundle-dir> [--] [streamer args...]
//	usbridge-streamer-launch --run-sunshine [--] [sunshine args...]
//
// --verify prints "version=<release tag>" and "cap=<0|1>" (whether this
// process actually received CAP_SYS_ADMIN from its file capability -- 0 on
// a nosuid mount or under no_new_privs) and exits 0 only if the bundle
// verifies. --run verifies, then execs the verified bytes from a sealed
// memfd with CAP_SYS_ADMIN raised into the ambient set. --run-sunshine
// execs the root-owned Sunshine tree the same way (see runSunshine).
package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

	"golang.org/x/sys/unix"

	"usbridge_agent/internal/streamerlaunch"
)

const capSysAdmin = unix.CAP_SYS_ADMIN

// Exit codes the agent can tell apart in its log.
const (
	exitUsage    = 2
	exitVerify   = 3
	exitNoCap    = 4
	exitDenied   = 5
	exitExecFail = 6
)

// init pins the main goroutine to the main OS thread: PR_SET_PDEATHSIG
// and the capability sets are per-thread (task) attributes, and execve
// keeps only the calling thread's. Without the pin the Go scheduler can
// run the prctl on one thread and the exec on another -- confirmed live:
// the streamer then outlived a killed agent.
func init() { runtime.LockOSThread() }

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "--version":
		fmt.Printf("usbridge-streamer-launch protocol=%d\n", streamerlaunch.Protocol)
	case "--verify":
		if len(os.Args) != 3 {
			usage()
		}
		v, err := streamerlaunch.LoadVerified(os.Args[2], streamerlaunch.Platform(), streamerlaunch.ReleasePublicKey())
		if err != nil {
			fail(exitVerify, "verify: %v", err)
		}
		fmt.Printf("version=%s\ncap=%s\n", v.Version, boolDigit(havePermitted(capSysAdmin)))
	case "--run-sunshine":
		args := os.Args[2:]
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
		runSunshine(args)
	case "--run":
		if len(os.Args) < 3 {
			usage()
		}
		args := os.Args[3:]
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
		run(os.Args[2], args)
	default:
		usage()
	}
}

func run(bundleDir string, args []string) {
	prepare()

	v, err := streamerlaunch.LoadVerified(bundleDir, streamerlaunch.Platform(), streamerlaunch.ReleasePublicKey())
	if err != nil {
		fail(exitVerify, "verify: %v", err)
	}

	fd, err := sealedMemfd(v.Binary)
	if err != nil {
		fail(exitExecFail, "memfd: %v", err)
	}

	raiseAmbient()

	// argv[0] stays "usbridge-streamer" so ps/cmdline-based process
	// matching (streamhost.killOrphanStreamerProcesses) still finds it --
	// comm itself becomes the memfd's fd name. /proc/self/fd/N is exactly
	// runc's memfd re-exec pattern; the kernel opens the file before
	// closing O_CLOEXEC descriptors, so the CLOEXEC memfd doesn't leak
	// into the streamer.
	argv := append([]string{streamerlaunch.BinaryName}, args...)
	env := streamerlaunch.SanitizedEnv(os.Environ(), false)
	err = unix.Exec("/proc/self/fd/"+strconv.Itoa(fd), argv, env)
	fail(exitExecFail, "exec %s (%s): %v", streamerlaunch.BinaryName, v.Version, err)
}

// runSunshine execs the root-owned Sunshine tree (streamerlaunch.SunshineDir)
// -- never a caller-supplied path. The whole tree is re-checked as
// root-owned on every launch, since its shared libraries run with the same
// capability. cwd is the tree root: Sunshine (SUNSHINE_BUILD_APPIMAGE=ON)
// finds its assets at ./usr/local/assets.
func runSunshine(args []string) {
	prepare()
	if err := streamerlaunch.CheckTreeRootOwned(streamerlaunch.SunshineDir); err != nil {
		fail(exitVerify, "sunshine tree: %v", err)
	}
	if err := os.Chdir(streamerlaunch.SunshineDir); err != nil {
		fail(exitExecFail, "chdir: %v", err)
	}
	raiseAmbient()
	argv := append([]string{streamerlaunch.SunshineBin}, args...)
	env := streamerlaunch.SanitizedEnv(os.Environ(), true)
	err := unix.Exec(streamerlaunch.SunshineBin, argv, env)
	fail(exitExecFail, "exec %s: %v", streamerlaunch.SunshineBin, err)
}

// prepare is the shared prologue of both run modes: PDEATHSIG, caller uid
// allowlist, and a check that the file capability actually took effect.
func prepare() {
	// The agent sets PR_SET_PDEATHSIG=SIGKILL on the child it forks, but the
	// kernel clears it on exec of a file-capability binary -- i.e. exec of
	// this launcher -- which would leave the streamer an orphan outliving
	// the agent. Re-arm before anything else, then bail if the parent
	// already died in the window before the re-arm.
	parent := os.Getppid()
	if err := unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "usbridge-streamer-launch: set PDEATHSIG: %v\n", err)
	}
	if os.Getppid() != parent || parent == 1 {
		os.Exit(exitExecFail)
	}

	if err := streamerlaunch.UIDAllowed(streamerlaunch.AllowedUIDsPath, os.Getuid()); err != nil {
		fail(exitDenied, "%v", err)
	}
	if !havePermitted(capSysAdmin) {
		fail(exitNoCap, "CAP_SYS_ADMIN not granted to %s (not setcap'd, nosuid mount, or no_new_privs)", os.Args[0])
	}
}

// raiseAmbient moves CAP_SYS_ADMIN into the ambient set, which survives
// execve into a binary with no file capability of its own -- so the target
// is never in secure-execution mode and its RPATH keeps resolving.
func raiseAmbient() {
	// Ambient-raise needs the capability in both permitted (from our file
	// capability, `=ep`) and inheritable. A fresh exec's inheritable set
	// comes unchanged from the (unprivileged) parent regardless of the
	// file's own bits, so add it here -- allowed since it's permitted.
	if err := addInheritable(capSysAdmin); err != nil {
		fail(exitNoCap, "add CAP_SYS_ADMIN to inheritable set: %v", err)
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_RAISE, capSysAdmin, 0, 0); err != nil {
		fail(exitNoCap, "raise ambient CAP_SYS_ADMIN: %v", err)
	}
}

// sealedMemfd copies bin into an anonymous memfd and seals it against any
// further change, so the bytes exec'd are exactly the bytes verified.
func sealedMemfd(bin []byte) (int, error) {
	const flags = unix.MFD_CLOEXEC | unix.MFD_ALLOW_SEALING
	// MFD_EXEC (Linux 6.3+) states the intent explicitly so a
	// vm.memfd_noexec=1 default doesn't create a non-executable memfd;
	// older kernels reject the unknown flag with EINVAL, so retry without.
	fd, err := unix.MemfdCreate(streamerlaunch.BinaryName, flags|unix.MFD_EXEC)
	if err == unix.EINVAL {
		fd, err = unix.MemfdCreate(streamerlaunch.BinaryName, flags)
	}
	if err != nil {
		return -1, err
	}
	for off := 0; off < len(bin); {
		n, err := unix.Write(fd, bin[off:])
		if err != nil {
			return -1, err
		}
		off += n
	}
	seals := unix.F_SEAL_SEAL | unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS, seals); err != nil {
		return -1, fmt.Errorf("seal: %w", err)
	}
	return fd, nil
}

func capData() (unix.CapUserHeader, [2]unix.CapUserData, error) {
	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	err := unix.Capget(&hdr, &data[0])
	return hdr, data, err
}

func havePermitted(capNum uint) bool {
	_, data, err := capData()
	if err != nil {
		return false
	}
	return data[capNum/32].Permitted&(1<<(capNum%32)) != 0
}

func addInheritable(capNum uint) error {
	hdr, data, err := capData()
	if err != nil {
		return err
	}
	data[capNum/32].Inheritable |= 1 << (capNum % 32)
	return unix.Capset(&hdr, &data[0])
}

func boolDigit(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: usbridge-streamer-launch --version | --verify <bundle-dir> | --run <bundle-dir> [--] [args...] | --run-sunshine [--] [args...]")
	os.Exit(exitUsage)
}

func fail(code int, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "usbridge-streamer-launch: "+format+"\n", a...)
	os.Exit(code)
}
