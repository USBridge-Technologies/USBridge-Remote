package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
)

// Two monitors as the agent reports them for a laptop with a monitor above
// it -- the setup that sent the two streamers to different screens.
const twoMonitorsStatus = `{"active_backend":"rustshine","available_backends":["sunshine","rustshine"],"monitors":[
	{"id":"\\\\.\\DISPLAY1","name":"GPD1001H","x":0,"y":0,"width":2560,"height":1600,"primary":true},
	{"id":"\\\\.\\DISPLAY6","name":"ARZOPA","x":0,"y":-1600,"width":2560,"height":1600,"primary":false}]}`

func withBenchStatus(t *testing.T, status string) {
	t.Helper()
	prev := benchTestStatus
	benchTestStatus = status
	benchTestMonitor.Store("<not run>")
	t.Cleanup(func() { benchTestStatus = prev })
}

// monitorSelect finds the setup dialog's monitor list (the only dropdown
// whose options mention a resolution).
func monitorSelect(w fyne.Window) *view.HeaderDropdown {
	o := findInOverlays(w, func(o fyne.CanvasObject) bool {
		s, ok := o.(*view.HeaderDropdown)
		return ok && len(s.Options) > 0 && strings.Contains(s.Options[0], "2560x1600")
	})
	if o == nil {
		return nil
	}
	return o.(*view.HeaderDropdown)
}

func quickRun(ran chan<- struct{}) benchRunStub {
	return func(ctx context.Context, backends []string, window time.Duration, progress benchmarkProgress) (*benchmarkResult, error) {
		close(ran)
		return sampleBenchResult(), nil
	}
}

func TestBenchMonitorChoicesLabelEachMonitorAndDefaultToPrimary(t *testing.T) {
	i18n.Init("en")
	choices, def := benchMonitorChoices([]api.BenchMonitor{
		{ID: `\\.\DISPLAY6`, Name: "ARZOPA", Width: 2560, Height: 1600},
		{ID: `\\.\DISPLAY1`, Name: "GPD1001H", Width: 2560, Height: 1600, Primary: true},
		{ID: `\\.\DISPLAY2`, Width: 1920, Height: 1080},
	})
	want := []benchMonitorChoice{
		{"ARZOPA (DISPLAY6) · 2560x1600", `\\.\DISPLAY6`},
		{"GPD1001H (DISPLAY1) · 2560x1600 · primary", `\\.\DISPLAY1`},
		{"DISPLAY2 · 1920x1080", `\\.\DISPLAY2`},
	}
	if len(choices) != len(want) {
		t.Fatalf("%d choices, want %d", len(choices), len(want))
	}
	for i := range want {
		if choices[i] != want[i] {
			t.Errorf("choice %d = %+v, want %+v", i, choices[i], want[i])
		}
	}
	if def != 1 {
		t.Errorf("default choice %d, want 1 (the primary monitor)", def)
	}
}

func TestBenchmarkSetupDefaultsToThePrimaryMonitor(t *testing.T) {
	withBenchStatus(t, twoMonitorsStatus)
	ran := make(chan struct{})
	mw, w := newBenchTestWindow(t, quickRun(ran))
	openBenchmarkSetup(t, mw, w)

	sel := monitorSelect(w)
	if sel == nil {
		t.Fatal("the setup dialog has no monitor list")
	}
	if !strings.HasPrefix(sel.Selected, "GPD1001H (DISPLAY1)") {
		t.Fatalf("preselected %q, want the primary monitor", sel.Selected)
	}
	tap(findButton(w, i18n.Current.BenchStart))
	<-ran
	if got := benchTestMonitor.Load(); got != `\\.\DISPLAY1` {
		t.Fatalf("benchmark ran on monitor %q, want the primary DISPLAY1", got)
	}
}

func TestBenchmarkSetupRunsOnThePickedMonitor(t *testing.T) {
	withBenchStatus(t, twoMonitorsStatus)
	ran := make(chan struct{})
	mw, w := newBenchTestWindow(t, quickRun(ran))
	openBenchmarkSetup(t, mw, w)

	sel := monitorSelect(w)
	if sel == nil {
		t.Fatal("the setup dialog has no monitor list")
	}
	fyne.DoAndWait(func() { sel.SetSelected(sel.Options[1]) }) // ARZOPA
	tap(findButton(w, i18n.Current.BenchStart))
	<-ran
	if got := benchTestMonitor.Load(); got != `\\.\DISPLAY6` {
		t.Fatalf("benchmark ran on monitor %q, want the picked DISPLAY6", got)
	}
}

func TestBenchmarkSetupWithoutMonitorsPinsNone(t *testing.T) {
	withBenchStatus(t, `{"active_backend":"sunshine","available_backends":["sunshine","rustshine"]}`)
	ran := make(chan struct{})
	mw, w := newBenchTestWindow(t, quickRun(ran))
	openBenchmarkSetup(t, mw, w)

	if monitorSelect(w) != nil {
		t.Fatal("monitor list shown although the host reported no monitors")
	}
	tap(findButton(w, i18n.Current.BenchStart))
	<-ran
	if got := benchTestMonitor.Load(); got != "" {
		t.Fatalf("benchmark ran on monitor %q, want none pinned", got)
	}
}

func TestBenchVideoPlacementError(t *testing.T) {
	cases := []struct {
		name  string
		video api.BenchVideo
		fail  bool
	}{
		{"nothing pinned", api.BenchVideo{Monitor: `\\.\DISPLAY6`}, false},
		{"on the pinned monitor", api.BenchVideo{RequestedMonitor: `\\.\DISPLAY6`, Monitor: `\\.\DISPLAY6`}, false},
		{"on another monitor", api.BenchVideo{RequestedMonitor: `\\.\DISPLAY6`, Monitor: `\\.\DISPLAY1`}, true},
		{"window not visible to the agent", api.BenchVideo{RequestedMonitor: `\\.\DISPLAY6`}, false},
	}
	for _, c := range cases {
		err := benchVideoPlacementError(c.video)
		if (err != nil) != c.fail {
			t.Errorf("%s: error = %v, want failure %v", c.name, err, c.fail)
		}
	}
}

// BenchSetMonitor and BenchVideoStart against a fake agent: the pin goes
// out as {"monitor": id}, and the player's actual monitor comes back.
func TestBenchMonitorAgentCalls(t *testing.T) {
	var pinned []string
	deferred := true
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/bench/monitor":
			var body struct {
				Monitor      string `json:"monitor"`
				DeferRestart bool   `json:"defer_restart"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			pinned = append(pinned, body.Monitor)
			deferred = deferred && body.DeferRestart
			_, _ = rw.Write([]byte(`{"success":true,"data":{}}`))
		case "/api/bench/backend":
			_, _ = rw.Write([]byte(`{"success":true,"data":{"active_backend":"rustshine","switch_ms":26500,"stopped":"sunshine","stop_ms":900,"start_ms":25600}}`))
		case "/api/bench/video/start":
			_, _ = rw.Write([]byte(`{"success":true,"data":{"player":"ffplay","content":"bbb.mp4","requested_monitor":"\\\\.\\DISPLAY6","monitor":"\\\\.\\DISPLAY6"}}`))
		default:
			http.NotFound(rw, r)
		}
	}))
	defer srv.Close()
	client := newTestUSBClient(t, srv)

	if err := client.BenchSetMonitor(`\\.\DISPLAY6`); err != nil {
		t.Fatal(err)
	}
	if err := client.BenchSetMonitor(""); err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 2 || pinned[0] != `\\.\DISPLAY6` || pinned[1] != "" {
		t.Fatalf("pins sent %q, want [DISPLAY6, release]", pinned)
	}
	if !deferred {
		t.Fatal("pin/release sent without defer_restart: the agent would restart the streamer twice")
	}
	sw, err := client.BenchSetBackend("rustshine")
	if err != nil {
		t.Fatal(err)
	}
	if sw != (api.BenchSwitch{SwitchMs: 26500, Stopped: "sunshine", StopMs: 900, StartMs: 25600}) {
		t.Fatalf("switch timing = %+v", sw)
	}
	v, err := client.BenchVideoStart()
	if err != nil {
		t.Fatal(err)
	}
	if v.Content != "bbb.mp4 (ffplay)" || v.RequestedMonitor != `\\.\DISPLAY6` || v.Monitor != `\\.\DISPLAY6` {
		t.Fatalf("video = %+v", v)
	}
}

func TestBenchVirtualDisplayIsItsOwnChoice(t *testing.T) {
	i18n.Init("en")
	mons := []api.BenchMonitor{
		{ID: `\\.\DISPLAY1`, Name: "GPD1001H", Width: 2560, Height: 1600, Primary: true},
		{ID: benchVirtualMonitorID, Name: "Virtual display", Width: 2560, Height: 1440},
	}
	choices, def := benchMonitorChoices(mons)
	if len(choices) != 1 || choices[0].id != `\\.\DISPLAY1` || def != 0 {
		t.Fatalf("as-is choices = %+v (default %d), want the real monitor only", choices, def)
	}
	v, ok := benchVirtualMonitor(mons)
	if !ok {
		t.Fatal("virtual display entry not found")
	}
	if got := benchVirtualSpec(v, 1920, 1080); got != "virtual:1920x1080@120" {
		t.Errorf("picked resolution: %s", got)
	}
	if got := benchVirtualSpec(v, 0, 0); got != "virtual:2560x1440@120" {
		t.Errorf("saved resolution: %s", got)
	}
	if got := benchVirtualSpec(api.BenchMonitor{}, 0, 0); got != "virtual:1920x1080@120" {
		t.Errorf("no size: %s", got)
	}
}
