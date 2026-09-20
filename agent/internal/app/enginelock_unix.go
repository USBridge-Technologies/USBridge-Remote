//go:build !windows

package app

import (
	"os"
	"syscall"
)

// tryLockExclusive attempts a non-blocking exclusive flock on f. ok is true
// if this process now holds it, false if some other process already does
// (EWOULDBLOCK) -- not an error case, the caller is expected to check ok.
func tryLockExclusive(f *os.File) (ok bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if err == syscall.EWOULDBLOCK {
		return false, nil
	}
	return false, err
}

// killPID sends SIGKILL to pid. Returns false if the process was already
// gone (ESRCH) or otherwise unreachable -- both treated as "nothing left to
// do" by callers rather than an error worth surfacing.
func killPID(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return true
}
