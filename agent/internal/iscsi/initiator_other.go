//go:build !linux && !windows

package iscsi

import (
	"context"
	"fmt"
)

// otherInitiator covers macOS and any other platform with no built-in
// iSCSI initiator support. Explicit "unsupported" rather than a silent
// no-op, so callers surface a clear error instead of a mysterious mount
// failure.
type otherInitiator struct{}

// New returns a stub initiator that reports itself unavailable.
func New() Initiator {
	return &otherInitiator{}
}

func (otherInitiator) Available() bool { return false }

func (otherInitiator) Login(ctx context.Context, opts LoginOptions) (LoginResult, error) {
	return LoginResult{}, fmt.Errorf("iSCSI initiator is not supported on this platform yet")
}

func (otherInitiator) Logout(ctx context.Context, opts LoginOptions) error {
	return nil
}
