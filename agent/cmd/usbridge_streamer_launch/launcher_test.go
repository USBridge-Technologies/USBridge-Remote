//go:build linux

package main

// Security tests for the launcher binary itself. The first group builds a
// fresh, non-capability copy and always runs. The "Installed" group runs
// against /usr/local/libexec/usbridge's real, setcap'd launcher whenever
// one is installed (skipped otherwise, never prompts). The "Live" group
// also needs a real release bundle: set USBRIDGE_LIVE_BUNDLE to a dir
// with usbridge-streamer.tar.gz + manifest.json(.sig), e.g. from
//
//	gh release download --repo itsme228/rust-shine -p manifest.json \
//	  -p manifest.json.sig -p usbridge-streamer-linux-x86_64.tar.gz
//	mv usbridge-streamer-linux-x86_64.tar.gz usbridge-streamer.tar.gz
//
// and USBRIDGE_LIVE_ENTITLEMENT/USBRIDGE_LIVE_HWID to the agent's
// entitlement token file and hardware id (see the running agent's
// usbridge-usb-broker command line).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"usbridge_agent/internal/streamerlaunch"
)

var builtLauncher string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "launcher-test-")
	if err != nil {
		panic(err)
	}
	builtLauncher = filepath.Join(dir, "usbridge-streamer-launch")
	cmd := exec.Command("go", "build", "-o", builtLauncher, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("build launcher: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err == nil {
		return 0
	}
	return -1
}

// sentinelArchive is a .tar.gz whose usbridge-streamer is a shell script
// that creates marker -- if it ever runs, the launcher executed something
// it must not have.
func sentinelArchive(t *testing.T, marker string) []byte {
	t.Helper()
	body := []byte("#!/bin/sh\ntouch " + marker + "\n")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "usbridge-streamer", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// signedBundle writes a bundle signed by priv (a throwaway key, i.e. NOT
// the release key the launcher trusts).
func signedBundle(t *testing.T, priv ed25519.PrivateKey, archive []byte) string {
	t.Helper()
	dir := t.TempDir()
	sum := sha256.Sum256(archive)
	m, _ := json.Marshal(map[string]any{
		"app": "usbridge-streamer", "version": "usbridge-streamer-v9.9.9",
		"platforms": map[string]any{"linux-x86_64": map[string]string{"asset": "x", "sha256": hex.EncodeToString(sum[:])}},
	})
	os.WriteFile(filepath.Join(dir, streamerlaunch.ManifestName), m, 0o644)
	os.WriteFile(filepath.Join(dir, streamerlaunch.SigName), []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, m))), 0o644)
	os.WriteFile(filepath.Join(dir, streamerlaunch.ArchiveName), archive, 0o644)
	return dir
}

// realManifestBundle pairs the real CI-signed manifest with an archive it
// doesn't cover -- a validly signed manifest must not vouch for other bytes.
func realManifestBundle(t *testing.T, archive []byte) string {
	t.Helper()
	dir := t.TempDir()
	for src, dst := range map[string]string{
		"release-v0.3.86-manifest.json":     streamerlaunch.ManifestName,
		"release-v0.3.86-manifest.json.sig": streamerlaunch.SigName,
	} {
		b, err := os.ReadFile(filepath.Join("..", "..", "internal", "streamerlaunch", "testdata", src))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, dst), b, 0o644)
	}
	os.WriteFile(filepath.Join(dir, streamerlaunch.ArchiveName), archive, 0o644)
	return dir
}

func rejectedBundles(t *testing.T, marker string) map[string]string {
	_, foreign, _ := ed25519.GenerateKey(nil)
	return map[string]string{
		"foreign key":                  signedBundle(t, foreign, sentinelArchive(t, marker)),
		"real manifest, other archive": realManifestBundle(t, sentinelArchive(t, marker)),
		"empty dir":                    t.TempDir(),
	}
}

func assertRefused(t *testing.T, launcher string, marker string) {
	t.Helper()
	for name, bundle := range rejectedBundles(t, marker) {
		out, err := exec.Command(launcher, "--verify", bundle).CombinedOutput()
		if exitCode(err) != exitVerify {
			t.Errorf("%s: --verify exit=%d want %d (%s)", name, exitCode(err), exitVerify, out)
		}
		out, err = exec.Command(launcher, "--run", bundle, "--").CombinedOutput()
		if exitCode(err) == 0 {
			t.Errorf("%s: --run succeeded (%s)", name, out)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("launcher executed an unverified binary")
	}
}

func TestLauncher_RefusesUnsignedAndTamperedBundles(t *testing.T) {
	assertRefused(t, builtLauncher, filepath.Join(t.TempDir(), "ran"))
}

// A copy outside the root-owned install (no file capability, or a uid not
// in the allowlist) must refuse to run anything, even a bundle that would
// verify -- it never execs without CAP_SYS_ADMIN in hand.
func TestLauncher_UserCopyNeverRuns(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	_, priv, _ := ed25519.GenerateKey(nil)
	bundle := signedBundle(t, priv, sentinelArchive(t, marker))
	for _, args := range [][]string{{"--run", bundle, "--"}, {"--run-sunshine", "--"}} {
		out, err := exec.Command(builtLauncher, args...).CombinedOutput()
		if c := exitCode(err); c != exitDenied && c != exitNoCap {
			t.Errorf("%v: exit=%d want %d or %d (%s)", args[0], c, exitDenied, exitNoCap, out)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("user copy executed a binary")
	}
}

func TestLauncher_Usage(t *testing.T) {
	for _, args := range [][]string{{}, {"--bogus"}, {"--verify"}, {"--run"}, {"/bin/sh", "-c", "id"}} {
		if c := exitCode(exec.Command(builtLauncher, args...).Run()); c != exitUsage {
			t.Errorf("%v: exit=%d want %d", args, c, exitUsage)
		}
	}
}

func installedLauncher(t *testing.T) string {
	t.Helper()
	if err := streamerlaunch.CheckRootOwned(streamerlaunch.InstallPath); err != nil {
		t.Skipf("no installed launcher: %v", err)
	}
	return streamerlaunch.InstallPath
}

// The setcap'd, root-owned launcher: same refusals, capability or not.
func TestInstalled_RefusesUnsignedAndTamperedBundles(t *testing.T) {
	assertRefused(t, installedLauncher(t), filepath.Join(t.TempDir(), "ran"))
}

func TestInstalled_TrustsOnlyRootOwnedFiles(t *testing.T) {
	installedLauncher(t)
	for _, p := range []string{streamerlaunch.InstallDir, streamerlaunch.InstallPath, streamerlaunch.AllowedUIDsPath} {
		if err := streamerlaunch.CheckRootOwned(p); err != nil {
			t.Errorf("%v", err)
		}
	}
	if _, err := os.Stat(streamerlaunch.SunshineDir); err == nil {
		if err := streamerlaunch.CheckTreeRootOwned(streamerlaunch.SunshineDir); err != nil {
			t.Errorf("installed Sunshine tree: %v", err)
		}
	}
}

func liveArgs(t *testing.T) (bundle string, args []string) {
	t.Helper()
	installedLauncher(t)
	bundle = os.Getenv("USBRIDGE_LIVE_BUNDLE")
	ent, hwid := os.Getenv("USBRIDGE_LIVE_ENTITLEMENT"), os.Getenv("USBRIDGE_LIVE_HWID")
	if bundle == "" || ent == "" || hwid == "" {
		t.Skip("set USBRIDGE_LIVE_BUNDLE, USBRIDGE_LIVE_ENTITLEMENT, USBRIDGE_LIVE_HWID")
	}
	dir := t.TempDir()
	return bundle, []string{"--http-port", "58989", "--credentials-path", filepath.Join(dir, "creds.json"),
		"--webrtc-disable", "--entitlement-file", ent, "--hardware-id", hwid}
}

func procStatus(t *testing.T, pid int, key string) string {
	t.Helper()
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, key+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// The real signed streamer, run through the installed launcher, ends up
// with exactly CAP_SYS_ADMIN (ambient) and nothing else, and ignores a
// hostile LD_PRELOAD.
func TestLive_StreamerGetsOnlyCapSysAdmin(t *testing.T) {
	bundle, args := liveArgs(t)
	cmd := exec.Command(streamerlaunch.InstallPath, append([]string{"--run", bundle, "--"}, args...)...)
	cmd.Env = append(os.Environ(), "LD_PRELOAD=/nonexistent/evil.so", "VK_LAYER_PATH=/tmp", "PATH=/tmp:/usr/bin")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	time.Sleep(2 * time.Second)
	const capSysAdminMask = "0000000000200000"
	for _, k := range []string{"CapEff", "CapPrm", "CapAmb"} {
		if got := procStatus(t, cmd.Process.Pid, k); got != capSysAdminMask {
			t.Errorf("%s = %s, want %s (CAP_SYS_ADMIN only)", k, got, capSysAdminMask)
		}
	}
	if procStatus(t, cmd.Process.Pid, "NoNewPrivs") != "0" {
		t.Error("unexpected NoNewPrivs")
	}
}

// The streamer must die with the agent even though exec'ing the
// file-capability launcher clears PDEATHSIG (confirmed live before the
// launcher pinned its OS thread: the streamer outlived a killed parent).
func TestLive_StreamerDiesWithParent(t *testing.T) {
	bundle, args := liveArgs(t)
	for i := 0; i < 3; i++ {
		// Middle process = "agent": starts the launcher with PDEATHSIG,
		// prints its pid, then exits.
		script := `import subprocess,sys,ctypes,signal,time
libc=ctypes.CDLL(None)
p=subprocess.Popen(sys.argv[1:],preexec_fn=lambda: libc.prctl(1,signal.SIGKILL),stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
print(p.pid,flush=True)
time.sleep(2)
`
		out, err := exec.Command("python3", append([]string{"-c", script, streamerlaunch.InstallPath, "--run", bundle, "--"}, args...)...).Output()
		if err != nil {
			t.Skipf("python3 unavailable: %v", err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			t.Fatalf("bad pid %q", out)
		}
		time.Sleep(700 * time.Millisecond)
		if err := syscall.Kill(pid, 0); err == nil {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("run %d: streamer pid %d outlived its parent", i, pid)
		}
	}
}
