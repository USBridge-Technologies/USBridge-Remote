package adminapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/streamhost"
	"usbridge_agent/internal/tailscale"
)

// Client is a thin RPC client for Server. A single Client value satisfies
// all three interfaces internal/ui.Window needs (token, perms, ts) — it can
// be passed to ui.NewWindow in place of *app.App/*permissions.Service/
// *tailscale.Service.
type Client struct {
	httpClient *http.Client
	base       string

	mu          sync.Mutex
	authHandler func(string)
	streamStop  chan struct{}
}

// Dial connects to the admin socket at socketPath and confirms a server is
// actually listening (via /ping) before returning. A non-nil error means
// no instance is running there — the caller should proceed to start its
// own engine rather than treating this as fatal.
func Dial(socketPath string) (*Client, error) {
	c := &Client{
		base: "http://admin",
		// No client-wide Timeout: the long-lived /ts/auth-stream request
		// must be allowed to stay open indefinitely. Regular request/response
		// calls get their own bounded context in do() instead.
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					d := net.Dialer{Timeout: 2 * time.Second}
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/ping", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("admin socket ping: unexpected status %d", resp.StatusCode)
	}
	return c, nil
}

// Close stops the auth-URL stream goroutine (if started) and idle connections.
func (c *Client) Close() {
	c.mu.Lock()
	if c.streamStop != nil {
		close(c.streamStop)
		c.streamStop = nil
	}
	c.mu.Unlock()
	c.httpClient.CloseIdleConnections()
}

func (c *Client) do(method, path string, body any, out any) error {
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	// 90s covers the slowest calls (RequestAccessibility/RequestKMSCapture
	// wait on an interactive pkexec/polkit prompt) with headroom; ordinary
	// calls return in milliseconds.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var eb errorBody
		_ = json.NewDecoder(resp.Body).Decode(&eb)
		if eb.Error == "" {
			eb.Error = fmt.Sprintf("admin request failed: status %d", resp.StatusCode)
		}
		return fmt.Errorf("%s", eb.Error)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// CurrentConfig fetches the engine's live config — used once at GUI startup
// in place of the cfg a locally-owned App would already have in hand.
func (c *Client) CurrentConfig() (config.Config, error) {
	var cfg config.Config
	err := c.do(http.MethodGet, "/config", nil, &cfg)
	return cfg, err
}

// --- TokenBackend ---

func (c *Client) RegenerateMasterKey() (config.Config, error) {
	var cfg config.Config
	err := c.do(http.MethodPost, "/token/regenerate-master-key", nil, &cfg)
	return cfg, err
}

func (c *Client) SaveConfig(cfg config.Config) error {
	return c.do(http.MethodPost, "/token/save-config", cfg, nil)
}

func (c *Client) SunshineCaptureMode() string {
	var body captureModeBody
	_ = c.do(http.MethodGet, "/token/capture-mode", nil, &body)
	return body.Mode
}

func (c *Client) SetSunshineCaptureMode(mode string) error {
	return c.do(http.MethodPost, "/token/capture-mode", captureModeBody{Mode: mode}, nil)
}

func (c *Client) KMSCaptureGranted() bool {
	var body boolBody
	_ = c.do(http.MethodGet, "/token/kms-granted", nil, &body)
	return body.Value
}

func (c *Client) RequestKMSCapture() bool {
	var body boolBody
	_ = c.do(http.MethodPost, "/token/request-kms", nil, &body)
	return body.Value
}

func (c *Client) RestartSunshine() error {
	return c.do(http.MethodPost, "/token/restart-sunshine", nil, nil)
}

func (c *Client) ListSunshineClients() ([]streamhost.Client, error) {
	var clients []streamhost.Client
	err := c.do(http.MethodGet, "/token/clients", nil, &clients)
	return clients, err
}

func (c *Client) UnpairSunshineClient(uniqueID string) error {
	return c.do(http.MethodPost, "/token/unpair", unpairBody{UniqueID: uniqueID}, nil)
}

func (c *Client) SubmitMoonlightPIN(pin string) error {
	return c.do(http.MethodPost, "/token/pin", pinBody{PIN: pin}, nil)
}

func (c *Client) UpdateListenAddr(host string, port int) (config.Config, error) {
	var cfg config.Config
	err := c.do(http.MethodPost, "/token/listen-addr", listenAddrBody{Host: host, Port: port}, &cfg)
	return cfg, err
}

func (c *Client) UpdateSunshinePort(port int) (config.Config, error) {
	var cfg config.Config
	err := c.do(http.MethodPost, "/token/sunshine-port", sunshinePortBody{Port: port}, &cfg)
	return cfg, err
}

func (c *Client) UpdateSunshineStreamAddr(host string, streamPort int) (config.Config, error) {
	var cfg config.Config
	err := c.do(http.MethodPost, "/token/sunshine-stream-addr", sunshineStreamAddrBody{Host: host, StreamPort: streamPort}, &cfg)
	return cfg, err
}

// adminCredentials caches the streaming host's admin user/pass fetched over
// the socket, refreshed on every call — cheap, and the password can rotate
// across a Restart on the engine side.
func (c *Client) adminCredentials() adminCredentialsBody {
	var body adminCredentialsBody
	_ = c.do(http.MethodGet, "/token/admin-credentials", nil, &body)
	return body
}

func (c *Client) AdminUser() string { return c.adminCredentials().User }

func (c *Client) AdminPass() string { return c.adminCredentials().Pass }

func (c *Client) SunshineStreamHost() string {
	var body stringBody
	_ = c.do(http.MethodGet, "/token/sunshine-stream-host", nil, &body)
	return body.Value
}

func (c *Client) StreamerName() string {
	var body stringBody
	_ = c.do(http.MethodGet, "/token/streamer-name", nil, &body)
	return body.Value
}

// --- PermsBackend ---

func (c *Client) AccessibilityGranted() bool {
	var body boolBody
	_ = c.do(http.MethodGet, "/perms/accessibility", nil, &body)
	return body.Value
}

func (c *Client) ScreenRecordingGranted() bool {
	var body boolBody
	_ = c.do(http.MethodGet, "/perms/screen-recording", nil, &body)
	return body.Value
}

func (c *Client) RequestAccessibility() bool {
	var body boolBody
	_ = c.do(http.MethodPost, "/perms/request-accessibility", nil, &body)
	return body.Value
}

func (c *Client) RequestScreenRecording() bool {
	var body boolBody
	_ = c.do(http.MethodPost, "/perms/request-screen-recording", nil, &body)
	return body.Value
}

func (c *Client) OpenPrivacySettings() error {
	return c.do(http.MethodPost, "/perms/open-privacy-settings", nil, nil)
}

func (c *Client) OpenScreenRecordingSettings() error {
	return c.do(http.MethodPost, "/perms/open-screen-recording-settings", nil, nil)
}

// --- TSBackend ---

func (c *Client) Status(ctx context.Context) (*tailscale.Status, error) {
	var status tailscale.Status
	err := c.do(http.MethodGet, "/ts/status", nil, &status)
	if err != nil {
		return nil, err
	}
	return &status, nil
}

func (c *Client) StartLogin(ctx context.Context) (string, error) {
	var body loginBody
	err := c.do(http.MethodPost, "/ts/login", nil, &body)
	return body.URL, err
}

func (c *Client) Logout(ctx context.Context) error {
	return c.do(http.MethodPost, "/ts/logout", nil, nil)
}

// SetAuthURLHandler stores fn and, on first call, starts a background
// goroutine reading the server's /ts/auth-stream so fn still gets invoked
// for AuthURL events even though a callback can't cross the socket by
// itself — see Server.handleTSAuthStream.
func (c *Client) SetAuthURLHandler(fn func(string)) {
	c.mu.Lock()
	c.authHandler = fn
	alreadyStreaming := c.streamStop != nil
	if !alreadyStreaming {
		c.streamStop = make(chan struct{})
	}
	stop := c.streamStop
	c.mu.Unlock()

	if alreadyStreaming {
		return
	}
	go c.runAuthStream(stop)
}

func (c *Client) runAuthStream(stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		req, err := http.NewRequest(http.MethodGet, c.base+"/ts/auth-stream", nil)
		if err == nil {
			ctx, cancel := context.WithCancel(context.Background())
			req = req.WithContext(ctx)
			go func() {
				<-stop
				cancel()
			}()

			resp, err := c.httpClient.Do(req)
			if err == nil {
				scanner := bufio.NewScanner(resp.Body)
				for scanner.Scan() {
					line := scanner.Text()
					if line == "" {
						continue
					}
					c.mu.Lock()
					handler := c.authHandler
					c.mu.Unlock()
					if handler != nil {
						handler(line)
					}
				}
				resp.Body.Close()
			}
			cancel()
		}

		select {
		case <-stop:
			return
		case <-time.After(2 * time.Second):
		}
	}
}
