package moonlight

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// Client is a Moonlight HTTP/HTTPS API client
type Client struct {
	Host      string
	Port      int
	HTTPSPort int
	Identity  *Identity
	ClientID  string

	httpClient        *http.Client
	httpsClient       *http.Client
	pairingHTTPClient *http.Client // long timeout — Sunshine holds the connection until the host user enters the PIN
}

type ServerInfo struct {
	// StatusCode is the root's status_code: a host answers an unpaired
	// client's HTTPS request with HTTP 200 and status_code 401 inside.
	StatusCode             int    `xml:"status_code,attr"`
	AppVersion             string `xml:"appversion"`
	GfeVersion             string `xml:"GfeVersion"`
	PairStatus             int    `xml:"PairStatus"`
	ServerCodecModeSupport int    `xml:"ServerCodecModeSupport"`
	MacAddress             string `xml:"mac"`
	// Apollo/Vibepollo's virtual display (SudoVDA on Windows), reported on
	// both the paired and the unpaired /serverinfo. Capable: the host can
	// stream a per-client virtual monitor; DriverReady: its driver is
	// installed and up right now. Both false on any other host.
	VirtualDisplayCapable     bool `xml:"VirtualDisplayCapable"`
	VirtualDisplayDriverReady bool `xml:"VirtualDisplayDriverReady"`
}

type App struct {
	AppTitle string `xml:"AppTitle"`
	ID       int    `xml:"ID"`
	IsHdr    int    `xml:"IsHdrSupported"`
}

type AppList struct {
	Apps []App `xml:"App"`
}

func NewClient(host string, port, httpsPort int, identity *Identity) *Client {
	if identity == nil {
		panic("moonlight.NewClient: identity must not be nil — LoadOrGenerateIdentity failed")
	}
	// The client ID is the first 16 bytes of the certificate's SHA-256 as hex: stable
	// for this identity and unique per client. It used to be the first 16 bytes of the
	// DER itself -- the same ASN.1 header for every certificate -- so every client
	// paired under one id, and Vibepollo refuses a pairing state with duplicate ids.
	hash := sha256.Sum256(ExtractPlainCert(identity.CertPEM))
	clientID := hex.EncodeToString(hash[:16])

	// Unauthenticated HTTP client
	httpClient := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(nil)},
	}

	// Pairing HTTP client — Sunshine holds the /pair connection open until the host user enters the PIN,
	// so we need a much longer timeout to avoid timing out before the user can respond.
	pairingHTTPClient := &http.Client{
		Timeout:   120 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(nil)},
	}

	// Authenticated HTTPS client
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{{Certificate: [][]byte{identity.Cert.Raw}, PrivateKey: identity.PrivateKey}},
		InsecureSkipVerify: true, // We don't verify the server's cert authority because it's self-signed
	}
	httpsClient := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(nil), TLSClientConfig: tlsConfig},
		Timeout:   10 * time.Second,
	}

	return &Client{
		Host:              host,
		Port:              port,
		HTTPSPort:         httpsPort,
		Identity:          identity,
		ClientID:          clientID,
		httpClient:        httpClient,
		httpsClient:       httpsClient,
		pairingHTTPClient: pairingHTTPClient,
	}
}

// SetDialTransport replaces the underlying transport on all HTTP clients with
// one that uses the provided dialer. Call this when Tailscale runs in userspace
// (Android) so connections to Tailscale IPs go through the Tailscale netstack
// instead of the default OS dialer which can't reach them.
func (c *Client) SetDialTransport(dialCtx func(ctx context.Context, network, addr string) (net.Conn, error)) {
	base := &http.Transport{Proxy: http.ProxyURL(nil), DialContext: dialCtx}
	tlsCfg := &tls.Config{
		Certificates:       []tls.Certificate{{Certificate: [][]byte{c.Identity.Cert.Raw}, PrivateKey: c.Identity.PrivateKey}},
		InsecureSkipVerify: true,
	}
	tlsBase := &http.Transport{Proxy: http.ProxyURL(nil), DialContext: dialCtx, TLSClientConfig: tlsCfg}

	c.httpClient = &http.Client{Transport: base, Timeout: 10 * time.Second}
	c.httpsClient = &http.Client{Transport: tlsBase, Timeout: 10 * time.Second}
	c.pairingHTTPClient = &http.Client{Transport: tlsBase, Timeout: 120 * time.Second}
}

func (c *Client) getURL(secure bool, path string, params map[string]string) string {
	scheme := "http"
	port := c.Port
	if secure {
		scheme = "https"
		port = c.HTTPSPort
	}

	u := url.URL{
		Scheme: scheme,
		Host:   fmt.Sprintf("%s:%d", c.Host, port),
		Path:   path,
	}

	q := u.Query()
	q.Set("uniqueid", c.ClientID)
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	return u.String()
}

func (c *Client) GetServerInfo() (*ServerInfo, error) {
	// Paired: the HTTPS answer (PairStatus 1, the MAC). Unpaired, a host
	// refuses the certificate -- some with HTTP 401, Sunshine's forks with
	// HTTP 200 and status_code 401 in the XML -- and the plain HTTP answer
	// is the one to read.
	if info, err := c.fetchServerInfo(c.httpsClient, c.getURL(true, "/serverinfo", nil)); err == nil && info.StatusCode != 401 {
		return info, nil
	}
	return c.fetchServerInfo(c.httpClient, c.getURL(false, "/serverinfo", nil))
}

func (c *Client) fetchServerInfo(client *http.Client, url string) (*ServerInfo, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch server info: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("serverinfo returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("📩 [Moonlight] ServerInfo Raw XML: %s", string(body))

	var info ServerInfo
	if err := xml.Unmarshal(body, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

func (c *Client) GetAppList() ([]App, error) {
	url := c.getURL(true, "/applist", nil)
	resp, err := c.httpsClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("applist returned status %d. Make sure you are paired", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	logrus.Debugf("📩 [Moonlight] AppList Raw XML: %s", string(body))
	var root struct {
		Apps []App `xml:"App"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, err
	}

	return root.Apps, nil
}

// displayCursor is the client's "Show Mouse" setting, sent on every
// /launch and /resume as usbridgeDisplayCursor so the host starts the
// session with its cursor drawn (1) or hidden (0) -- Sunshine (usbridge
// fork) and RustShine both honor it. 0 = never set: the parameter is left
// out and the host keeps its own default.
var displayCursor atomic.Int32

// SetDisplayCursor records the Show Mouse setting for the next launch.
func SetDisplayCursor(show bool) {
	if show {
		displayCursor.Store(2)
	} else {
		displayCursor.Store(1)
	}
}

// DisplayCursor reports the recorded Show Mouse setting; ok is false while
// it was never set. Read by the WebRTC path, which sends it in its offer.
func DisplayCursor() (show, ok bool) {
	switch displayCursor.Load() {
	case 1:
		return false, true
	case 2:
		return true, true
	}
	return false, false
}

type launchRoot struct {
	SessionUrl string `xml:"sessionUrl0"`
	Resume     int    `xml:"resume"`
}

// Launch starts an app (or resumes an already-running one) and returns the RTSP session URL and stream key.
//
// codecFormat is NOT sent to the server anywhere in this function -- there is
// no "codec" query param in the GameStream /launch or /resume protocol.
// Codec selection happens entirely via the RTSP handshake's
// supportedVideoFormats bitmask (see moonlight_service.go's
// moonlightVideoFormat/StartStream), which this HTTP call has no part in.
// codecFormat is accepted here purely so callers/logs can show what codec
// was intended alongside this HTTP request.
//
// extra holds further /launch and /resume parameters for this session, e.g.
// Apollo/Vibepollo's virtualDisplay (see MoonlightService.stockHostLaunchParams).
func (c *Client) Launch(appId int, codecFormat string, width, height, fps, bitrate int, extra map[string]string) (string, []byte, error) {
	logrus.Infof("🎯 [CODEC-TRACE] Client.Launch: appId=%d codecFormat=%q(unused by this HTTP call, see doc comment) mode=%dx%dx%d",
		appId, codecFormat, width, height, fps)
	rikeyBytes := make([]byte, 16)
	if _, err := rand.Read(rikeyBytes); err != nil {
		return "", nil, err
	}
	rikey := hex.EncodeToString(rikeyBytes)

	sessionParams := map[string]string{
		"mode":                  fmt.Sprintf("%dx%dx%d", width, height, fps),
		"additionalContentType": "1",
		"sops":                  "0",
		"rikey":                 rikey,
		"rikeyid":               "1",
		"gc":                    "1",
		"localAudioPlayMode":    "2",
		"surroundAudioInfo":     "196610",
	}
	switch displayCursor.Load() {
	case 1:
		sessionParams["usbridgeDisplayCursor"] = "0"
	case 2:
		sessionParams["usbridgeDisplayCursor"] = "1"
	}
	for k, v := range extra {
		sessionParams[k] = v
	}

	// Try /launch first; fall back to /resume if an app is already running.
	launchParams := make(map[string]string, len(sessionParams)+1)
	for k, v := range sessionParams {
		launchParams[k] = v
	}
	launchParams["appid"] = fmt.Sprintf("%d", appId)

	sessionUrl, err := c.doLaunchOrResume("/launch", launchParams)
	if err != nil {
		// "An app is already running" → try /resume instead.
		logrus.Warnf("🌕 /launch rejected (%v), trying /resume...", err)
		logrus.Warnf("🎯 [CODEC-TRACE] Client.Launch: FALLING BACK TO /resume -- /resume reattaches to whatever session is already running server-side and does NOT apply mode=%dx%dx%d or the intended codec=%q; if this fires right after a codec switch, that's the bug",
			width, height, fps, codecFormat)
		resumeParams := make(map[string]string, len(sessionParams))
		for k, v := range sessionParams {
			resumeParams[k] = v
		}
		sessionUrl, err = c.doLaunchOrResume("/resume", resumeParams)
		if err != nil {
			return "", nil, fmt.Errorf("launch and resume both failed: %v", err)
		}
		logrus.Infof("🎯 [CODEC-TRACE] Client.Launch: RESULT = /resume (codec=%q was NOT applied)", codecFormat)
		return sessionUrl, rikeyBytes, nil
	}

	logrus.Infof("🎯 [CODEC-TRACE] Client.Launch: RESULT = /launch (fresh session, codec=%q negotiation will proceed via RTSP next)", codecFormat)
	return sessionUrl, rikeyBytes, nil
}

func (c *Client) doLaunchOrResume(path string, params map[string]string) (string, error) {
	url := c.getURL(true, path, params)
	resp, err := c.httpsClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	logrus.Infof("📩 [Moonlight] %s HTTP %d raw: %s", path, resp.StatusCode, string(body))

	var root launchRoot
	if err := xml.Unmarshal(body, &root); err != nil {
		return "", err
	}

	if root.SessionUrl == "" {
		return "", fmt.Errorf("empty session URL (status_message in response body above)")
	}

	// Strip rtsp:// scheme — moonlight-common-c expects host:port only.
	return strings.TrimPrefix(root.SessionUrl, "rtsp://"), nil
}

// Quit terminates the currently-running app on Sunshine, resetting its session state.
// Call this before reconnecting after a failed connection so that the next Launch()
// gets a fresh session instead of resuming a potentially-corrupted one.
//
// The real endpoint is "/cancel", not "/quit" — confirmed against Sunshine's own
// source (nvhttp.cpp: https_server.resource["^/cancel$"]["GET"] = cancel). This
// had always been hitting "/quit" (a nonexistent route, 404) until Disconnect()
// started calling it too; cancel() ignores query params entirely (it just
// unconditionally terminates the running session), so appId is passed only for
// logging/back-compat, not because Sunshine reads it.
func (c *Client) Quit(appId int) error {
	logrus.Infof("🎯 [CODEC-TRACE] Client.Quit: sending /cancel appId=%d", appId)
	url := c.getURL(true, "/cancel", map[string]string{
		"appid": fmt.Sprintf("%d", appId),
	})
	resp, err := c.httpsClient.Get(url)
	if err != nil {
		logrus.Warnf("🎯 [CODEC-TRACE] Client.Quit: /cancel request failed: %v", err)
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	logrus.Infof("📩 [Moonlight] /cancel HTTP %d raw: %s", resp.StatusCode, string(body))
	logrus.Infof("🎯 [CODEC-TRACE] Client.Quit: /cancel done, HTTP %d", resp.StatusCode)
	return nil
}
