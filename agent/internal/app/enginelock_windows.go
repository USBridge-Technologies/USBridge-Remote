//go:build windows

package app

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryLockExclusive attempts a non-blocking exclusive lock on f's whole
// range via LockFileEx. ok is true if this process now holds it, false if
// some other process already does (ERROR_LOCK_VIOLATION) -- not an error
// case, the caller is expected to check ok. Mirrors flock's semantics on
// unix (enginelock_unix.go).
func tryLockExclusive(f *os.File) (ok bool, err error) {
	var overlapped windows.Overlapped
	const lockFlags = windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY
	err = windows.LockFileEx(windows.Handle(f.Fd()), lockFlags, 0, 1, 0, &overlapped)
	if err == nil {
		return true, nil
	}
	if err == windows.ERROR_LOCK_VIOLATION {
		return false, nil
	}
	return false, err
}

// killPID force-terminates pid. Returns false if the process could not be
// opened (already gone, or genuinely inaccessible) -- both treated as
// "nothing left to do" by callers rather than an error worth surfacing.
func killPID(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	_ = windows.TerminateProcess(h, 1)
	return true
}
