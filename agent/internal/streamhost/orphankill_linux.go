//go:build linux

package streamhost

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// killOrphansByArgv0 is killOrphanStreamerProcesses's Linux complement to
// killall: a streamer started through usbridge-streamer-launch runs from a
// sealed memfd, so its comm is the memfd's fd name rather than
// "usbridge-streamer" and killall may not match it. The launcher keeps
// argv[0] as the plain binary name, so match that instead -- only for
// processes owned by this uid.
func killOrphansByArgv0(names []string) {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	self, uid := os.Getpid(), uint32(os.Getuid())
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		info, err := os.Stat("/proc/" + e.Name())
		if err != nil {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != uid {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || len(cmdline) == 0 {
			continue
		}
		argv0, _, _ := bytes.Cut(cmdline, []byte{0})
		if !want[filepath.Base(string(argv0))] {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err == nil {
			log.Printf("[streamhost] killed orphaned %s (pid %d, matched by argv[0])", filepath.Base(string(argv0)), pid)
		}
	}
}
