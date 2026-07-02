// Package sunshine locates the Sunshine (Moonlight GameStream host) binary
// bundled next to the agent and manages the small subset of its own
// sunshine.conf that the agent needs to control — currently just the Linux
// capture backend (portal vs. KMS/root).
package sunshine

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
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

// BinaryPath returns the path to the bundled sunshine binary next to the
// agent executable, or "" if it isn't present (not bundled, or unsupported OS).
// This is the raw ELF/exe — used for capability checks (e.g. setcap), not for
// launching (see LaunchPath).
func BinaryPath(exeDir string) string {
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(exeDir, "sunshine", "usr", "bin", "sunshine")
	case "windows":
		return filepath.Join(exeDir, "sunshine", "sunshine.exe")
	case "darwin":
		return filepath.Join(exeDir, "sunshine", "Sunshine.app", "Contents", "MacOS", "sunshine")
	default:
		return ""
	}
}

// LaunchPath returns the entry point to actually start Sunshine. On Linux
// this is the AppImage's AppRun wrapper (sets up library paths/GTK env)
// rather than the raw binary from BinaryPath.
func LaunchPath(exeDir string) string {
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(exeDir, "sunshine", "AppRun")
	default:
		return BinaryPath(exeDir)
	}
}

// Process manages the lifecycle of a bundled Sunshine instance launched by
// the agent.
type Process struct {
	mu         sync.Mutex
	launchPath string
	logPath    string
	cmd        *exec.Cmd
}

// NewProcess creates a Process for the given launch entry point. logPath, if
// non-empty, captures Sunshine's stdout/stderr (its own structured logging
// still goes to sunshine.conf's log_path, independent of this).
func NewProcess(launchPath, logPath string) *Process {
	return &Process{launchPath: launchPath, logPath: logPath}
}

// Running reports whether this Process's Sunshine instance is currently alive.
func (p *Process) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd != nil && p.cmd.Process != nil
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
	if p.logPath != "" {
		if f, err := os.OpenFile(p.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
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
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	log.Printf("[sunshine] stopping pid=%d", p.cmd.Process.Pid)
	err := p.cmd.Process.Kill()
	p.cmd = nil
	return err
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

// SetCaptureMode upserts the "capture" key in sunshine.conf. An empty mode
// removes the key (Sunshine auto-detects: portal on Wayland, x11 on X11).
// The file is created if missing; other keys/values are preserved verbatim.
func SetCaptureMode(mode string) error {
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
			if strings.HasPrefix(strings.TrimSpace(line), "capture") && strings.Contains(line, "=") {
				continue // drop existing capture line, re-added below if needed
			}
			lines = append(lines, line)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	mode = strings.TrimSpace(mode)
	if mode != "" {
		lines = append(lines, "capture = "+mode)
	}

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// CaptureMode reads the current "capture" value from sunshine.conf, or ""
// if unset (auto-detect).
func CaptureMode() string {
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
		if !strings.HasPrefix(line, "capture") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "capture" {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}
