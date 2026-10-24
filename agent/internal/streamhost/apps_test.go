package streamhost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Sunshine-written apps.json (string booleans, a fork's own keys) comes
// back with every unedited key as it was.
func TestHostAppsRoundTripKeepsForkKeys(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "env": {"PATH": "$(PATH);C:\\Tools"},
  "apps": [
    {"name": "Desktop", "image-path": "desktop.png", "uuid": "A1"},
    {"name": "Game", "cmd": "game.exe", "working-dir": "C:\\Games", "elevated": "true",
     "auto-detach": "false", "exit-timeout": "9", "virtual-display": true,
     "prep-cmd": [{"do": "a", "undo": "b", "elevated": "true"}]}
  ]
}`
	if err := os.WriteFile(HostAppsPath(dir), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	apps, err := LoadHostApps(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[1].Cmd != "game.exe" || !apps[1].Elevated || apps[1].AutoDetach || !apps[1].WaitAll || apps[1].ExitTimeout != 9 {
		t.Fatalf("parsed %+v", apps)
	}
	if p := apps[1].PrepCmd; len(p) != 1 || p[0] != (HostAppPrepCmd{Do: "a", Undo: "b", Elevated: true}) {
		t.Fatalf("prep %+v", p)
	}
	apps[1].Name = "Game 2"
	if err := SaveHostApps(dir, apps); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(HostAppsPath(dir))
	var m struct {
		Env  map[string]string `json:"env"`
		Apps []map[string]any  `json:"apps"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Env["PATH"] != `$(PATH);C:\Tools` {
		t.Fatalf("env lost: %v", m.Env)
	}
	if m.Apps[0]["uuid"] != "A1" || m.Apps[1]["virtual-display"] != true || m.Apps[1]["name"] != "Game 2" {
		t.Fatalf("written %v", m.Apps)
	}
	if m.Apps[1]["elevated"] != true || m.Apps[1]["exit-timeout"] != float64(9) {
		t.Fatalf("flags written as %v", m.Apps[1])
	}
}

func TestHostAppsDefaultUntilSaved(t *testing.T) {
	dir := t.TempDir()
	apps, err := LoadHostApps(dir)
	if err != nil || len(apps) == 0 || apps[0].Name != "Desktop" {
		t.Fatalf("default %v %v", apps, err)
	}
	if err := SaveHostApps(dir, []HostApp{{Name: " "}}); err == nil {
		t.Fatal("a nameless app was saved")
	}
}

func TestAdoptHostAppsFileOnce(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "sunshine", "apps.json")
	os.MkdirAll(filepath.Dir(legacy), 0o755)
	os.WriteFile(legacy, []byte(`{"apps":[{"name":"Old"}]}`), 0o644)
	adoptHostAppsFile(dir, legacy)
	apps, _ := LoadHostApps(dir)
	if len(apps) != 1 || apps[0].Name != "Old" {
		t.Fatalf("adopted %v", apps)
	}
	os.WriteFile(legacy, []byte(`{"apps":[{"name":"Newer"}]}`), 0o644)
	adoptHostAppsFile(dir, legacy)
	if apps, _ := LoadHostApps(dir); apps[0].Name != "Old" {
		t.Fatalf("adopted twice: %v", apps)
	}
}

func TestPunktfunkAppsConversion(t *testing.T) {
	steam := NewHostApp("Steam Big Picture")
	steam.Detached = []string{"steam://open/bigpicture"}
	steam.PrepCmd = []HostAppPrepCmd{{Undo: "steam://close/bigpicture"}}
	game := NewHostApp("Game")
	game.Cmd = "./game"
	game.WorkingDir = "/opt/game"
	got := punktfunkApps([]HostApp{NewHostApp("Desktop"), steam, game})
	if got[0].ID != 1 || got[0].Cmd != "" || got[0].Icon != "monitor" {
		t.Fatalf("desktop %+v", got[0])
	}
	if got[1].Cmd != "steam://open/bigpicture" || got[1].Icon != "steam" || len(got[1].Prep) != 1 || got[1].Prep[0].Undo != "steam://close/bigpicture" {
		t.Fatalf("steam %+v", got[1])
	}
	if got[2].ID != 3 || !strings.Contains(got[2].Cmd, "/opt/game") || !strings.HasSuffix(got[2].Cmd, "./game") {
		t.Fatalf("game %+v", got[2])
	}
}

func TestWritePunktfunkAppsOnlyAfterASave(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "punktfunk")
	if err := writePunktfunkApps(dir, conf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(conf, "apps.json")); !os.IsNotExist(err) {
		t.Fatal("wrote punktfunk's apps.json with no saved list")
	}
	SaveHostApps(dir, DefaultHostApps())
	if err := writePunktfunkApps(dir, conf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(conf, "apps.json")); err != nil {
		t.Fatal(err)
	}
}
