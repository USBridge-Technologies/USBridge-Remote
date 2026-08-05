package streamhost

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// upsertConfigKey and readConfigKey implement the plain "key = value" line
// config file format shared by every backend this package supports
// (Sunshine's sunshine.conf and rust-shine's positionally-specified config
// file use the identical format).

// configFileLocks serializes upsertConfigKey's read-modify-write sequence
// per config path. The admin HTTP API dispatches each request on its own
// goroutine (net/http's default), and a real client has been observed
// firing two device/capture-mode changes milliseconds apart (e.g. two
// video_set_device calls 6ms apart) -- without this, two goroutines each
// read the file's old contents, computed different `lines` slices, and
// wrote their own version back with a bare os.WriteFile (open+truncate,
// not atomic), so whichever write landed last silently discarded the
// other's key AND could interleave truncate/write syscalls with it,
// observed live corrupting the file mid-line (a `capture = kms` line
// dropped entirely, another line sheared at a byte offset) -- not just a
// lost update, actual on-disk corruption. Keyed by path (not a single
// global lock) so Sunshine's and rust-shine's independent config files
// never block on each other.
var (
	configFileLocksMu sync.Mutex
	configFileLocks    = map[string]*sync.Mutex{}
)

func configFileLock(path string) *sync.Mutex {
	configFileLocksMu.Lock()
	defer configFileLocksMu.Unlock()
	l, ok := configFileLocks[path]
	if !ok {
		l = &sync.Mutex{}
		configFileLocks[path] = l
	}
	return l
}

// acquireCrossProcessLock guards upsertConfigKey's read-modify-write-rename
// against a second, wholly separate OS process doing the same thing at the
// same time -- the in-process mutex above only serializes goroutines within
// one process, but this config path is also reachable from a GUI "thin
// client" instance running alongside the headless daemon (see
// cmd/usbridge_agent's runThinClientGUI): normally the thin client proxies
// every stream setting through the daemon's admin HTTP API rather than
// touching this file itself, but a plain filesystem lock costs nothing and
// removes the possibility entirely rather than depending on every current
// and future caller getting that proxying right. Implemented as a lockfile
// via O_CREATE|O_EXCL (atomic, and portable across every OS Go supports,
// unlike flock(2)) instead of a real dependency -- config writes are rare
// enough that a short poll loop is not worth pulling one in for.
func acquireCrossProcessLock(path string) func() {
	lockPath := path + ".lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(lockPath) }
		}
		if !os.IsExist(err) {
			// Can't even attempt the lock (e.g. directory missing) -- fall
			// through and let the write itself fail/succeed on its own
			// rather than blocking forever on a lock we can't take.
			return func() {}
		}
		if time.Now().After(deadline) {
			// Almost certainly a stale lock left by a process that crashed
			// mid-write rather than genuine 5s+ contention -- steal it
			// instead of wedging every future config write on this path.
			os.Remove(lockPath)
			continue
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// upsertConfigKey upserts a single "key = value" line in the file at path.
// An empty value removes the key (falling back to the backend's own
// default/auto behavior). The file is created if missing; other
// keys/values are preserved verbatim.
func upsertConfigKey(path, key, value string) error {
	if path == "" {
		return os.ErrNotExist
	}
	lock := configFileLock(path)
	lock.Lock()
	defer lock.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	unlockCrossProcess := acquireCrossProcessLock(path)
	defer unlockCrossProcess()

	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, key) && strings.Contains(trimmed, "=") {
				parts := strings.SplitN(trimmed, "=", 2)
				if strings.TrimSpace(parts[0]) == key {
					continue // drop existing line, re-added below if needed
				}
			}
			lines = append(lines, line)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	value = strings.TrimSpace(value)
	if value != "" {
		lines = append(lines, key+" = "+value)
	}

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}

	// Write via temp file + rename instead of a direct os.WriteFile
	// (open+truncate+write): rename is atomic on POSIX, so a reader (or
	// gamestream-server re-reading this file on its own restart) never
	// observes a half-written file even under a crash or a write this
	// mutex didn't cover (e.g. a concurrent process outside this agent).
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readConfigKey reads the current value of a "key = value" line from the
// file at path, or "" if unset or the file doesn't exist.
func readConfigKey(path, key string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, key) {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}
