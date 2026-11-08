package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Channel selects which GitHub releases the updater follows.
type Channel string

const (
	// ChannelStable follows /releases/latest, the newest non-prerelease.
	ChannelStable Channel = "stable"
	// ChannelTest follows the newest release, pre-releases included.
	ChannelTest Channel = "test"
)

var (
	channelMu     sync.Mutex
	channelLoaded bool
	channelValue  = ChannelStable
)

// InAppUpdates reports whether the user can update from inside the app. It is
// off on iOS and in market builds, where the store delivers updates, and in the
// web client, which is always the deployed version.
func InAppUpdates() bool { return inAppUpdatesOn(runtime.GOOS) }

func inAppUpdatesOn(goos string) bool { return Enabled && goos != "ios" && goos != "js" }

// channelFilePath is where the choice persists between launches.
func channelFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "USBridge", appName+"-update-channel")
}

// GetChannel returns the selected channel. USBRIDGE_UPDATE_CHANNEL=test
// overrides the stored choice, so a CI/dev run needs no UI.
func GetChannel() Channel {
	// Without in-app updates there is no switch to leave Test with, and an
	// earlier choice would pin What's new to pre-release notes.
	if !InAppUpdates() {
		return ChannelStable
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("USBRIDGE_UPDATE_CHANNEL")), string(ChannelTest)) {
		return ChannelTest
	}
	channelMu.Lock()
	defer channelMu.Unlock()
	if !channelLoaded {
		channelLoaded = true
		if p := channelFilePath(); p != "" {
			if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) == string(ChannelTest) {
				channelValue = ChannelTest
			}
		}
	}
	return channelValue
}

// SetChannel switches the channel and persists it best-effort; the
// in-memory value applies even if the config dir is unwritable.
func SetChannel(c Channel) {
	if c != ChannelTest {
		c = ChannelStable
	}
	channelMu.Lock()
	channelLoaded = true
	channelValue = c
	channelMu.Unlock()
	p := channelFilePath()
	if p == "" {
		return
	}
	if c == ChannelStable {
		_ = os.Remove(p)
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
		_ = os.WriteFile(p, []byte(c), 0o644)
	}
}
