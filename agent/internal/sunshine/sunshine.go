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
	"io"
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

// windowsSunshineDir holds the directory containing sunshine.exe, set by
// NewProcess. The Windows portable build resolves its default config path
// (like assets/) relative to itself — ./config/sunshine.conf next to the
// exe — not the traditional %LOCALAPPDATA%\Sunshine\config used by an
// installed build, so ConfigPath must match that on Windows.
var windowsSunshineDir string

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

// CapExecPath returns the path to the bundled sunshine_capexec launcher
// (cmd/sunshine_capexec), or "" if not bundled (non-Linux, or a dev build
// without the AppImage layout). This is what actually carries the
// CAP_SYS_ADMIN file capability for KMS screen capture — never sunshine
// itself, since a file capability on sunshine would break its RPATH-based
// dependency resolution. See RequestKMSCapture in internal/permissions.
func CapExecPath(exeDir string) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	p := filepath.Join(exeDir, "sunshine-capexec")
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p
	}
	return ""
}

// RuntimeCapExecPath returns the path sunshine_capexec should actually be
// setcap'd and launched from, mirroring RuntimeBinaryPath: inside an
// AppImage the bundled copy lives on the read-only squashfs mount, so
// pkexec setcap needs the writable staged copy instead. Shares the same
// staging pass as RuntimeBinaryPath — both binaries are copied together by
// stageSunshineRuntime — so the two are consistent as long as both are
// called while stageSunshineRuntime's staleness check (keyed off the
// sunshine binary) still holds.
func RuntimeCapExecPath(exeDir, stateDir string) string {
	capexecSrc := CapExecPath(exeDir)
	sunshineSrc := BinaryPath(exeDir)
	if runtime.GOOS != "linux" || capexecSrc == "" || sunshineSrc == "" || stateDir == "" {
		return capexecSrc
	}
	if os.Getenv("APPIMAGE") == "" {
		return capexecSrc
	}
	if _, err := stageSunshineRuntime(sunshineSrc, stateDir); err != nil {
		log.Printf("[sunshine] failed to stage writable copy for KMS setcap: %v", err)
		return capexecSrc
	}
	return filepath.Join(stateDir, "sunshine-runtime", "usr", "bin", "sunshine-capexec")
}

// RuntimeBinaryPath returns the path Sunshine should actually be launched
// from, and the path `setcap` should target for KMS capture. On Linux, when
// running from inside an AppImage, the bundled binary lives on the
// squashfs/FUSE mount that the AppImage runtime sets up — which is read-only,
// so `pkexec setcap` on it always fails silently (the pkexec prompt succeeds,
// but the capability is never actually written). To fix that, the bundled
// Sunshine tree is copied once into stateDir, which is a normal writable
// directory, and that copy is used instead.
func RuntimeBinaryPath(exeDir, stateDir string) string {
	src := BinaryPath(exeDir)
	if runtime.GOOS != "linux" || src == "" || stateDir == "" {
		return src
	}
	if os.Getenv("APPIMAGE") == "" {
		// Not running from an AppImage mount — the bundled path is already
		// on a normal writable filesystem.
		return src
	}
	dst, err := stageSunshineRuntime(src, stateDir)
	if err != nil {
		log.Printf("[sunshine] failed to stage writable copy for KMS setcap: %v", err)
		return src
	}
	return dst
}

// stageSunshineRuntime copies the bundled Sunshine binary, its usr/local/assets
// (shaders/web UI/config templates), and its usr/lib (shared libraries bundled
// by linuxdeploy, which the binary's RPATH=$ORIGIN/../lib resolves against)
// from src's read-only AppImage mount into stateDir/sunshine-runtime, skipping
// the copy if a matching one is already there (compared by size + mtime).
// Omitting usr/lib here would leave the staged binary unable to resolve its
// bundled dependencies (e.g. libminiupnpc.so.17) once it's copied outside the
// AppImage mount, since RPATH is resolved relative to the binary's own path.
func stageSunshineRuntime(src, stateDir string) (string, error) {
	root := filepath.Join(stateDir, "sunshine-runtime")
	dstBin := filepath.Join(root, "usr", "bin", "sunshine")

	srcInfo, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if dstInfo, err := os.Stat(dstBin); err == nil &&
		dstInfo.Size() == srcInfo.Size() && dstInfo.ModTime().Equal(srcInfo.ModTime()) {
		return dstBin, nil
	}

	if err := os.RemoveAll(root); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dstBin), 0o755); err != nil {
		return "", err
	}
	if err := copyFile(src, dstBin, srcInfo.Mode()); err != nil {
		return "", err
	}
	if err := os.Chtimes(dstBin, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		return "", err
	}

	// usr/local/assets and usr/lib sit 3 dirs up from usr/bin/sunshine in the
	// source tree (see Start()'s cwd handling, which relies on the same layout).
	appDir := filepath.Dir(filepath.Dir(filepath.Dir(src)))
	srcAssets := filepath.Join(appDir, "usr", "local", "assets")
	if info, err := os.Stat(srcAssets); err == nil && info.IsDir() {
		if err := copyDir(srcAssets, filepath.Join(root, "usr", "local", "assets")); err != nil {
			return "", err
		}
	}
	srcLib := filepath.Join(appDir, "usr", "lib")
	if info, err := os.Stat(srcLib); err == nil && info.IsDir() {
		if err := copyDir(srcLib, filepath.Join(root, "usr", "lib")); err != nil {
			return "", err
		}
	}

	// sunshine_capexec (cmd/sunshine_capexec) sits alongside sunshine in
	// usr/bin — stage it too so RuntimeCapExecPath's writable copy exists
	// for pkexec setcap.
	srcCapExec := filepath.Join(appDir, "usr", "bin", "sunshine-capexec")
	if info, err := os.Stat(srcCapExec); err == nil && !info.IsDir() {
		if err := copyFile(srcCapExec, filepath.Join(root, "usr", "bin", "sunshine-capexec"), info.Mode()); err != nil {
			return "", err
		}
	}

	return dstBin, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

// Process manages the lifecycle of a bundled Sunshine instance launched by
// the agent.
type Process struct {
	mu          sync.Mutex
	launchPath  string
	capExecPath string
	logPath     string
	cmd         *exec.Cmd
}

// NewProcess creates a Process for the given launch entry point. logPath, if
// non-empty, captures Sunshine's stdout/stderr (its own structured logging
// still goes to sunshine.conf's log_path, independent of this).
func NewProcess(launchPath, logPath string) *Process {
	if runtime.GOOS == "windows" && launchPath != "" {
		windowsSunshineDir = filepath.Dir(launchPath)
	}
	return &Process{launchPath: launchPath, logPath: logPath}
}

// SetCapExecPath sets the sunshine_capexec launcher path (see
// RuntimeCapExecPath) that Start uses to launch Sunshine with CAP_SYS_ADMIN
// when the configured capture mode is "kms". A no-op path (empty, or the
// launcher lacking the capability) just means Start launches Sunshine
// directly, same as before KMS capture was requested/granted.
func (p *Process) SetCapExecPath(capExecPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.capExecPath = capExecPath
}

// Running reports whether this Process's Sunshine instance is currently alive.
func (p *Process) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd != nil && p.cmd.Process != nil
}

// CurrentVideoCodec returns the codec negotiated by the most recent stream,
// defaulting to "h264" if unable to determine.
func (p *Process) CurrentVideoCodec() string {
	p.mu.Lock()
	logFile := p.logPath
	p.mu.Unlock()

	if logFile == "" {
		return "h264"
	}
	f, err := os.Open(logFile)
	if err != nil {
		return "h264"
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "h264"
	}
	size := stat.Size()
	if size == 0 {
		return "h264"
	}
	readSize := int64(8192)
	if size < readSize {
		readSize = size
	}
	f.Seek(-readSize, 2)
	buf := make([]byte, readSize)
	f.Read(buf)

	lines := strings.Split(string(buf), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.ToLower(lines[i])
		if strings.Contains(line, "codec") || strings.Contains(line, "encoder") {
			if strings.Contains(line, "hevc") || strings.Contains(line, "h.265") || strings.Contains(line, "h265") {
				return "h265"
			}
			if strings.Contains(line, "av1") {
				return "av1"
			}
			if strings.Contains(line, "h.264") || strings.Contains(line, "h264") {
				return "h264"
			}
		}
	}
	return "h264"
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
		// This process never ran --creds this session, so activeAdminPassword
		// is still empty. The already-running Sunshine still has whatever
		// password was baked in via --creds the last time IT was launched
		// fresh, which is exactly what's persisted in adminPassFile — load it
		// so SubmitPIN/ListClients/UnpairClient (which read adminPass()
		// directly, not the file-fallback AdminPass()) don't send an empty
		// password and get every request rejected with 401.
		if pf := adminPassFile(); pf != "" {
			if data, err := os.ReadFile(pf); err == nil {
				if pass := strings.TrimSpace(string(data)); pass != "" {
					activeAdminPassword = pass
				}
			}
		}
		return nil
	}

	// Pin web_bind_address to 127.0.0.1 so the HTTPS admin UI is only reachable
	// on localhost, independently of bind_address (which restricts streaming ports
	// to the VPN/LAN interface). Requires our itsme228/Sunshine fork.
	if err := setConfigKey("web_bind_address", "127.0.0.1"); err != nil {
		log.Printf("[sunshine] warning: could not set web_bind_address: %v", err)
	}

	// On Windows the portable build expects sunshine_state.json to already
	// exist before --creds can write into it; create an empty-but-valid
	// template so the file is there when --creds runs.
	if runtime.GOOS == "windows" {
		if err := ensureSunshineStateFile(); err != nil {
			log.Printf("[sunshine] warning: could not pre-create sunshine_state.json: %v", err)
		}
	}

	// Set a fresh random admin password before starting Sunshine so the
	// process always starts with credentials we generated (not a stale or
	// default password). --creds writes directly to sunshine_state.json.
	newPass := generatePassword()
	credsCmd := exec.Command(p.launchPath, "--creds", AdminUser, newPass)
	configureProcess(credsCmd)
	// Sunshine resolves config paths (including sunshine_state.json) relative
	// to its process CWD on Windows, not its exe path. Without setting Dir,
	// --creds runs from the agent's CWD and writes to the wrong location while
	// the main Sunshine process (which has Dir set below) reads from a different
	// path — the credentials never match.
	if sunshineDir := filepath.Dir(p.launchPath); sunshineDir != "" && sunshineDir != "." {
		credsCmd.Dir = sunshineDir
	}
	if out, err := credsCmd.CombinedOutput(); err != nil {
		log.Printf("[sunshine] --creds failed: %v: %s", err, out)
	} else {
		activeAdminPassword = newPass
		if pf := adminPassFile(); pf != "" {
			_ = os.WriteFile(pf, []byte(newPass), 0600)
		}
		log.Printf("[sunshine] admin password set (user=%s)", AdminUser)
	}

	// If a capability-granted sunshine_capexec launcher is set (Linux KMS
	// capture only — see SetCapExecPath), launch Sunshine through it so it
	// inherits CAP_SYS_ADMIN via ambient capabilities instead of carrying a
	// file capability itself, which would break its RPATH-based library
	// resolution. p.capExecPath is only ever set once the capability has
	// actually been granted (internal/app), so this exec is expected to
	// succeed whenever it's used.
	var cmd *exec.Cmd
	if p.capExecPath != "" {
		cmd = exec.Command(p.capExecPath, p.launchPath)
	} else {
		cmd = exec.Command(p.launchPath)
	}
	configureProcess(cmd)
	switch runtime.GOOS {
	case "linux":
		// Sunshine built with SUNSHINE_BUILD_APPIMAGE=ON uses ./usr/local/assets
		// relative to cwd. Set cwd to the root of the install tree (3 dirs up from
		// usr/bin/sunshine), which is either the AppImage $APPDIR or the staging dir.
		sunshineRoot := filepath.Dir(filepath.Dir(filepath.Dir(p.launchPath)))
		if sunshineRoot != "" && sunshineRoot != "." {
			cmd.Dir = sunshineRoot
		}
	default:
		// Windows (and macOS) Sunshine resolves assets/ relative to its process
		// cwd, not its own exe path. Without this, launching from the agent
		// (whose own cwd may differ) breaks shader/asset lookup with
		// ERROR_PATH_NOT_FOUND while double-clicking sunshine.exe directly
		// works by accident (Explorer sets cwd to the exe's own folder).
		if dir := filepath.Dir(p.launchPath); dir != "" && dir != "." {
			cmd.Dir = dir
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
	return err
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
	if runtime.GOOS == "windows" {
		if windowsSunshineDir == "" {
			return ""
		}
		return filepath.Join(windowsSunshineDir, "config", "sunshine.conf")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(home, ".config", "sunshine", "sunshine.conf")
	case "darwin":
		return filepath.Join(home, ".config", "sunshine", "sunshine.conf")
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

// ensureSunshineStateFile creates sunshine_state.json with a valid template
// if it does not already exist. On Windows the portable Sunshine build will
// not create this file itself — --creds only updates it, so the file must
// pre-exist or credential bootstrap silently fails.
//
// uniqueid must be a non-empty UUID: Sunshine treats an empty uniqueid as
// "first run" and regenerates the entire file on startup, wiping any
// credentials that --creds wrote before the main process started.
func ensureSunshineStateFile() error {
	if windowsSunshineDir == "" {
		return nil
	}
	stateFile := filepath.Join(windowsSunshineDir, "config", "sunshine_state.json")
	if _, err := os.Stat(stateFile); err == nil {
		return nil // already exists
	}
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o755); err != nil {
		return err
	}
	uid, err := randomUUID()
	if err != nil {
		return err
	}
	content := `{
    "username": "sunshine",
    "salt": "",
    "password": "",
    "root": {
        "uniqueid": "` + uid + `",
        "named_devices": []
    }
}
`
	return os.WriteFile(stateFile, []byte(content), 0o644)
}

// randomUUID returns a random UUID v4 string (xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx).
func randomUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
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
