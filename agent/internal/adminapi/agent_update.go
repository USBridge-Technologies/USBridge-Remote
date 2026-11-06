package adminapi

import (
	"context"
	"net/http"
	"sync"

	"usbridge_agent/internal/update"

	"github.com/sirupsen/logrus"
)

// Agent self-update for a GUI that only attached to the engine (a Windows
// service, a headless systemd/launchd instance). The engine process is the
// one that must check, pick the channel and apply: it is the instance that
// runs, and on Windows it runs as LocalSystem, whose channel file and
// install rights are its own. The GUI drives it through these endpoints and
// polls the status while the download runs.

// AgentVersion is this engine's version, set by app.Start.
var AgentVersion string

type agentUpdateState struct {
	mu       sync.Mutex
	pending  *update.Manifest
	active   bool  // download (then apply) running
	applying bool  // download verified, the swap is starting: this process exits next
	done     int64 // bytes
	total    int64
	errText  string
}

var agentUpd agentUpdateState

// AgentUpdateStatus is GET /agent-update/status.
type AgentUpdateStatus struct {
	Active   bool   `json:"active"`
	Applying bool   `json:"applying"`
	Done     int64  `json:"done"`
	Total    int64  `json:"total"`
	Error    string `json:"error,omitempty"`
	Version  string `json:"version,omitempty"` // the version being installed
}

type agentUpdateChannelBody struct {
	Test bool `json:"test"`
}

type agentUpdateCheckBody struct {
	Version string `json:"version,omitempty"` // "" = up to date
	Error   string `json:"error,omitempty"`
}

func (s *Server) registerAgentUpdate(mux *http.ServeMux) {
	mux.HandleFunc("GET /agent-update/channel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, agentUpdateChannelBody{Test: update.GetChannel() == update.ChannelTest})
	})
	mux.HandleFunc("POST /agent-update/channel", func(w http.ResponseWriter, r *http.Request) {
		var body agentUpdateChannelBody
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
			return
		}
		if body.Test {
			update.SetChannel(update.ChannelTest)
		} else {
			update.SetChannel(update.ChannelStable)
		}
		writeJSON(w, http.StatusOK, struct{}{})
	})
	mux.HandleFunc("POST /agent-update/check", func(w http.ResponseWriter, r *http.Request) {
		m, err := update.CheckNow(r.Context(), AgentVersion)
		agentUpd.mu.Lock()
		if !agentUpd.active {
			agentUpd.pending = m
		}
		agentUpd.mu.Unlock()
		out := agentUpdateCheckBody{}
		if m != nil {
			out.Version = m.Version
		}
		if err != nil {
			out.Error = err.Error()
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /agent-update/install", func(w http.ResponseWriter, r *http.Request) {
		agentUpd.mu.Lock()
		m := agentUpd.pending
		if m == nil || agentUpd.active {
			busy := agentUpd.active
			agentUpd.mu.Unlock()
			if busy {
				writeJSON(w, http.StatusOK, struct{}{})
				return
			}
			writeJSON(w, http.StatusConflict, errorBody{Error: "no update pending -- check first"})
			return
		}
		agentUpd.active, agentUpd.applying = true, false
		agentUpd.done, agentUpd.total, agentUpd.errText = 0, 0, ""
		agentUpd.mu.Unlock()
		go func() {
			logrus.WithField("component", "update").Infof("installing %s on request of the attached GUI", m.Version)
			err := update.DownloadAndApply(context.Background(), m, func(done, total int64) {
				agentUpd.mu.Lock()
				agentUpd.done, agentUpd.total = done, total
				if total > 0 && done >= total {
					agentUpd.applying = true
				}
				agentUpd.mu.Unlock()
			})
			// A successful apply exits this process; a package-managed one
			// (deb/rpm) returns nil and restarts the service on its own.
			agentUpd.mu.Lock()
			agentUpd.active = false
			if err != nil {
				agentUpd.applying = false
				agentUpd.errText = err.Error()
				logrus.WithField("component", "update").WithError(err).Error("failed to apply update")
			} else {
				agentUpd.applying = true
			}
			agentUpd.mu.Unlock()
		}()
		writeJSON(w, http.StatusOK, struct{}{})
	})
	mux.HandleFunc("GET /agent-update/status", func(w http.ResponseWriter, r *http.Request) {
		agentUpd.mu.Lock()
		st := AgentUpdateStatus{Active: agentUpd.active, Applying: agentUpd.applying,
			Done: agentUpd.done, Total: agentUpd.total, Error: agentUpd.errText}
		if agentUpd.pending != nil {
			st.Version = agentUpd.pending.Version
		}
		agentUpd.mu.Unlock()
		writeJSON(w, http.StatusOK, st)
	})
}

// AgentUpdateChannel reports whether the engine follows test builds.
func (c *Client) AgentUpdateChannel() (bool, error) {
	var out agentUpdateChannelBody
	err := c.do(http.MethodGet, "/agent-update/channel", nil, &out)
	return out.Test, err
}

func (c *Client) SetAgentUpdateChannel(test bool) error {
	return c.do(http.MethodPost, "/agent-update/channel", agentUpdateChannelBody{Test: test}, nil)
}

// CheckAgentUpdate returns the newer version on the engine's channel, ""
// when up to date.
func (c *Client) CheckAgentUpdate() (string, error) {
	var out agentUpdateCheckBody
	if err := c.do(http.MethodPost, "/agent-update/check", nil, &out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return out.Version, errString(out.Error)
	}
	return out.Version, nil
}

func (c *Client) InstallAgentUpdate() error {
	return c.do(http.MethodPost, "/agent-update/install", nil, nil)
}

func (c *Client) AgentUpdateStatus() (AgentUpdateStatus, error) {
	var out AgentUpdateStatus
	err := c.do(http.MethodGet, "/agent-update/status", nil, &out)
	return out, err
}

type errString string

func (e errString) Error() string { return string(e) }
