package streamhost

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// sunshineDataDir is where this agent keeps EVERY file its managed Sunshine
// reads or writes — config, web-UI credentials, paired-app list, TLS
// keypair, and Sunshine's own internal log — under the agent's own stateDir
// (e.g. ~/Library/Application Support/usbridge-agent/sunshine on macOS,
// %AppData%\usbridge-agent\sunshine on Windows), never Sunshine's own
// generic per-OS default ($HOME/.config/sunshine, or a location relative to
// wherever sunshine.exe happens to be running from).
//
// This is the fix for a real, reported bug: Sunshine's own default config
// resolution has no idea it's running bundled inside USBridge Agent — if a
// user separately/independently installs standalone Sunshine on the same
// machine, both processes' default paths collide, and closing one and
// starting the other silently overwrites the other's web UI password and
// settings. stateDir's directory name ("usbridge-agent") is unique to this
// app, so nothing else can ever default to the same place.
func (b *sunshineBackend) sunshineDataDir() string {
	if b.stateDir == "" {
		return ""
	}
	return filepath.Join(b.stateDir, "sunshine")
}

// ConfigPath returns where this agent keeps sunshine.conf — always inside
// sunshineDataDir, passed to Sunshine explicitly as its positional config
// argument on every launch (see sunshineConfigArgs) rather than relying on
// Sunshine's own default resolution.
func (b *sunshineBackend) ConfigPath() string {
	dir := b.sunshineDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "sunshine.conf")
}

// credentialsFilePath is where this agent keeps sunshine_state.json (web UI
// username/password + pairing state) — passed explicitly via Sunshine's
// "credentials_file=" config override. Without this override, Sunshine
// writes sunshine_state.json to its own default appdata() directory
// *regardless* of the config file path given on the command line (verified
// against the actual bundled binary — the positional config argument alone
// is not enough), so this one matters just as much as ConfigPath itself for
// actually fixing the collision.
func (b *sunshineBackend) credentialsFilePath() string {
	dir := b.sunshineDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "sunshine_state.json")
}

// appsFilePath is where this agent keeps apps.json (the Moonlight app/game
// list) — passed explicitly via Sunshine's "file_apps=" config override,
// for the same reason as credentialsFilePath.
func (b *sunshineBackend) appsFilePath() string {
	dir := b.sunshineDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "apps.json")
}

// pkeyPath/certPath are where this agent keeps the TLS keypair Sunshine
// generates for its HTTPS web UI/streaming — passed explicitly via
// Sunshine's "pkey="/"cert=" config overrides, for the same reason as
// credentialsFilePath.
func (b *sunshineBackend) pkeyPath() string {
	dir := b.sunshineDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "pkey.pem")
}

func (b *sunshineBackend) certPath() string {
	dir := b.sunshineDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "cert.pem")
}

// LogPath returns where this agent keeps Sunshine's own internal
// sunshine.log — always alongside sunshine.conf, passed explicitly via
// Sunshine's "log_path=" config override (see sunshineConfigArgs). Distinct
// from b.logPath, which is this agent's own capture of Sunshine's OS-level
// stdout/stderr (see Start).
func (b *sunshineBackend) LogPath() string {
	dir := b.sunshineDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "sunshine.log")
}

// sunshineConfigArgs returns the CLI arguments that pin every file this
// agent's managed Sunshine reads or writes to sunshineDataDir(), instead of
// Sunshine's own default resolution. Order matters: verified against the
// actual bundled binary, the config-file path must be the *first*
// argument — passing it after --creds silently drops it (CLI11 swallows it
// as extra input to --creds instead of the separate positional config
// argument), and everything falls back to Sunshine's own default directory.
// The "name=value" overrides can follow in any order.
func (b *sunshineBackend) sunshineConfigArgs() []string {
	dir := b.sunshineDataDir()
	if dir == "" {
		return nil
	}
	return []string{
		b.ConfigPath(),
		"credentials_file=" + b.credentialsFilePath(),
		"file_apps=" + b.appsFilePath(),
		"log_path=" + b.LogPath(),
		"pkey=" + b.pkeyPath(),
		"cert=" + b.certPath(),
	}
}

// legacyDataDir returns the directory this agent's managed Sunshine used to
// default to before sunshineDataDir existed — the same generic per-OS
// location an independently-installed standalone Sunshine also defaults
// to, which is exactly the bug sunshineDataDir fixes. Used only by
// migrateLegacyData, to give upgrading installs (which had no choice but to
// use this shared location) a one-time, copy-only path forward without
// losing existing pairings/settings.
func (b *sunshineBackend) legacyDataDir() string {
	if runtime.GOOS == "windows" {
		if b.windowsDir == "" {
			return ""
		}
		return filepath.Join(b.windowsDir, "config")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".config", "sunshine")
}

// migrateLegacyData copies this agent's pre-existing Sunshine state from
// legacyDataDir into sunshineDataDir, once, the first time sunshineDataDir
// doesn't have a config yet — so upgrading installs keep their existing
// pairings/settings instead of silently starting fresh. Deliberately a
// *copy*, never a move or delete: legacyDataDir is a generic shared
// location, so on a machine where it's actually a separate, independent
// Sunshine install rather than this agent's own prior data, leaving it
// completely untouched is what avoids the exact bug this whole change
// fixes. Best-effort — any failure is logged and otherwise ignored, since
// falling back to a fresh Sunshine state is always safe, just less
// convenient than migrating.
func (b *sunshineBackend) migrateLegacyData() {
	newDir := b.sunshineDataDir()
	oldDir := b.legacyDataDir()
	if newDir == "" || oldDir == "" || oldDir == newDir {
		return
	}
	if _, err := os.Stat(filepath.Join(newDir, "sunshine.conf")); err == nil {
		return // already migrated (or already a fresh install of the new layout)
	}
	if _, err := os.Stat(filepath.Join(oldDir, "sunshine.conf")); err != nil {
		return // nothing to migrate
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		log.Printf("[sunshine] could not create %s for legacy data migration: %v", newDir, err)
		return
	}
	if err := copyDir(oldDir, newDir); err != nil {
		log.Printf("[sunshine] legacy data migration from %s to %s failed (starting fresh): %v", oldDir, newDir, err)
		return
	}
	log.Printf("[sunshine] migrated existing config/credentials/apps from %s to %s (leaving the original in place)", oldDir, newDir)
}

// SetConfigKey upserts a single "key = value" line in sunshine.conf.
// An empty value removes the key (falling back to Sunshine's own default).
func (b *sunshineBackend) SetConfigKey(key, value string) error {
	return b.setConfigKey(key, value)
}

// setConfigKey upserts a single "key = value" line in sunshine.conf. An
// empty value removes the key (falling back to Sunshine's own default/auto
// behavior). The file is created if missing; other keys/values are
// preserved verbatim.
func (b *sunshineBackend) setConfigKey(key, value string) error {
	return upsertConfigKey(b.ConfigPath(), key, value)
}

// ConfigKey reads the current value of a "key = value" line from
// sunshine.conf, or "" if unset.
func (b *sunshineBackend) ConfigKey(key string) string {
	return readConfigKey(b.ConfigPath(), key)
}

// SetExternalIP sets the IP address Sunshine advertises to Moonlight clients
// via the external_ip key in sunshine.conf. Pass "" or "0.0.0.0" to remove
// the override (Sunshine auto-detects).
func (b *sunshineBackend) SetExternalIP(ip string) error {
	if ip == "" || ip == "0.0.0.0" {
		return b.setConfigKey("external_ip", "")
	}
	return b.setConfigKey("external_ip", ip)
}

// ExternalIP reads the current external_ip value from sunshine.conf, or ""
// if unset (auto-detect / all interfaces).
func (b *sunshineBackend) ExternalIP() string {
	return b.ConfigKey("external_ip")
}

// SetBindAddress sets (or removes) the bind_address key in sunshine.conf,
// restricting ALL Sunshine servers (web admin + streaming) to the given IP.
// Pass "" or "0.0.0.0" to remove the restriction (bind on all interfaces).
func (b *sunshineBackend) SetBindAddress(ip string) error {
	if ip == "" || ip == "0.0.0.0" {
		return b.setConfigKey("bind_address", "")
	}
	return b.setConfigKey("bind_address", ip)
}

// BindAddress reads the current bind_address from sunshine.conf, or "" if
// unset (Sunshine defaults to all interfaces).
func (b *sunshineBackend) BindAddress() string {
	return b.ConfigKey("bind_address")
}

// SetCaptureMode upserts the "capture" key in sunshine.conf. An empty mode
// removes the key (Sunshine auto-detects: portal on Wayland, x11 on X11).
func (b *sunshineBackend) SetCaptureMode(mode string) error {
	return b.setConfigKey("capture", mode)
}

// CaptureMode reads the current "capture" value from sunshine.conf, or ""
// if unset (auto-detect).
func (b *sunshineBackend) CaptureMode() string {
	return b.ConfigKey("capture")
}

// SetAudioSink upserts the "audio_sink" key in sunshine.conf — which system
// audio output device Sunshine captures from for GameStream. An empty sink
// removes the key (Sunshine falls back to the system default sink).
func (b *sunshineBackend) SetAudioSink(sink string) error {
	return b.setConfigKey("audio_sink", sink)
}

// AudioSink reads the current "audio_sink" value from sunshine.conf, or ""
// if unset (system default).
func (b *sunshineBackend) AudioSink() string {
	return b.ConfigKey("audio_sink")
}

// SetOutputName upserts the "output_name" key in sunshine.conf — which
// connected monitor Sunshine captures, addressed by Sunshine's own
// connected-output index (see display.Connectors). An empty name removes
// the key (Sunshine auto-picks the first output it finds).
func (b *sunshineBackend) SetOutputName(name string) error {
	return b.setConfigKey("output_name", name)
}

// OutputName reads the current "output_name" value from sunshine.conf, or
// "" if unset (auto-pick).
func (b *sunshineBackend) OutputName() string {
	return b.ConfigKey("output_name")
}
