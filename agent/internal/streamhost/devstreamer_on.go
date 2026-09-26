//go:build devstreamer

package streamhost

import (
	"log"
	"os"
	"path/filepath"
	"sync"
)

// devStreamerEnv names a locally built usbridge-streamer the agent runs
// instead of the staged, signed release -- for debugging rust-shine changes
// on a real agent install without cutting a release. Only compiled into
// agents built with `-tags devstreamer` (scripts/build_linux.sh
// DEV_STREAMER=1); release builds get devstreamer_off.go, which has neither
// this code nor this variable name.
//
// The override binary is exec'd directly, never through
// usbridge-streamer-launch: that root-owned launcher only ever runs the
// signed bundle (it's what stands between "any local file" and
// CAP_SYS_ADMIN), and stays that way. For KMS capture, grant the dev binary
// the capability yourself:
//
//	sudo setcap cap_sys_admin+ep /path/to/usbridge-streamer
//
// (re-run after every rebuild -- cargo writes a new inode). Without it the
// streamer still starts but falls back to its non-KMS capture path.
const devStreamerEnv = "USBRIDGE_DEV_STREAMER"

var devStreamerLogOnce sync.Once

// devStreamerOverride returns the absolute path from USBRIDGE_DEV_STREAMER,
// or "" when unset or not a regular file (logged once, then the normal
// staged binary is used).
func devStreamerOverride() string {
	p := os.Getenv(devStreamerEnv)
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(abs)
		if err == nil && !info.Mode().IsRegular() {
			err = os.ErrInvalid
		}
	}
	if err != nil {
		devStreamerLogOnce.Do(func() {
			log.Printf("[rustshine] DEV: %s=%q unusable (%v), using the staged release", devStreamerEnv, p, err)
		})
		return ""
	}
	devStreamerLogOnce.Do(func() {
		log.Printf("[rustshine] DEV: running local streamer %s (%s), bypassing the signed-release launcher", abs, devStreamerEnv)
	})
	return abs
}
