package streamhost

import (
	"log"
	"os/exec"
	"runtime"
)

// streamerProcessNamesUnix/streamerProcessNamesWindows list every process
// name either backend's launcher can ever produce. Used to guarantee a
// clean slate before Start() trusts a reachable admin port -- see
// killOrphanStreamerProcesses's doc comment.
var streamerProcessNamesUnix = []string{"gamestream-server", "usbridge-streamer", "sunshine"}
var streamerProcessNamesWindows = []string{"gamestream-server.exe", "usbridge-streamer.exe", "sunshine.exe"}

// killOrphanStreamerProcesses kills, by name, every process either backend
// could have left running without this Go process holding a live handle to
// it: a previous agent process that exited without a clean Stop() (crash,
// SIGKILL, or a force-quit that raced the OS-level watchdog/Job Object), or
// simply the *other* backend kind still running from before a switch.
//
// Start() (both sunshine_backend.go and rustshine_backend.go) calls this
// whenever it finds the admin port reachable but has no b.proc of its own,
// instead of the old behavior of just trusting whatever credentials file
// happened to be on disk and hoping the reachable service was actually this
// same backend -- confirmed live: it wasn't, a leftover RustShine instance
// from a different agent process answered a Sunshine backend's guessed
// password with 401 on every PIN submission, because the two backends'
// admin credentials are never the same thing. Best-effort and silent about
// individual misses (a name not currently running is the overwhelmingly
// common case, not worth logging every call).
func killOrphanStreamerProcesses() {
	names := streamerProcessNamesUnix
	if runtime.GOOS == "windows" {
		names = streamerProcessNamesWindows
	}
	for _, name := range names {
		var err error
		if runtime.GOOS == "windows" {
			err = hiddenTaskkill("/F", "/IM", name)
		} else {
			err = exec.Command("killall", name).Run()
		}
		if err == nil {
			log.Printf("[streamhost] killed orphaned %s before starting fresh", name)
		}
	}
	if runtime.GOOS != "windows" {
		killOrphansByArgv0(names)
	}
}
