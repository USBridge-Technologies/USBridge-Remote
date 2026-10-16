package moonlight

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// A Vibepollo /serverinfo (unpaired, HTTP) as it reports its virtual
// display: ptree writes the bools as "true"/"false".
const vibepolloServerInfo = `<?xml version="1.0" encoding="utf-8"?>
<root status_code="200"><hostname>PC</hostname><appversion>7.1.431.-1</appversion><GfeVersion>3.23.0.74</GfeVersion><uniqueid>X</uniqueid><HttpsPort>47984</HttpsPort><ExternalPort>47989</ExternalPort><VirtualDisplayCapable>true</VirtualDisplayCapable><VirtualDisplayDriverReady>false</VirtualDisplayDriverReady><VirtualDisplayHDRCapable>true</VirtualDisplayHDRCapable><mac>00:00:00:00:00:00</mac><PairStatus>0</PairStatus><ServerCodecModeSupport>3</ServerCodecModeSupport></root>`

func TestServerInfoVirtualDisplay(t *testing.T) {
	var info ServerInfo
	if err := xml.Unmarshal([]byte(vibepolloServerInfo), &info); err != nil {
		t.Fatal(err)
	}
	if !info.VirtualDisplayCapable || info.VirtualDisplayDriverReady {
		t.Fatalf("capable=%v ready=%v, want true/false", info.VirtualDisplayCapable, info.VirtualDisplayDriverReady)
	}
	// Sunshine and RustShine say nothing: both false.
	var plain ServerInfo
	if err := xml.Unmarshal([]byte(`<root><appversion>7.1</appversion><PairStatus>1</PairStatus></root>`), &plain); err != nil {
		t.Fatal(err)
	}
	if plain.VirtualDisplayCapable || plain.VirtualDisplayDriverReady {
		t.Fatalf("plain host reports a virtual display")
	}
}

// The extra parameters reach both /launch and its /resume fallback.
func TestLaunchSendsExtraParams(t *testing.T) {
	var mu sync.Mutex
	got := map[string]string{}
	running := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		q := r.URL.Query()
		got[r.URL.Path] = q.Get("virtualDisplay") + "," + q.Get("hdrMode")
		if r.URL.Path == "/launch" && running {
			fmt.Fprint(w, `<root status_code="400"></root>`)
			return
		}
		running = true
		fmt.Fprint(w, `<root status_code="200"><sessionUrl0>rtsp://127.0.0.1:48010</sessionUrl0></root>`)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	if _, _, err := c.Launch(1, "h265", 2560, 1440, 120, 30000, map[string]string{"virtualDisplay": "1", "hdrMode": "1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Launch(1, "h265", 2560, 1440, 120, 30000, map[string]string{"virtualDisplay": "0"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got["/launch"] != "0," || got["/resume"] != "0," {
		t.Fatalf("second launch sent %q, its resume %q; want \"0,\" both", got["/launch"], got["/resume"])
	}
}
