// Package iscsi implements the agent's role as an iSCSI initiator: it logs
// into a target exported by the client (see client/internal/service/iscsi_target.go),
// discovers the resulting local block device, and logs out again on unmount.
//
// This is a small interface + per-OS implementation, mirroring the
// composition pattern already used by internal/streamhost (Backend
// interface, platform-specific files, factory function).
package iscsi

import "context"

// LoginOptions describes an iSCSI target to log into.
type LoginOptions struct {
	Portal   string // host:port, e.g. "192.168.1.50:3260"
	TargetIQN string
	LUN      int

	CHAPUsername string // optional
	CHAPSecret   string // optional
}

// LoginResult is what a successful Login returns.
type LoginResult struct {
	// DevicePath is the OS-local block device path (e.g. "/dev/sdb" on
	// Linux, a physical drive identifier on Windows).
	DevicePath string
	// SessionID is an opaque, initiator-specific handle Logout needs to
	// tear the session back down (Linux: unused, we log out by IQN/portal;
	// kept for forward-compat with initiators that need a session handle).
	SessionID string
}

// Initiator is the agent-side iSCSI initiator contract.
type Initiator interface {
	// Login logs into the target and returns the resulting local block
	// device. Must be safe to call concurrently for different targets.
	Login(ctx context.Context, opts LoginOptions) (LoginResult, error)
	// Logout logs out of a previously logged-in target. Must be
	// idempotent — logging out twice (or a target that was never logged
	// in) should not be treated as a hard error by callers.
	Logout(ctx context.Context, opts LoginOptions) error
	// Available reports whether this platform can act as an iSCSI
	// initiator at all (e.g. false on macOS, which has no built-in
	// initiator).
	Available() bool
}
