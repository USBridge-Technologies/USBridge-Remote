package adminapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/sunshine"
	"usbridge_agent/internal/tailscale"
)

// TokenBackend mirrors the operations internal/ui.Window drives on the
// engine that owns the agent's config/Sunshine lifecycle (normally *app.App).
type TokenBackend interface {
	RegenerateMasterKey() (config.Config, error)
	SaveConfig(config.Config) error
	SunshineCaptureMode() string
	SetSunshineCaptureMode(mode string) error
	KMSCaptureGranted() bool
	RequestKMSCapture() bool
	RestartSunshine() error
	ListSunshineClients() ([]sunshine.Client, error)
	UnpairSunshineClient(uniqueID string) error
	SubmitMoonlightPIN(pin string) error
	UpdateListenAddr(host string, port int) (config.Config, error)
	UpdateSunshinePort(port int) (config.Config, error)
	UpdateSunshineStreamAddr(host string, streamPort int) (config.Config, error)
}

// PermsBackend mirrors internal/permissions.Service's methods.
type PermsBackend interface {
	AccessibilityGranted() bool
	ScreenRecordingGranted() bool
	RequestAccessibility() bool
	RequestScreenRecording() bool
	OpenPrivacySettings() error
	OpenScreenRecordingSettings() error
}

// TSBackend mirrors internal/tailscale.Service's methods used by the GUI.
type TSBackend interface {
	Status(context.Context) (*tailscale.Status, error)
	StartLogin(context.Context) (string, error)
	Logout(context.Context) error
	SetAuthURLHandler(func(string))
}

// Server exposes TokenBackend/PermsBackend/TSBackend over a local unix
// socket so a GUI process can attach to an already-running headless agent
// instead of starting a second engine. Never listens on any network
// interface — socketPath is a filesystem path under the agent's StateDir,
// permissioned 0600 so only the owning user can connect.
type Server struct {
	socketPath string
	token      TokenBackend
	perms      PermsBackend
	ts         TSBackend
	cfgFn      func() config.Config

	listener net.Listener
	httpSrv  *http.Server

	mu      sync.Mutex
	subs    map[int]chan string
	nextSub int
}

// NewServer builds the socket listener and route table but does not start
// serving — call Serve (typically in its own goroutine) to do that.
func NewServer(socketPath string, token TokenBackend, perms PermsBackend, ts TSBackend, cfgFn func() config.Config) (*Server, error) {
	// A stale socket file left behind by a previous crash makes a fresh
	// Listen fail with "address already in use" even though nothing is
	// listening on it — remove it first. If another instance actually IS
	// live, the caller already probed for that via Dial before ever
	// reaching here (see app.Start), so this is safe.
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen admin socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod admin socket: %w", err)
	}

	s := &Server{
		socketPath: socketPath,
		token:      token,
		perms:      perms,
		ts:         ts,
		cfgFn:      cfgFn,
		listener:   ln,
		subs:       make(map[int]chan string),
	}

	if ts != nil {
		ts.SetAuthURLHandler(s.broadcastAuthURL)
	}

	s.httpSrv = &http.Server{Handler: s.routes()}
	return s, nil
}

// Serve blocks, accepting connections until Close is called. Run it in its
// own goroutine.
func (s *Server) Serve() error {
	err := s.httpSrv.Serve(s.listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Close shuts down the listener and removes the socket file.
func (s *Server) Close() error {
	err := s.httpSrv.Close()
	_ = os.Remove(s.socketPath)
	return err
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.cfgFn())
	})

	mux.HandleFunc("POST /token/regenerate-master-key", s.handleRegenerateMasterKey)
	mux.HandleFunc("POST /token/save-config", s.handleSaveConfig)
	mux.HandleFunc("GET /token/capture-mode", s.handleGetCaptureMode)
	mux.HandleFunc("POST /token/capture-mode", s.handleSetCaptureMode)
	mux.HandleFunc("GET /token/kms-granted", s.handleKMSGranted)
	mux.HandleFunc("POST /token/request-kms", s.handleRequestKMS)
	mux.HandleFunc("POST /token/restart-sunshine", s.handleRestartSunshine)
	mux.HandleFunc("GET /token/clients", s.handleListClients)
	mux.HandleFunc("POST /token/unpair", s.handleUnpair)
	mux.HandleFunc("POST /token/pin", s.handlePIN)
	mux.HandleFunc("POST /token/listen-addr", s.handleListenAddr)
	mux.HandleFunc("POST /token/sunshine-port", s.handleSunshinePort)
	mux.HandleFunc("POST /token/sunshine-stream-addr", s.handleSunshineStreamAddr)

	mux.HandleFunc("GET /perms/accessibility", s.handlePermsBool(func() bool { return s.perms.AccessibilityGranted() }))
	mux.HandleFunc("GET /perms/screen-recording", s.handlePermsBool(func() bool { return s.perms.ScreenRecordingGranted() }))
	mux.HandleFunc("POST /perms/request-accessibility", s.handlePermsBool(func() bool { return s.perms.RequestAccessibility() }))
	mux.HandleFunc("POST /perms/request-screen-recording", s.handlePermsBool(func() bool { return s.perms.RequestScreenRecording() }))
	mux.HandleFunc("POST /perms/open-privacy-settings", s.handlePermsErr(func() error { return s.perms.OpenPrivacySettings() }))
	mux.HandleFunc("POST /perms/open-screen-recording-settings", s.handlePermsErr(func() error { return s.perms.OpenScreenRecordingSettings() }))

	mux.HandleFunc("GET /ts/status", s.handleTSStatus)
	mux.HandleFunc("POST /ts/login", s.handleTSLogin)
	mux.HandleFunc("POST /ts/logout", s.handleTSLogout)
	mux.HandleFunc("GET /ts/auth-stream", s.handleTSAuthStream)

	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, errorBody{Error: err.Error()})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) handleRegenerateMasterKey(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.token.RegenerateMasterKey()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	if err := readJSON(r, &cfg); err != nil {
		writeError(w, err)
		return
	}
	if err := s.token.SaveConfig(cfg); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleGetCaptureMode(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, captureModeBody{Mode: s.token.SunshineCaptureMode()})
}

func (s *Server) handleSetCaptureMode(w http.ResponseWriter, r *http.Request) {
	var body captureModeBody
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.token.SetSunshineCaptureMode(body.Mode); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleKMSGranted(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, boolBody{Value: s.token.KMSCaptureGranted()})
}

func (s *Server) handleRequestKMS(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, boolBody{Value: s.token.RequestKMSCapture()})
}

func (s *Server) handleRestartSunshine(w http.ResponseWriter, r *http.Request) {
	if err := s.token.RestartSunshine(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request) {
	clients, err := s.token.ListSunshineClients()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

func (s *Server) handleUnpair(w http.ResponseWriter, r *http.Request) {
	var body unpairBody
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.token.UnpairSunshineClient(body.UniqueID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handlePIN(w http.ResponseWriter, r *http.Request) {
	var body pinBody
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.token.SubmitMoonlightPIN(body.PIN); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleListenAddr(w http.ResponseWriter, r *http.Request) {
	var body listenAddrBody
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	cfg, err := s.token.UpdateListenAddr(body.Host, body.Port)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSunshinePort(w http.ResponseWriter, r *http.Request) {
	var body sunshinePortBody
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	cfg, err := s.token.UpdateSunshinePort(body.Port)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSunshineStreamAddr(w http.ResponseWriter, r *http.Request) {
	var body sunshineStreamAddrBody
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	cfg, err := s.token.UpdateSunshineStreamAddr(body.Host, body.StreamPort)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handlePermsBool(fn func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, boolBody{Value: fn()})
	}
}

func (s *Server) handlePermsErr(fn func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	}
}

func (s *Server) handleTSStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.ts.Status(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleTSLogin(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	url, err := s.ts.StartLogin(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, loginBody{URL: url})
}

func (s *Server) handleTSLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.ts.Logout(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// handleTSAuthStream holds the connection open and pushes one line per
// AuthURL event (see broadcastAuthURL) for as long as the client stays
// connected — the RPC equivalent of tailscale.Service.SetAuthURLHandler,
// since a callback can't cross the socket.
func (s *Server) handleTSAuthStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan string, 4)
	id := s.subscribe(ch)
	defer s.unsubscribe(id)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case url := <-ch:
			if _, err := fmt.Fprintln(w, url); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) subscribe(ch chan string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSub
	s.nextSub++
	s.subs[id] = ch
	return id
}

func (s *Server) unsubscribe(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs, id)
}

func (s *Server) broadcastAuthURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- url:
		default:
			logrus.Warn("[adminapi] auth-stream subscriber too slow, dropping event")
		}
	}
}
