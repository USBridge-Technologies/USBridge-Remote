package streamhost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePunktfunk stands in for punktfunk-host's management API, validating
// the exact wire shapes confirmed from api/openapi.json: bearer-token auth
// (not Basic), GET /clients, GET /pair + POST /pair/pin with the full
// ceremony identity, DELETE /clients/{fingerprint}, GET /status and
// GET /health. mu guards every field ServeHTTP reads: TestPunktfunkSubmitPINWaitsForTheCeremonyToAppear
// mutates `pending` from a second goroutine while SubmitPIN's poll loop is
// concurrently hitting GET /pair.
type fakePunktfunk struct {
	token string // "" means don't check auth

	mu      sync.Mutex
	clients []punktfunkPairedClient
	pending []punktfunkPendingCeremony
	posted  *struct {
		Pin         string `json:"pin"`
		UniqueID    string `json:"uniqueid"`
		Fingerprint string `json:"fingerprint"`
		PeerIP      string `json:"peer_ip"`
	}
	unpaired   string
	activeSess int
}

func (f *fakePunktfunk) setPending(p []punktfunkPendingCeremony) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = p
}

func (f *fakePunktfunk) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/health" {
		if f.token != "" {
			got := r.Header.Get("Authorization")
			if got != "Bearer "+f.token {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing or invalid bearer token"})
				return
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/api/v1/health" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "test", "abi_version": 1})
	case r.URL.Path == "/api/v1/clients" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(f.clients)
	case r.URL.Path == "/api/v1/pair" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(punktfunkPairingStatus{PinPending: len(f.pending) > 0, Pending: f.pending})
	case r.URL.Path == "/api/v1/pair/pin" && r.Method == http.MethodPost:
		var body struct {
			Pin         string `json:"pin"`
			UniqueID    string `json:"uniqueid"`
			Fingerprint string `json:"fingerprint"`
			PeerIP      string `json:"peer_ip"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.posted = &body
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/api/v1/status" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(punktfunkRuntimeStatus{ActiveSessions: f.activeSess})
	case len(r.URL.Path) > len("/api/v1/clients/") && r.URL.Path[:len("/api/v1/clients/")] == "/api/v1/clients/" && r.Method == http.MethodDelete:
		f.unpaired = r.URL.Path[len("/api/v1/clients/"):]
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func startFakePunktfunk(t *testing.T, f *fakePunktfunk) int {
	t.Helper()
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestPunktfunkListClientsMapsFingerprintAsUniqueID(t *testing.T) {
	label := "Living Room TV"
	f := &fakePunktfunk{
		token: "tok",
		clients: []punktfunkPairedClient{
			{Fingerprint: "abc123", Label: &label},
			{Fingerprint: "def456"},
		},
	}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok"}

	clients, err := b.ListClients(port)
	if err != nil {
		t.Fatalf("ListClients: %v", err)
	}
	if len(clients) != 2 {
		t.Fatalf("got %d clients, want 2", len(clients))
	}
	if clients[0].UniqueID != "abc123" || clients[0].Name != "Living Room TV" {
		t.Errorf("clients[0] = %+v, want fingerprint abc123 named by label", clients[0])
	}
	if clients[1].UniqueID != "def456" || clients[1].Name != "def456" {
		t.Errorf("clients[1] = %+v, want unlabeled client named by its own fingerprint", clients[1])
	}
}

func TestPunktfunkSubmitPINEchoesTheCeremonyIdentity(t *testing.T) {
	f := &fakePunktfunk{
		token: "tok",
		pending: []punktfunkPendingCeremony{
			{UniqueID: "0123456789ABCDEF", Fingerprint: "9f86d0", PeerIP: "192.168.1.42"},
		},
	}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok"}

	if err := b.SubmitPIN(port, "1234"); err != nil {
		t.Fatalf("SubmitPIN: %v", err)
	}
	if f.posted == nil {
		t.Fatal("POST /pair/pin was never called")
	}
	if f.posted.Pin != "1234" || f.posted.UniqueID != "0123456789ABCDEF" ||
		f.posted.Fingerprint != "9f86d0" || f.posted.PeerIP != "192.168.1.42" {
		t.Errorf("posted %+v, want the PIN plus the full pending ceremony identity", f.posted)
	}
}

func TestPunktfunkSubmitPINWaitsForTheCeremonyToAppear(t *testing.T) {
	old := punktfunkPendingWait
	punktfunkPendingWait = 2 * time.Second
	t.Cleanup(func() { punktfunkPendingWait = old })

	f := &fakePunktfunk{token: "tok"}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok"}

	// Flip the ceremony into existence shortly after SubmitPIN starts polling.
	go func() {
		time.Sleep(50 * time.Millisecond)
		f.setPending([]punktfunkPendingCeremony{{UniqueID: "u", Fingerprint: "fp", PeerIP: "1.2.3.4"}})
	}()

	if err := b.SubmitPIN(port, "0042"); err != nil {
		t.Fatalf("SubmitPIN: %v", err)
	}
	if f.posted == nil || f.posted.UniqueID != "u" {
		t.Fatalf("posted = %+v, want the ceremony that appeared mid-poll", f.posted)
	}
}

func TestPunktfunkSubmitPINFailsWhenNoCeremonyShowsUp(t *testing.T) {
	old := punktfunkPendingWait
	punktfunkPendingWait = 300 * time.Millisecond
	t.Cleanup(func() { punktfunkPendingWait = old })

	f := &fakePunktfunk{token: "tok"}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok"}

	err := b.SubmitPIN(port, "1234")
	if err == nil {
		t.Fatal("SubmitPIN: want an error when no ceremony ever appears")
	}
}

func TestPunktfunkUnpairClientUsesFingerprintAsUniqueID(t *testing.T) {
	f := &fakePunktfunk{token: "tok"}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok"}

	if err := b.UnpairClient(port, "abc123"); err != nil {
		t.Fatalf("UnpairClient: %v", err)
	}
	if f.unpaired != "abc123" {
		t.Errorf("unpaired = %q, want abc123", f.unpaired)
	}
}

func TestPunktfunkSessionActiveReadsRuntimeStatus(t *testing.T) {
	f := &fakePunktfunk{token: "tok", activeSess: 1}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "tok", adminPort: port}

	if !b.SessionActive() {
		t.Error("SessionActive() = false with active_sessions:1, want true")
	}

	f.activeSess = 0
	if b.SessionActive() {
		t.Error("SessionActive() = true with active_sessions:0, want false")
	}
}

func TestPunktfunkWaitReadyPollsHealth(t *testing.T) {
	f := &fakePunktfunk{token: "tok"}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{}

	if !b.WaitReady(port, 2*time.Second) {
		t.Error("WaitReady() = false against a live /health endpoint, want true")
	}
}

func TestPunktfunkRejectsAWrongBearerToken(t *testing.T) {
	f := &fakePunktfunk{token: "correct-token"}
	port := startFakePunktfunk(t, f)
	b := &punktfunkBackend{token: "wrong-token"}

	if _, err := b.ListClients(port); err == nil {
		t.Error("ListClients with a wrong bearer token: want an error, got nil")
	}
}

func TestParsePunktfunkMonitors(t *testing.T) {
	out := "Kwin:\n" +
		"  HDMI-A-1      3840x2160@60 at +0,+0  scale 2.69921875  Woodwind Communications Systems Inc SU13TO\n" +
		"  DP-2          2560x1440@144 at +3840,+0  scale 1  Dell Inc. U2720Q  [primary, PINNED]\n" +
		"  DP-3          1920x1080@60 at +0,+0  scale 1  Old Panel  [disabled]\n" +
		"  Virtual-punktfunk-1  1920x1080@60 at +6400,+0  scale 1  punktfunk  [punktfunk virtual display]\n"
	got := parsePunktfunkMonitors(out)
	if len(got) != 2 {
		t.Fatalf("got %d monitors, want the 2 enabled physical ones: %+v", len(got), got)
	}
	if d := got[0]; d.OutputName != "HDMI-A-1" || d.Width != 3840 || d.Height != 2160 || d.Primary ||
		d.DisplayName != "Woodwind Communications Systems Inc SU13TO" {
		t.Errorf("first monitor = %+v", d)
	}
	if d := got[1]; d.OutputName != "DP-2" || d.Width != 2560 || d.Height != 1440 || !d.Primary || d.DisplayName != "Dell Inc. U2720Q" {
		t.Errorf("second monitor = %+v", d)
	}
	if got := parsePunktfunkMonitors("Kwin: no monitors\n"); len(got) != 0 {
		t.Errorf("no monitors parsed as %+v", got)
	}
}

func TestPunktfunkOutputNamePersists(t *testing.T) {
	stateDir := t.TempDir()
	b := NewPunktfunk("", stateDir, "")
	if got := b.OutputName(); got != "" {
		t.Fatalf("fresh OutputName = %q, want none", got)
	}
	if err := b.SetOutputName("HDMI-A-1"); err != nil {
		t.Fatal(err)
	}
	// A second backend over the same state dir (what the benchmark's
	// restore builds) must see the pin.
	if got := NewPunktfunk("", stateDir, "").OutputName(); got != "HDMI-A-1" {
		t.Fatalf("OutputName = %q, want HDMI-A-1", got)
	}
	if err := b.SetOutputName(""); err != nil {
		t.Fatal(err)
	}
	if got := b.OutputName(); got != "" {
		t.Fatalf("cleared OutputName = %q, want none", got)
	}
	if err := b.SetOutputName(""); err != nil {
		t.Fatalf("clearing an unset pin: %v", err)
	}
}

func TestPunktfunkBinaryLookup(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("punktfunk-host has Linux and Windows builds only")
	}
	name := "punktfunk-host"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exeDir := t.TempDir()
	if PunktfunkAvailable(exeDir) {
		t.Fatal("available with no binary anywhere")
	}

	// A punktfunk-host on PATH is somebody else's build: never run.
	onPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(onPath, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", onPath)
	if got := punktfunkBinaryPath(exeDir); got != "" {
		t.Fatalf("took %q from PATH; only the bundled or downloaded fork may run", got)
	}

	bundled := filepath.Join(exeDir, "punktfunk", name)
	if err := os.MkdirAll(filepath.Dir(bundled), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundled, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := punktfunkBinaryPath(exeDir); got != bundled {
		t.Fatalf("bundled copy must win: %q, want %q", got, bundled)
	}
}

// usePunktfunkStage makes bin (named punktfunk-host) the punktfunk-host the
// agent downloaded, for the test: the only place besides the bundle it is taken from.
func usePunktfunkStage(t *testing.T, bin string) {
	t.Helper()
	prev, _ := punktfunkStageDir.Load().(string)
	SetPunktfunkStageDir(filepath.Dir(bin))
	t.Cleanup(func() { SetPunktfunkStageDir(prev) })
}

// fakePunktfunkHost writes a stand-in punktfunk-host whose probe-compositor
// succeeds only once the .desktop file RequestCapture installs names it --
// KWin's own rule, see RequestCapture's doc comment.
func fakePunktfunkHost(t *testing.T, home string) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("KWin authorization is Linux-only")
	}
	bin := filepath.Join(t.TempDir(), "punktfunk-host")
	script := "#!/bin/sh\n" +
		"PATH=/usr/bin:/bin\n" + // the test empties PATH for the backend's own lookups
		"[ \"$1\" = probe-compositor ] || exit 0\n" +
		"grep -qx \"Exec=" + bin + "\" \"" + filepath.Join(home, ".local", "share", "applications", punktfunkDesktopFile) + "\" 2>/dev/null\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// With Punktfunk active the agent's Screen capture row used to check a KMS
// grant Punktfunk never uses, so it showed "Grant" forever and the button
// did nothing. The row now asks the backend, and Grant installs the
// .desktop file KWin wants.
func TestPunktfunkCaptureGrant(t *testing.T) {
	home := t.TempDir()
	bin := fakePunktfunkHost(t, home)
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir()) // no kbuildsycoca, no other punktfunk-host
	usePunktfunkStage(t, bin)
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	punktfunkCaptureCache.Lock()
	punktfunkCaptureCache.at = time.Time{}
	punktfunkCaptureCache.Unlock()

	b := NewPunktfunk(t.TempDir(), t.TempDir(), "")
	ca, ok := b.(CaptureAccess)
	if !ok {
		t.Fatal("the punktfunk backend must report its own capture permission")
	}
	if ca.CaptureGranted() {
		t.Fatal("granted before KWin was told about the binary")
	}
	if err := ca.RequestCapture(); err != nil {
		t.Fatalf("RequestCapture: %v", err)
	}
	if !ca.CaptureGranted() {
		t.Fatal("still not granted after RequestCapture")
	}
	entry, err := os.ReadFile(filepath.Join(home, ".local", "share", "applications", punktfunkDesktopFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Exec=" + bin + "\n", "zkde_screencast_unstable_v1", "org_kde_kwin_fake_input"} {
		if !strings.Contains(string(entry), want) {
			t.Errorf(".desktop file lacks %q:\n%s", want, entry)
		}
	}
}

// Picking Punktfunk on KDE authorizes it with KWin without a Grant click;
// on any other desktop nothing is written.
func TestPunktfunkCaptureInstalledBeforeStart(t *testing.T) {
	home := t.TempDir()
	bin := fakePunktfunkHost(t, home)
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	usePunktfunkStage(t, bin)
	entry := filepath.Join(home, ".local", "share", "applications", punktfunkDesktopFile)
	resetCache := func() {
		punktfunkCaptureCache.Lock()
		punktfunkCaptureCache.at = time.Time{}
		punktfunkCaptureCache.Unlock()
	}
	b := &punktfunkBackend{}

	t.Setenv("KDE_FULL_SESSION", "")
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	resetCache()
	b.ensureCapture()
	if _, err := os.Stat(entry); err == nil {
		t.Fatal("the KWin file must not be written outside a KDE session")
	}
	// Grant on GNOME writes nothing either and says what the host said.
	resetCache()
	err := b.RequestCapture()
	if _, statErr := os.Stat(entry); statErr == nil {
		t.Fatal("Grant must not write the KWin file outside a KDE session")
	}
	if err == nil || !strings.Contains(err.Error(), "cannot reach the desktop compositor") {
		t.Errorf("a host that is not ready off KDE must be reported, got %v", err)
	}

	// A GNOME host is ready with nothing installed (Mutter's D-Bus API).
	gnome := filepath.Join(t.TempDir(), "punktfunk-host")
	if werr := os.WriteFile(gnome, []byte("#!/bin/sh\necho 'Mutter ready'\n"), 0o755); werr != nil {
		t.Fatal(werr)
	}
	usePunktfunkStage(t, gnome)
	resetCache()
	if rerr := b.RequestCapture(); rerr != nil || !b.CaptureGranted() {
		t.Errorf("a ready GNOME host must need nothing: err=%v", rerr)
	}
	b.ensureCapture()
	if _, statErr := os.Stat(entry); statErr == nil {
		t.Fatal("nothing is installed for a GNOME host")
	}
	usePunktfunkStage(t, bin)

	t.Setenv("XDG_CURRENT_DESKTOP", "ubuntu:KDE")
	resetCache()
	b.ensureCapture()
	if !b.CaptureGranted() {
		t.Fatal("not granted after ensureCapture on KDE")
	}
	// Already granted: the file is left alone.
	before, _ := os.Stat(entry)
	b.ensureCapture()
	if after, _ := os.Stat(entry); before == nil || after == nil || !after.ModTime().Equal(before.ModTime()) {
		t.Error("an authorized host must not have its file rewritten")
	}
}

// Sunshine and RustShine must keep going through the KMS grant.
func TestOnlyPunktfunkReportsCompositorCapture(t *testing.T) {
	for name, b := range map[string]Backend{
		"sunshine":  NewSunshine(t.TempDir(), t.TempDir(), ""),
		"rustshine": NewRustshine(t.TempDir(), t.TempDir(), ""),
	} {
		if _, ok := b.(CaptureAccess); ok {
			t.Errorf("%s must not implement CaptureAccess", name)
		}
	}
}

// Punktfunk has no macOS host, so a Mac agent must never offer it -- not in
// the streamer picker, not in the benchmark -- even when a file with the
// right name is lying around or the override points at one.
func TestPunktfunkUnavailableOffLinuxAndWindows(t *testing.T) {
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		t.Skip("punktfunk-host exists for this OS")
	}
	exeDir := t.TempDir()
	bundled := filepath.Join(exeDir, "punktfunk", "punktfunk-host")
	if err := os.MkdirAll(filepath.Dir(bundled), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundled, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	usePunktfunkStage(t, bundled)
	t.Setenv("PATH", filepath.Dir(bundled))
	if PunktfunkAvailable(exeDir) {
		t.Fatalf("Punktfunk reported available on %s", runtime.GOOS)
	}
}

// fakePunktfunkMonitors points the backend at a stand-in punktfunk-host
// that lists one monitor, HDMI-A-1.
func fakePunktfunkMonitors(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("list-monitors is Linux-only")
	}
	bin := filepath.Join(t.TempDir(), "punktfunk-host")
	script := "#!/bin/sh\n" +
		"[ \"$1\" = list-monitors ] || exit 0\n" +
		"echo 'Kwin:'\n" +
		"echo '  HDMI-A-1      3840x2160@60 at +0,+0  scale 2  Some Panel'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	usePunktfunkStage(t, bin)
	t.Setenv("PATH", t.TempDir())
}

// The agent's device and virtual-display APIs hand every backend the same
// names. Punktfunk must take its own connector (also out of RustShine's
// "card|connector" form) and a "virtual:" spec, and refuse anything else:
// it fails every session on a monitor name that matches nothing.
func TestPunktfunkSetOutputNameAcceptsOnlyWhatItCanStream(t *testing.T) {
	fakePunktfunkMonitors(t)
	b := NewPunktfunk(t.TempDir(), t.TempDir(), "")

	for _, tc := range []struct{ in, want string }{
		{"HDMI-A-1", "HDMI-A-1"},
		{"/dev/dri/card1|HDMI-A-1", "HDMI-A-1"},
		{"virtual:1920x1080@60", "virtual:1920x1080@60"},
	} {
		if err := b.SetOutputName(tc.in); err != nil {
			t.Fatalf("SetOutputName(%q): %v", tc.in, err)
		}
		if got := b.OutputName(); got != tc.want {
			t.Fatalf("SetOutputName(%q) stored %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"0", "DP-9", "/dev/dri/card1|DP-9"} {
		if err := b.SetOutputName(bad); err == nil {
			t.Errorf("SetOutputName(%q) was accepted", bad)
		}
	}
	if got := b.OutputName(); got != "virtual:1920x1080@60" {
		t.Fatalf("a refused name replaced the stored one: %q", got)
	}
}

// The benchmark asks the backend where a session's picture comes from: a
// virtual-display pick means the test video must be moved onto Punktfunk's
// own display, a mirrored monitor means it is already in the picture.
func TestPunktfunkVirtualOutputPrefix(t *testing.T) {
	fakePunktfunkMonitors(t)
	b := NewPunktfunk(t.TempDir(), t.TempDir(), "")
	v, ok := b.(interface{ VirtualOutputPrefix() string })
	if !ok {
		t.Fatal("the punktfunk backend must say which display it streams")
	}
	if got := v.VirtualOutputPrefix(); got != "" {
		t.Fatalf("nothing picked: prefix %q, want none", got)
	}
	if err := b.SetOutputName("HDMI-A-1"); err != nil {
		t.Fatal(err)
	}
	if got := v.VirtualOutputPrefix(); got != "" {
		t.Fatalf("mirrored monitor: prefix %q, want none", got)
	}
	if err := b.SetOutputName("virtual:1920x1080@60"); err != nil {
		t.Fatal(err)
	}
	if got := v.VirtualOutputPrefix(); got != "Virtual-punktfunk" {
		t.Fatalf("virtual display: prefix %q, want Virtual-punktfunk", got)
	}
}
