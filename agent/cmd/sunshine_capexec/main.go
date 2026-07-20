// sunshine_capexec grants CAP_SYS_ADMIN to a target process without that
// process's own binary ever carrying a file capability.
//
// Linux puts any binary with a file capability (or setuid/setgid bit) into
// "secure execution" mode, in which the dynamic linker ignores
// RPATH/RUNPATH and LD_LIBRARY_PATH — the same protection that stops a
// setuid binary from being tricked into loading an attacker-controlled
// library. Sunshine needs CAP_SYS_ADMIN for its KMS screen-capture backend,
// but it also resolves its bundled shared library dependencies (e.g.
// libminiupnpc.so.17) via RPATH=$ORIGIN/../lib — setting the capability
// directly on the sunshine binary breaks that resolution the moment it's
// granted.
//
// This binary is the thing that actually gets `setcap cap_sys_admin=eip`
// (see internal/permissions/service_linux.go). It is built fully static
// (CGO_ENABLED=0) so it has zero dynamic library dependencies of its own —
// secure-execution mode applying to it is a non-issue. At runtime it raises
// CAP_SYS_ADMIN into its ambient capability set, which — per capabilities(7)
// — is preserved across execve(2) into a program that itself carries no
// file capabilities or setuid/setgid bits. Sunshine (the exec target) stays
// exactly such a plain binary, so it inherits CAP_SYS_ADMIN via the ambient
// set without ever being placed into secure-execution mode itself, and its
// RPATH keeps resolving normally.
//
// Usage: sunshine_capexec <target-binary> [args...]
package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// capSysAdmin is CAP_SYS_ADMIN's numeric value (linux/capability.h) — the
// same capability internal/permissions/service_linux.go grants via setcap.
const capSysAdmin = 21

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: sunshine_capexec <target-binary> [args...]")
		os.Exit(1)
	}
	target := os.Args[1]
	args := os.Args[1:]

	// The agent sets PR_SET_PDEATHSIG=SIGKILL on the process it forks so
	// Sunshine can never outlive it (crash, SIGKILL, a shutdown path that
	// hangs before reaching its own cleanup). But per capabilities(7)/
	// prctl(2), the kernel silently clears PDEATHSIG the moment that forked
	// process execve()s a binary carrying a file capability — which is
	// exactly what just happened: this binary is `setcap cap_sys_admin=eip`.
	// Left unaddressed, Sunshine (execve'd below into this same, now
	// deathsig-less process image) becomes an unkillable orphan the instant
	// KMS capture is in use — reparented to init instead of dying with the
	// agent. Re-arm it here before the final exec: this process itself
	// carries no file capability, so the clear-on-exec rule doesn't strip it
	// a second time, and Sunshine inherits it same as in non-KMS mode.
	if err := unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "sunshine_capexec: set PDEATHSIG: %v\n", err)
	}

	// `setcap cap_sys_admin=eip` on this binary puts CAP_SYS_ADMIN in our
	// effective and permitted sets at exec time (via the file's fP,
	// intersected with our capability bounding set) — but NOT in our
	// inheritable set: per capabilities(7), a freshly exec'd process's
	// inheritable set is inherited unchanged from its parent (an ordinary,
	// unprivileged shell in the normal case), regardless of the file's own
	// "i" bit. Ambient-raise requires the capability in BOTH permitted and
	// inheritable, so add it to our own inheritable set first — allowed
	// because we already hold it in our permitted set.
	if err := addInheritable(capSysAdmin); err != nil {
		fmt.Fprintf(os.Stderr, "sunshine_capexec: add CAP_SYS_ADMIN to inheritable set: %v\n", err)
		os.Exit(1)
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_RAISE, capSysAdmin, 0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "sunshine_capexec: raise ambient CAP_SYS_ADMIN: %v\n", err)
		os.Exit(1)
	}

	if err := unix.Exec(target, args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "sunshine_capexec: exec %s: %v\n", target, err)
		os.Exit(1)
	}
}

// addInheritable adds capNum (already held in this process's permitted set)
// to its inheritable set. capset(2) replaces the full set, so
// read-modify-write.
func addInheritable(capNum uint) error {
	var hdr unix.CapUserHeader
	var data [2]unix.CapUserData
	hdr.Version = unix.LINUX_CAPABILITY_VERSION_3
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		return err
	}
	word, bit := capNum/32, capNum%32
	data[word].Inheritable |= 1 << bit
	return unix.Capset(&hdr, &data[0])
}
