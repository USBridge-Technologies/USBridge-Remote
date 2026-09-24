//go:build windows

package vdisplay

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestHasMttVDD(t *testing.T) {
	cases := []struct {
		ids  []string
		want bool
	}{
		{nil, false},
		{[]string{`PCI\VEN_10DE&DEV_1E07&SUBSYS_866B1043&REV_A1`}, false},
		{[]string{`PCI\VEN_10DE&DEV_1E07`, `Root\MttVDD`}, true},
		{[]string{`ROOT\MTTVDD`}, true}, // PnP ids compare case-insensitively
		{[]string{`root\sudomaker\sudovda`, `Root\Parsec\VDA`, `Root\VirtualDisplayDriver`}, false},
		{[]string{`Root\MttVDD2`}, false},
	}
	for _, c := range cases {
		if got := hasMttVDD(c.ids); got != c.want {
			t.Errorf("hasMttVDD(%q) = %v, want %v", c.ids, got, c.want)
		}
	}
}

func TestPsSingleQuoted(t *testing.T) {
	cases := map[string]string{
		``:                  `''`,
		`C:\a b\c.ps1`:      `'C:\a b\c.ps1'`,
		`C:\Users\O'Brien`:  `'C:\Users\O''Brien'`,
		`$env:TEMP; exit 1`: `'$env:TEMP; exit 1'`, // no expansion inside single quotes
	}
	for in, want := range cases {
		if got := psSingleQuoted(in); got != want {
			t.Errorf("psSingleQuoted(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInstallCommandArgs_ElevatesOnlyWhenAsked(t *testing.T) {
	elevated := strings.Join(installCommandArgs(`C:\t\s.ps1`, `C:\t\l.log`, true), " ")
	if !strings.Contains(elevated, "-Verb RunAs") {
		t.Fatalf("elevated command lacks -Verb RunAs: %s", elevated)
	}
	plain := strings.Join(installCommandArgs(`C:\t\s.ps1`, `C:\t\l.log`, false), " ")
	if strings.Contains(plain, "RunAs") {
		t.Fatalf("non-elevated command asks for RunAs: %s", plain)
	}
}

// TestInstallCommandArgs_RoundTripsThroughRealPowerShell runs the exact
// two-level command (minus the UAC verb) against a stand-in script in a
// directory whose name has a space and an apostrophe -- a real %TEMP% under
// a user profile like "O'Brien Smith" -- and checks the inner script got
// both paths intact and its exit code comes back out.
func TestInstallCommandArgs_RoundTripsThroughRealPowerShell(t *testing.T) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe not available")
	}
	dir := filepath.Join(t.TempDir(), "it's a dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "stand in.ps1")
	logPath := filepath.Join(dir, "install.log")
	body := "param([string]$LogPath)\r\nSet-Content -LiteralPath $LogPath -Value (\"got:\" + $LogPath)\r\nexit 7\r\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("powershell.exe", installCommandArgs(script, logPath, false)...)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("want the inner script's exit code 7 propagated, got err=%v output=%s", err, out)
	}
	got, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("inner script never wrote its log (paths mangled?): %v; output=%s", readErr, out)
	}
	if strings.TrimSpace(string(got)) != "got:"+logPath {
		t.Fatalf("inner script saw LogPath %q, want %q", strings.TrimSpace(string(got)), logPath)
	}
}

// TestEmbeddedInstallScript_RefusesUnelevatedAndLogs runs the real embedded
// install script the way InstallDriver does, minus elevation: it must parse,
// refuse to do anything without admin rights, exit non-zero, and leave that
// reason in -LogPath -- which is how InstallDriver surfaces a failure the
// user couldn't otherwise see.
func TestEmbeddedInstallScript_RefusesUnelevatedAndLogs(t *testing.T) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe not available")
	}
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("running elevated: the script would really install/restart the driver")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "install-mttvdd.ps1")
	logPath := filepath.Join(dir, "install.log")
	if err := os.WriteFile(script, append([]byte{0xEF, 0xBB, 0xBF}, installScript...), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell.exe", installCommandArgs(script, logPath, false)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Skipf("script succeeded -- this test process is elevated; output=%s", out)
	}
	logData, _ := os.ReadFile(logPath)
	got := interpretInstallResult(err, string(out), lastLines(string(logData), 6))
	if got == nil || !strings.Contains(got.Error(), "elevated") {
		t.Fatalf("want the script's own 'elevated' refusal surfaced, got %v (log=%q)", got, logData)
	}
}

func TestInterpretInstallResult(t *testing.T) {
	boom := errors.New("exit status 1")
	if err := interpretInstallResult(nil, "anything", "tail"); err != nil {
		t.Fatalf("success reported as %v", err)
	}
	if err := interpretInstallResult(boom, "Start-Process : This command cannot be run due to the error: The operation was canceled by the user.", ""); !errors.Is(err, errInstallCancelled) {
		t.Fatalf("declined UAC reported as %v", err)
	}
	err := interpretInstallResult(boom, "noise", "mttvdd.cat signature is not valid: NotSigned")
	if err == nil || !strings.Contains(err.Error(), "signature is not valid") {
		t.Fatalf("script failure should carry the transcript tail, got %v", err)
	}
	err = interpretInstallResult(boom, "only output", "")
	if err == nil || !strings.Contains(err.Error(), "only output") {
		t.Fatalf("with no transcript, fall back to output; got %v", err)
	}
}

func TestLastLines(t *testing.T) {
	in := "a\r\n\r\nb\nc\n  \nd\n"
	if got := lastLines(in, 2); got != "c\nd" {
		t.Fatalf("lastLines = %q", got)
	}
	if got := lastLines(in, 10); got != "a\nb\nc\nd" {
		t.Fatalf("lastLines = %q", got)
	}
}

// TestDriverInstalled_Live checks detection against the real machine:
// USBRIDGE_EXPECT_MTTVDD=1 (installed and running) or =0 (absent).
func TestDriverInstalled_Live(t *testing.T) {
	want := os.Getenv("USBRIDGE_EXPECT_MTTVDD")
	if want == "" {
		t.Skip("set USBRIDGE_EXPECT_MTTVDD=1 or 0 to check detection on this machine")
	}
	if got := DriverInstalled(); got != (want == "1") {
		t.Fatalf("DriverInstalled() = %v, want %v (display ids: %q)", got, want == "1", displayDeviceIDs())
	}
}
