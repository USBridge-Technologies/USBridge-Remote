package moonlight

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
	AppVersion             string `xml:"appversion"`
	GfeVersion             string `xml:"GfeVersion"`
	PairStatus             int    `xml:"PairStatus"`
	ServerCodecModeSupport int    `xml:"ServerCodecModeSupport"`
	MacAddress             string `xml:"mac"`
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
	// Unique ID is a hex representation of the public key or a random string.
	// We'll use the first 16 bytes of the cert's hash as a consistent client ID.
	hash := ExtractPlainCert(identity.CertPEM)
	clientID := hex.EncodeToString(hash[:16])

	// Unauthenticated HTTP client
	httpClient := &http.Client{Timeout: 5 * time.Second}

	// Pairing HTTP client — Sunshine holds the /pair connection open until the host user enters the PIN,
	// so we need a much longer timeout to avoid timing out before the user can respond.
	pairingHTTPClient := &http.Client{Timeout: 120 * time.Second}

	// Authenticated HTTPS client
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{{Certificate: [][]byte{identity.Cert.Raw}, PrivateKey: identity.PrivateKey}},
		InsecureSkipVerify: true, // We don't verify the server's cert authority because it's self-signed
	}
	httpsClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
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
	// First try secure, fallback to insecure if we are not paired or host is fresh
	// Normally GameStream returns 401 on HTTPS if unpaired.
	url := c.getURL(true, "/serverinfo", nil)
	resp, err := c.httpsClient.Get(url)
	
	if err != nil || resp.StatusCode == 401 {
		url = c.getURL(false, "/serverinfo", nil)
		resp, err = c.httpClient.Get(url)
	}

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

type launchRoot struct {
	SessionUrl string `xml:"sessionUrl0"`
	Resume     int    `xml:"resume"`
}

// Launch starts an app (or resumes an already-running one) and returns the RTSP session URL and stream key.
func (c *Client) Launch(appId int, codecFormat string, width, height, fps, bitrate int) (string, []byte, error) {
	rikeyBytes := make([]byte, 16)
	if _, err := rand.Read(rikeyBytes); err != nil {
		return "", nil, err
	}
	rikey := hex.EncodeToString(rikeyBytes)

	sessionParams := map[string]string{
		"mode":               fmt.Sprintf("%dx%dx%d", width, height, fps),
		"additionalContentType": "1",
		"sops":               "0",
		"rikey":              rikey,
		"rikeyid":            "1",
		"gc":                 "1",
		"localAudioPlayMode": "2",
		"surroundAudioInfo":  "196610",
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
		resumeParams := make(map[string]string, len(sessionParams))
		for k, v := range sessionParams {
			resumeParams[k] = v
		}
		sessionUrl, err = c.doLaunchOrResume("/resume", resumeParams)
		if err != nil {
			return "", nil, fmt.Errorf("launch and resume both failed: %v", err)
		}
	}

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
