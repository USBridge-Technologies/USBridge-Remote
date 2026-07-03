// Package sunshine locates the Sunshine (Moonlight GameStream host) binary
// and manages the small subset of its own sunshine.conf that the agent needs
// to control — currently just the Linux capture backend (portal vs.
// KMS/root). On Windows/macOS, Sunshine is bundled next to the agent
// executable. On Linux it's built from source and installed system-wide by
// the build script (see scripts/fetch_sunshine.sh) — its web-UI assets are
// compiled in with an absolute /usr path, so it can't be kept self-contained
// like the other platforms, and a non-AppImage build is required for KMS
// capture to work at all.
package sunshine

import (
	"bufio"
	"bytes"
	"crypto/tls"
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

// AdminUser and AdminPassword are the Sunshine web-UI credentials the agent
// bootstraps on first launch and uses to relay Moonlight pairing PINs
// (matches the canonical usbridge hardware image's provisioning).
const (
	AdminUser     = "sunshine"
	AdminPassword = "sunshine"
)

// BinaryPath returns the path to the sunshine binary, or "" if it can't be
// found (not installed/bundled, or unsupported OS). This is the raw ELF/exe
// — used for capability checks (e.g. setcap) as well as launching; unlike
// Windows/macOS, Linux has no separate AppImage wrapper to distinguish here
// since Sunshine is a normal system install there.
func BinaryPath(exeDir string) string {
	switch runtime.GOOS {
	case "linux":
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

// LaunchPath returns the entry point to actually start Sunshine. Identical
// to BinaryPath on every platform now that Linux installs Sunshine
// system-wide rather than bundling an AppImage.
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

	cmd := exec.Command(p.launchPath)
	configureProcess(cmd)
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
	if adminPort > 0 {
		go bootstrapAdminCredentials(adminPort)
	}
	return nil
}

// bootstrapAdminCredentials waits for a freshly launched Sunshine's admin API
// to come up and creates the sunshine/sunshine web-UI account the agent uses
// to relay Moonlight pairing PINs. A no-op (Sunshine rejects it) if an admin
// account already exists — safe to call on every launch.
func bootstrapAdminCredentials(adminPort int) {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if portReachable(adminPort, 500*time.Millisecond) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	body, _ := json.Marshal(map[string]string{
		"currentUsername":    "",
		"currentPassword":    "",
		"newUsername":        AdminUser,
		"newPassword":        AdminPassword,
		"confirmNewPassword": AdminPassword,
	})
	url := "https://127.0.0.1:" + strconv.Itoa(adminPort) + "/api/password"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Sunshine uses a self-signed cert on localhost
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[sunshine] admin credential bootstrap failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		log.Printf("[sunshine] admin credentials provisioned (user=%s)", AdminUser)
	}
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
	if adminPort > 0 {
		go bootstrapAdminCredentials(adminPort)
	}
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
	url := fmt.Sprintf("https://127.0.0.1:%d/api/clients/list", adminPort)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(AdminUser, AdminPassword)
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
	url := fmt.Sprintf("https://127.0.0.1:%d/api/pin", adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(AdminUser, AdminPassword)
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
	url := fmt.Sprintf("https://127.0.0.1:%d/api/clients/unpair", adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(AdminUser, AdminPassword)
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
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), timeout)
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
