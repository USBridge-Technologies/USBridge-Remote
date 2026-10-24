package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Streamer benchmark endpoints (agent/internal/api/bench.go).

// BenchStatus is the agent's GET /api/bench/status payload.
type BenchStatus struct {
	ActiveBackend     string   `json:"active_backend"`
	AvailableBackends []string `json:"available_backends"`
	// ListedBackends: every streamer the host's OS has (empty on agents from
	// before this existed).
	ListedBackends []string `json:"listed_backends"`
	Video             struct {
		Player      string `json:"player"`
		Content     string `json:"content"`
		Playing     bool   `json:"playing"`
		ContentPath string `json:"content_path"`
	} `json:"video"`
	// Monitors the benchmark can be pinned to (empty on hosts that can't
	// enumerate them, and on agents from before this existed); Monitor is
	// the current pin, "" for none.
	Monitors []BenchMonitor `json:"monitors"`
	Monitor  string         `json:"monitor"`
	// GPU names the host's GPU, for Net Graph's GPU line (see that struct
	// field's doc comment on the agent side, agent/internal/api/bench.go).
	GPU string `json:"gpu"`
}

// BenchMonitor is one host monitor (agent/internal/monitors.Monitor).
type BenchMonitor struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Primary bool   `json:"primary"`
}

// BenchVideo is what BenchVideoStart started: the content and player, and
// the monitor the player was asked for vs. the one its window is really on
// ("" when the agent couldn't see it).
type BenchVideo struct {
	Content          string
	RequestedMonitor string
	Monitor          string
}

type agentResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Error   string          `json:"error"`
	Details string          `json:"details"`
	Data    json.RawMessage `json:"data"`
}

func decodeAgentResponse(body []byte, err error, out any) error {
	if err != nil {
		// The agent's error body is JSON; surface its details, not the
		// whole "HTTP error 500: {...}" string.
		msg := err.Error()
		if i := strings.Index(msg, "{"); i >= 0 {
			var r agentResponse
			if json.Unmarshal([]byte(msg[i:]), &r) == nil && (r.Details != "" || r.Error != "") {
				if r.Details != "" {
					return fmt.Errorf("%s", r.Details)
				}
				return fmt.Errorf("%s", r.Error)
			}
		}
		return err
	}
	var r agentResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}
	if !r.Success {
		if r.Details != "" {
			return fmt.Errorf("%s", r.Details)
		}
		return fmt.Errorf("%s", r.Error)
	}
	if out != nil && len(r.Data) > 0 {
		return json.Unmarshal(r.Data, out)
	}
	return nil
}

// BenchStatus reports the agent's active/available streamers and player.
func (c *USBClient) BenchStatus() (*BenchStatus, error) {
	body, err := c.makeRequest("GET", "/api/bench/status", nil)
	var st BenchStatus
	if err := decodeAgentResponse(body, err, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// BenchSwitch is how long the agent took to switch streamers: SwitchMs in
// all, split into stopping the previous one (StopMs, until its ports were
// free; Stopped names it) and starting this one (StartMs, until its
// listener answered). An agent that predates the split reports SwitchMs
// only.
type BenchSwitch struct {
	SwitchMs float64 `json:"switch_ms"`
	Stopped  string  `json:"stopped,omitempty"`
	StopMs   float64 `json:"stop_ms"`
	StartMs  float64 `json:"start_ms"`
}

// BenchSetBackend switches the agent to kind (or, with the same kind,
// carries out a restart BenchSetMonitor left pending) and reports the
// timing.
func (c *USBClient) BenchSetBackend(kind string) (BenchSwitch, error) {
	payload, _ := json.Marshal(map[string]string{"kind": kind})
	body, err := c.PostRawWithTimeout("/api/bench/backend", payload, 2*time.Minute)
	var out BenchSwitch
	if err := decodeAgentResponse(body, err, &out); err != nil {
		return BenchSwitch{}, err
	}
	return out, nil
}

// BenchSetMonitor pins both streamers' capture and the test video to one
// host monitor for the benchmark; "" releases the pin and puts each
// streamer's own monitor back. Changing the active streamer's monitor
// restarts it, hence the long timeout.
func (c *USBClient) BenchSetMonitor(id string) error {
	// defer_restart: the agent doesn't restart the running streamer for
	// this; the BenchSetBackend that always follows does, once, instead
	// of a restart here and another one right after it. An older agent
	// ignores the field and restarts here as before.
	payload, _ := json.Marshal(map[string]any{"monitor": id, "defer_restart": true})
	body, err := c.PostRawWithTimeout("/api/bench/monitor", payload, 2*time.Minute)
	return decodeAgentResponse(body, err, nil)
}

// BenchPrepare has the agent download the benchmark content. ready=false
// means it will fall back to a generated pattern (reason in note).
func (c *USBClient) BenchPrepare() (ready bool, note string, err error) {
	body, err := c.PostRawWithTimeout("/api/bench/prepare", []byte("{}"), 4*time.Minute)
	var out struct {
		Ready bool   `json:"ready"`
		Error string `json:"error"`
	}
	if err := decodeAgentResponse(body, err, &out); err != nil {
		return false, "", err
	}
	return out.Ready, out.Error, nil
}

// BenchVideoStart starts the content fullscreen on the host from its first
// frame, on the pinned monitor if there is one, and reports what's playing
// where.
func (c *USBClient) BenchVideoStart() (BenchVideo, error) { return c.BenchVideoStartFPS(0) }

// BenchVideoStartFPS is BenchVideoStart for streamers asked for fps: above 60 the host
// plays a pattern that changes that often (its 60fps clip would cap every streamer).
func (c *USBClient) BenchVideoStartFPS(fps int) (BenchVideo, error) {
	body, err := c.PostRawWithTimeout("/api/bench/video/start", []byte(fmt.Sprintf(`{"fps":%d}`, fps)), 4*time.Minute)
	var out struct {
		Player           string `json:"player"`
		Content          string `json:"content"`
		RequestedMonitor string `json:"requested_monitor"`
		Monitor          string `json:"monitor"`
	}
	if err := decodeAgentResponse(body, err, &out); err != nil {
		return BenchVideo{}, err
	}
	return BenchVideo{Content: out.Content + " (" + out.Player + ")", RequestedMonitor: out.RequestedMonitor, Monitor: out.Monitor}, nil
}

// BenchVideoStop closes the host-side player.
func (c *USBClient) BenchVideoStop() error {
	body, err := c.PostRawWithTimeout("/api/bench/video/stop", []byte("{}"), 20*time.Second)
	return decodeAgentResponse(body, err, nil)
}

// BenchLoadStart has the agent start sampling the host's CPU/GPU load.
// An agent without the endpoint answers with an error; the benchmark then
// runs without load data.
func (c *USBClient) BenchLoadStart() error {
	body, err := c.PostRawWithTimeout("/api/bench/load/start", []byte("{}"), 20*time.Second)
	return decodeAgentResponse(body, err, nil)
}

// BenchLoadStop ends the sampling and returns the samples as JSON (a
// list of service.BenchLoad; service imports this package, so it decodes).
// err is non-nil when the agent collected zero samples AND can name why
// (hostload.Sampler.LastError on the agent side) -- previously that
// reason only ever reached the agent's own log file, so an operator
// looking at the client's benchmark results saw every host-load row as a
// bare "n/a" with nothing explaining it.
func (c *USBClient) BenchLoadStop() (json.RawMessage, error) {
	body, err := c.PostRawWithTimeout("/api/bench/load/stop", []byte("{}"), 20*time.Second)
	var out struct {
		Samples json.RawMessage `json:"samples"`
		Error   string          `json:"error,omitempty"`
	}
	if err := decodeAgentResponse(body, err, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return out.Samples, fmt.Errorf("host load sampling unavailable: %s", out.Error)
	}
	return out.Samples, nil
}
