package app

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// engineLockPath is the exclusive lock file that marks which process
// currently owns the engine (HTTP server, streamhost backend, tsnet, admin
// socket) for a given state dir. It exists to close a real race in Start's
// old logic: that code decided "am I the engine owner" purely by dialing
// the admin socket and seeing if anyone answered within a short timeout.
// Two processes racing that check (or one dial spuriously timing out
// against an otherwise-healthy instance -- e.g. under load, or a socket
// file freshly unlinked-and-rebound by a third process) could both
// conclude "nobody's home" and both go on to bind their own HTTP server,
// streamhost backend, and admin socket -- confirmed live: a headless
// LaunchAgent instance and a manually-launched GUI instance both ended up
// owning a streamhost.Backend at once, fighting over the same GameStream
// ports (47989/47990/47984), with the loser's stale credentials file
// getting used against the winner's process and every PIN submission
// failing with 401.
//
// flock is what actually prevents that: only one process can ever hold
// this lock, and the OS releases it the instant the holder's fd closes --
// clean exit, panic, or SIGKILL -- with no possibility of it surviving the
// process (unlike the admin socket file, which can be left on disk after a
// crash). That's the same "dies with the owner" guarantee a Windows Job
// Object gives a child process, just applied one layer up, to the decision
// of which process gets to *be* the engine at all.
func engineLockPath(stateDir string) string {
	return filepath.Join(stateDir, "engine.lock")
}

// acquireEngineLock attempts to take stateDir's exclusive engine lock,
// stamping this process's PID into the file on success. The returned file
// must be kept open (never closed, never garbage collected) for as long as
// this process owns the engine -- see engineLockPath's doc comment.
//
// acquired is false (with lockFile nil) if some other process already
// holds the lock; holderPID is read back from the file's contents in that
// case (best-effort -- 0 if the file is empty/corrupt, which can happen if
// this raced the holder's own PID write, or the file predates this PID
// stamping and pre-dates a crash that never got to write one).
func acquireEngineLock(stateDir string) (lockFile *os.File, holderPID int, acquired bool, err error) {
	if stateDir == "" {
		return nil, 0, false, nil
	}
	path := engineLockPath(stateDir)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, 0, false, err
	}

	ok, lockErr := tryLockExclusive(f)
	if lockErr != nil {
		_ = f.Close()
		return nil, 0, false, lockErr
	}
	if !ok {
		holderPID = readLockPID(f)
		_ = f.Close()
		return nil, holderPID, false, nil
	}

	// Stamp our own PID in now that we hold the lock -- truncate first
	// since a stale PID from a prior holder (or our own prior run) may be
	// longer than what we're about to write.
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, 0, false, err
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0); err != nil {
		_ = f.Close()
		return nil, 0, false, err
	}
	return f, 0, true, nil
}

func readLockPID(f *os.File) int {
	data := make([]byte, 64)
	n, _ := f.ReadAt(data, 0)
	fields := strings.Fields(string(data[:n]))
	if len(fields) == 0 {
		return 0
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}
	return pid
}

// stampEngineMode appends "headless" or "gui" after the PID in the lock file
// so a later launch can tell what kind of process owns the engine.
func stampEngineMode(f *os.File, headless bool) {
	if f == nil {
		return
	}
	mode := "gui"
	if headless {
		mode = "headless"
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+" "+mode), 0)
}

// engineHolderIsHeadless reports whether the current holder stamped itself
// headless (unstamped/legacy lock files read as "no").
func engineHolderIsHeadless(stateDir string) bool {
	data, err := os.ReadFile(engineLockPath(stateDir))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data))
	return len(fields) >= 2 && fields[1] == "headless"
}

// evictEngineLockHolder gets holderPID to stop owning the engine so a
// fresh acquireEngineLock can succeed. It tries a cooperative shutdown
// first (dialing holderPID's admin socket and asking it to relinquish
// gracefully -- see relinquishEngine), which lets that process tear down
// its streamhost backend, tsnet, and HTTP server cleanly instead of having
// them vanish mid-syscall. Only if that fails to make the lock available
// within a short deadline (the holder is unresponsive: crashed but never
// reaped, or a stale PID left over from an unclean shutdown that never got
// to remove its own lock file) does it fall back to killing holderPID
// outright.
//
// Returns true once the lock file looks free to re-acquire (best-effort --
// the caller still needs to actually call acquireEngineLock to confirm).
func evictEngineLockHolder(stateDir string, socketPath string, holderPID int) bool {
	if holderPID <= 0 {
		// No usable PID on record (empty/corrupt lock file) -- nothing to
		// signal. The caller's next acquireEngineLock will tell us whether
		// the lock is actually still held by something.
		return true
	}

	log.Printf("[app] engine lock held by pid=%d -- requesting graceful relinquish before considering a hard kill", holderPID)
	if client, err := dialAdminSocket(socketPath, 2*time.Second); err == nil {
		relinquishErr := client.RelinquishEngine()
		client.Close()
		if relinquishErr == nil {
			// Give the holder a moment to actually finish tearing down and
			// close its lock fd -- RelinquishEngine's RPC returns once the
			// holder has *started* shutting down, not once the fd is closed.
			if waitForLockFree(stateDir, 5*time.Second) {
				log.Printf("[app] pid=%d relinquished the engine gracefully", holderPID)
				return true
			}
			log.Printf("[app] pid=%d acknowledged relinquish but never actually freed the lock -- falling back to a hard kill", holderPID)
		} else {
			log.Printf("[app] pid=%d did not relinquish gracefully: %v -- falling back to a hard kill", holderPID, relinquishErr)
		}
	} else {
		log.Printf("[app] pid=%d's admin socket is unresponsive (%v) -- treating it as stale and killing it directly", holderPID, err)
	}

	if !killPID(holderPID) {
		log.Printf("[app] pid=%d was already gone", holderPID)
		return true
	}
	log.Printf("[app] killed stale/unresponsive engine-owner pid=%d", holderPID)
	return waitForLockFree(stateDir, 3*time.Second)
}

// waitForLockFree polls whether stateDir's engine lock can be acquired
// (immediately releasing it again if so -- this is only a probe) until it
// can, or deadline passes.
func waitForLockFree(stateDir string, deadline time.Duration) bool {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		f, _, ok, err := acquireEngineLock(stateDir)
		if err == nil && ok {
			_ = f.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
