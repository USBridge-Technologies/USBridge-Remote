// Package sunshine locates the Sunshine (Moonlight GameStream host) binary
// and manages the subset of sunshine.conf that the agent controls. On all
// platforms Sunshine is bundled locally next to the agent binary: as
// Sunshine.app on macOS, sunshine.exe on Windows, and sunshine.AppImage on
// Linux. The agent sets web_bind_address=127.0.0.1 before each start so the
// HTTPS admin UI only listens on localhost (requires itsme228/Sunshine fork).
package sunshine

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AdminUser is the fixed Sunshine web-UI username.
const AdminUser = "sunshine"

// activeAdminPassword holds the per-session randomly generated admin password.
// Set in Start() via --creds before Sunshine launches.
var activeAdminPassword string

// adminPass returns the in-memory admin password set this session (internal).
func adminPass() string { return activeAdminPassword }

// AdminPass returns the current session admin password for use by other packages.
// Falls back to the persisted file from the previous session if bootstrap is
// still in progress (it waits up to 20 s for Sunshine to start).
func AdminPass() string {
	if activeAdminPassword != "" {
		return activeAdminPassword
	}
	if pf := adminPassFile(); pf != "" {
		if data, err := os.ReadFile(pf); err == nil {
			if p := strings.TrimSpace(string(data)); p != "" {
				return p
			}
		}
	}
	return ""
}

// adminPassFile returns the path where the current admin password is persisted
// so the next launch can use it to rotate to a new one.
func adminPassFile() string {
	cp := ConfigPath()
	if cp == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cp), "usbridge_admin_pass")
}

// generatePassword creates a cryptographically-random 20-character hex password.
// crypto/rand.Read never fails on supported OS (macOS/Linux/Windows all provide
// a reliable entropy source), so no fallback to a known string is needed.
func generatePassword() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		// Should be unreachable: OS-level RNG failure is fatal.
		log.Panicf("[sunshine] crypto/rand unavailable: %v", err)
	}
	return hex.EncodeToString(b)
}

// BinaryPath returns the path to the sunshine binary, or "" if it can't be
// found (not installed/bundled, or unsupported OS).
func BinaryPath(exeDir string) string {
	switch runtime.GOOS {
	case "linux":
		// Bundled alongside agent (cmake install tree: sunshine/usr/bin/sunshine).
		// Built with SUNSHINE_BUILD_APPIMAGE=ON — assets are relative to cwd.
		local := filepath.Join(exeDir, "sunshine", "usr", "bin", "sunshine")
		if _, err := os.Stat(local); err == nil {
			return local
		}
		// Inside our AppImage: both binaries share usr/bin/ under $APPDIR.
		inBin := filepath.Join(exeDir, "sunshine")
		if info, err := os.Stat(inBin); err == nil && !info.IsDir() {
			return inBin
		}
		// Fall back to system PATH.
		if path, err := exec.LookPath("sunshine"); err == nil {
			return path
		}
		return ""
	case "windows":
		return filepath.Join(exeDir, "sunshine", "sunshine.exe")
	case "darwin":
		return filepath.Join(exeDir, "sunshine", "Sunshine.app", "Contents", "MacOS", "sunshine")
	default:
		return ""
	}
}

// LaunchPath returns the entry point to actually start Sunshine.
// Identical to BinaryPath on all platforms.
func LaunchPath(exeDir string) string {
	return BinaryPath(exeDir)
}

// Process manages the lifecycle of a bundled Sunshine instance launched by
// the agent.
type Process struct {
	mu              sync.Mutex
	launchPath      string
	logPath         string
	cmd             *exec.Cmd
	elevatedHandle  uintptr // HANDLE from ShellExecuteExW (Windows elevated launch); 0 if unused
}

// NewProcess creates a Process for the given launch entry point. logPath, if
// non-empty, captures Sunshine's stdout/stderr (its own structured logging
// still goes to sunshine.conf's log_path, independent of this).
func NewProcess(launchPath, logPath string) *Process {
	return &Process{launchPath: launchPath, logPath: logPath}
}

// Running reports whether this Process's Sunshine instance is currently alive
// (either via a normal exec.Cmd or an elevated ShellExecuteEx handle on Windows).
func (p *Process) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		return true
	}
	return elevatedRunning(p.elevatedHandle)
}

// Start launches Sunshine if it isn't already running (by this Process, or
// reachable on adminPort — e.g. a system-installed Sunshine service). No-op
// if the launch path doesn't exist.
func (p *Process) Start(adminPort int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		return nil
	}
	if p.launchPath == "" {
		return nil
	}
	if _, err := os.Stat(p.launchPath); err != nil {
		log.Printf("[sunshine] launch path not found, skipping auto-start: %s", p.launchPath)
		return nil
	}
	if adminPort > 0 && portReachable(adminPort, 300*time.Millisecond) {
		log.Printf("[sunshine] admin port %d already reachable, assuming Sunshine is already running", adminPort)
		return nil
	}

	// Pin web_bind_address to 127.0.0.1 so the HTTPS admin UI is only reachable
	// on localhost, independently of bind_address (which restricts streaming ports
	// to the VPN/LAN interface). Requires our itsme228/Sunshine fork.
	if err := setConfigKey("web_bind_address", "127.0.0.1"); err != nil {
		log.Printf("[sunshine] warning: could not set web_bind_address: %v", err)
	}

	// Set a fresh random admin password before starting Sunshine so the
	// process always starts with credentials we generated (not a stale or
	// default password). --creds writes directly to sunshine_state.json.
	newPass := generatePassword()
	credsCmd := exec.Command(p.launchPath, "--creds", AdminUser, newPass)
	if out, err := credsCmd.CombinedOutput(); err != nil {
		log.Printf("[sunshine] --creds failed: %v: %s", err, out)
	} else {
		activeAdminPassword = newPass
		if pf := adminPassFile(); pf != "" {
			_ = os.WriteFile(pf, []byte(newPass), 0600)
		}
		log.Printf("[sunshine] admin password set (user=%s)", AdminUser)
	}

	cmd := exec.Command(p.launchPath)
	configureProcess(cmd)
	if runtime.GOOS == "linux" {
		// Sunshine built with SUNSHINE_BUILD_APPIMAGE=ON uses ./usr/share/sunshine
		// relative to cwd. Set cwd to the root of the install tree (3 dirs up from
		// usr/bin/sunshine), which is either the AppImage $APPDIR or the staging dir.
		sunshineRoot := filepath.Dir(filepath.Dir(filepath.Dir(p.launchPath)))
		if sunshineRoot != "" && sunshineRoot != "." {
			cmd.Dir = sunshineRoot
		}
	}
	if p.logPath != "" {
		if err := os.MkdirAll(filepath.Dir(p.logPath), 0o755); err != nil {
			log.Printf("[sunshine] failed to create log dir for %s: %v", p.logPath, err)
		} else if f, err := os.OpenFile(p.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err != nil {
			log.Printf("[sunshine] failed to open log file %s: %v", p.logPath, err)
		} else {
			cmd.Stdout = f
			cmd.Stderr = f
		}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	p.cmd = cmd
	log.Printf("[sunshine] started pid=%d launch=%s", cmd.Process.Pid, p.launchPath)
	go func() {
		err := cmd.Wait()
		log.Printf("[sunshine] process exited: %v", err)
	}()

	return nil
}

// Stop terminates a Sunshine instance started by this Process. No-op if not
// running or if Sunshine wasn't launched by us (e.g. system service).
func (p *Process) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var err error
	if p.cmd != nil && p.cmd.Process != nil {
		log.Printf("[sunshine] stopping pid=%d", p.cmd.Process.Pid)
		err = p.cmd.Process.Kill()
		p.cmd = nil
	}
	if p.elevatedHandle != 0 {
		log.Printf("[sunshine] stopping elevated process")
		terminateElevated(p.elevatedHandle)
		p.elevatedHandle = 0
	}
	return err
}

// StartElevated launches Sunshine with a UAC elevation prompt (Windows only).
// On other platforms it is a no-op. Stops any currently running instance first.
func (p *Process) StartElevated(adminPort int) error {
	p.mu.Lock()
	// Stop existing instances
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		p.cmd = nil
	}
	if p.elevatedHandle != 0 {
		terminateElevated(p.elevatedHandle)
		p.elevatedHandle = 0
	}
	path := p.launchPath
	logPath := p.logPath
	p.mu.Unlock()

	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		log.Printf("[sunshine] launch path not found, cannot elevate: %s", path)
		return err
	}
	_ = logPath // elevated process stdout goes to its own window; we can't redirect it via ShellExecuteEx
	log.Printf("[sunshine] requesting elevated launch via UAC: %s", path)
	handle, err := launchElevated(path)
	if err != nil {
		return fmt.Errorf("elevated launch: %w", err)
	}
	p.mu.Lock()
	p.elevatedHandle = handle
	p.mu.Unlock()
	log.Printf("[sunshine] elevated process started")
	waitElevatedAsync(handle, func() {
		p.mu.Lock()
		if p.elevatedHandle == handle {
			p.elevatedHandle = 0
		}
		p.mu.Unlock()
	})
	return nil
}

// Client is a Moonlight client that has been paired with Sunshine.
type Client struct {
	Name     string `json:"name"`
	UniqueID string `json:"uuid"`
}

// ListClients returns the Moonlight clients currently paired with the Sunshine
// instance running on adminPort. Requires valid admin credentials to have been
// bootstrapped first.
func ListClients(adminPort int) ([]Client, error) {
	url := fmt.Sprintf("https://%s:%d/api/clients/list", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(AdminUser, adminPass())
	c := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		NamedCerts []Client `json:"named_certs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.NamedCerts, nil
}

// SubmitPIN sends a Moonlight pairing PIN to Sunshine's admin API on adminPort.
// The PIN is the 4-digit code shown by the Moonlight client during pairing.
func SubmitPIN(adminPort int, pin string) error {
	body, _ := json.Marshal(map[string]string{"pin": pin})
	url := fmt.Sprintf("https://%s:%d/api/pin", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(AdminUser, adminPass())
	c := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("Sunshine unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sunshine returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// UnpairClient removes the Moonlight client with the given uniqueID from
// Sunshine's authorized client list via the admin API on adminPort.
func UnpairClient(adminPort int, uniqueID string) error {
	body, _ := json.Marshal(map[string]string{"uuid": uniqueID})
	url := fmt.Sprintf("https://%s:%d/api/clients/unpair", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(AdminUser, adminPass())
	c := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unpair failed: %s", resp.Status)
	}
	return nil
}

func portReachable(port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(adminHost(), strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ConfigPath returns the default sunshine.conf location Sunshine uses when
// launched without an explicit config argument (matches Sunshine's own
// platf::appdata() resolution: $HOME/.config/sunshine/sunshine.conf on Linux).
func ConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(home, ".config", "sunshine", "sunshine.conf")
	case "darwin":
		return filepath.Join(home, ".config", "sunshine", "sunshine.conf")
	case "windows":
		return filepath.Join(home, "AppData", "Local", "Sunshine", "config", "sunshine.conf")
	default:
		return ""
	}
}

// SetConfigKey upserts a single "key = value" line in sunshine.conf.
// An empty value removes the key (falling back to Sunshine's own default).
func SetConfigKey(key, value string) error { return setConfigKey(key, value) }

// setConfigKey upserts a single "key = value" line in sunshine.conf. An
// empty value removes the key (falling back to Sunshine's own default/auto
// behavior). The file is created if missing; other keys/values are
// preserved verbatim.
func setConfigKey(key, value string) error {
	path := ConfigPath()
	if path == "" {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

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
	return os.WriteFile(path, []byte(content), 0o644)
}

// configKey reads the current value of a "key = value" line from
// sunshine.conf, or "" if unset.
func configKey(key string) string {
	path := ConfigPath()
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

// SetExternalIP sets the IP address Sunshine advertises to Moonlight clients
// via the external_ip key in sunshine.conf. Pass "" or "0.0.0.0" to remove
// the override (Sunshine auto-detects).
func SetExternalIP(ip string) error {
	if ip == "" || ip == "0.0.0.0" {
		return setConfigKey("external_ip", "")
	}
	return setConfigKey("external_ip", ip)
}

// ExternalIP reads the current external_ip value from sunshine.conf, or ""
// if unset (auto-detect / all interfaces).
func ExternalIP() string {
	return configKey("external_ip")
}

// SetBindAddress sets (or removes) the bind_address key in sunshine.conf,
// restricting ALL Sunshine servers (web admin + streaming) to the given IP.
// Pass "" or "0.0.0.0" to remove the restriction (bind on all interfaces).
func SetBindAddress(ip string) error {
	if ip == "" || ip == "0.0.0.0" {
		return setConfigKey("bind_address", "")
	}
	return setConfigKey("bind_address", ip)
}

// GetBindAddress reads the current bind_address from sunshine.conf, or ""
// if unset (Sunshine defaults to all interfaces).
func GetBindAddress() string {
	return configKey("bind_address")
}

// adminHost returns the host for Sunshine admin API calls.
// web_bind_address is always set to 127.0.0.1 before Sunshine starts,
// so the admin HTTPS server only listens on localhost.
func adminHost() string { return "127.0.0.1" }

// SetCaptureMode upserts the "capture" key in sunshine.conf. An empty mode
// removes the key (Sunshine auto-detects: portal on Wayland, x11 on X11).
func SetCaptureMode(mode string) error {
	return setConfigKey("capture", mode)
}

// CaptureMode reads the current "capture" value from sunshine.conf, or ""
// if unset (auto-detect).
func CaptureMode() string {
	return configKey("capture")
}

// SetAudioSink upserts the "audio_sink" key in sunshine.conf — which system
// audio output device Sunshine captures from for GameStream. An empty sink
// removes the key (Sunshine falls back to the system default sink).
func SetAudioSink(sink string) error {
	return setConfigKey("audio_sink", sink)
}

// AudioSink reads the current "audio_sink" value from sunshine.conf, or ""
// if unset (system default).
func AudioSink() string {
	return configKey("audio_sink")
}
