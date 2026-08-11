package moonlightclient

import (
	"context"
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
	"time"
)

// nvhttpClient talks Sunshine's NvHTTP API (the classic GameStream HTTP/
// HTTPS control-plane API, on the "base" and "base-5" ports) against
// 127.0.0.1 only -- this package has no reason to ever talk to a remote
// host. Modeled on client/internal/api/moonlight's Client, trimmed to only
// what a loopback launcher needs (no applist/serverinfo polling loop, no
// SetDialTransport for tsnet).
type nvhttpClient struct {
	host      string
	httpPort  int
	httpsPort int
	id        *identity
	clientID  string

	plainClient   *http.Client // unauthenticated, for the pre-pairing /pair stages
	pairingClient *http.Client // like plainClient but a long timeout -- see below
	mtlsClient    *http.Client // client-cert authenticated, for /launch, /resume, /cancel
}

func newNVHTTPClient(host string, httpPort, httpsPort int, id *identity) *nvhttpClient {
	der := extractPlainCertDER(id.certPEM)
	clientID := hex.EncodeToString(der)
	if len(clientID) > 32 {
		clientID = clientID[:32]
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	dialCtx := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, addr)
	}

	plain := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(nil), DialContext: dialCtx},
	}
	// Sunshine holds a /pair request open server-side until it has a PIN to
	// check the request against (in our case, until Config.SubmitPIN's admin
	// API call arrives) -- a short timeout here would abort pairing before
	// SubmitPIN even runs. Mirrors client/internal/api/moonlight/client.go's
	// pairingHTTPClient.
	pairing := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(nil), DialContext: dialCtx},
	}
	mtls := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy:       http.ProxyURL(nil),
			DialContext: dialCtx,
			TLSClientConfig: &tls.Config{
				Certificates:       []tls.Certificate{{Certificate: [][]byte{id.cert.Raw}, PrivateKey: id.privateKey}},
				InsecureSkipVerify: true, //nolint:gosec // Sunshine's HTTPS cert is self-signed; pairing is what establishes trust here, not CA validation.
			},
		},
	}

	return &nvhttpClient{
		host: host, httpPort: httpPort, httpsPort: httpsPort,
		id: id, clientID: clientID,
		plainClient: plain, pairingClient: pairing, mtlsClient: mtls,
	}
}

func (c *nvhttpClient) buildURL(secure bool, path string, params map[string]string) string {
	scheme, port := "http", c.httpPort
	if secure {
		scheme, port = "https", c.httpsPort
	}
	u := url.URL{Scheme: scheme, Host: fmt.Sprintf("%s:%d", c.host, port), Path: path}
	q := u.Query()
	q.Set("uniqueid", c.clientID)
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// pairResponse is the XML body every /pair stage responds with.
type pairResponse struct {
	Paired            int    `xml:"paired"`
	PlainCert         string `xml:"plaincert"`
	ChallengeResponse string `xml:"challengeresponse"`
	PairingSecret     string `xml:"pairingsecret"`
	StatusMessage     string `xml:"statusmessage"`
}

func (c *nvhttpClient) pairStage(useLongTimeout bool, phrase string, params map[string]string, secure bool) (*pairResponse, error) {
	if params == nil {
		params = map[string]string{}
	}
	params["devicename"] = "usbridge"
	params["updateState"] = "1"
	params["phrase"] = phrase
	params["version"] = "7" // force SHA-256 (GFE gen7 salted-PIN scheme)

	client := c.plainClient
	if secure {
		client = c.mtlsClient
	} else if useLongTimeout {
		client = c.pairingClient
	}

	resp, err := client.Get(c.buildURL(secure, "/pair", params))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var root pairResponse
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("parse %s response: %w (body: %s)", phrase, err, body)
	}
	if root.Paired != 1 {
		return &root, fmt.Errorf("stage %q rejected: %s", phrase, root.StatusMessage)
	}
	return &root, nil
}

// pair runs the 5-stage NvHTTP salted-PIN pairing handshake described in
// moonlight-common-c / GFE's NvPairingManager (and reproduced practically in
// client/internal/api/moonlight/pairing.go, which this is adapted from):
//
//  1. getservercert   -- exchange certs (hex-encoded PEM), get Sunshine's salt
//  2. clientchallenge -- AES(salted-PIN-key) encrypted random challenge
//  3. serverchallengeresp -- SHA-256(serverChallenge||ourCertSig||ourSecret), AES-encrypted
//  4. clientpairingsecret -- our secret + RSA signature over it
//  5. pairchallenge (HTTPS, mutually authenticated with the cert from step 1)
//
// The PIN itself is never transmitted; both sides derive the same AES-128
// key from sha256(salt || pin) and prove knowledge of it by successfully
// encrypting/decrypting each other's challenges.
func (c *nvhttpClient) pair(ctx context.Context, pin string) error {
	salt := generateSalt()
	aesKey := deriveAESKey(salt, pin)

	// Stage 1: exchange certificates. This is the request Sunshine holds
	// open (server-side) waiting for a PIN, so it uses the long timeout.
	resp, err := c.pairStage(true, "getservercert", map[string]string{
		"salt":       hex.EncodeToString(salt),
		"clientcert": hex.EncodeToString(c.id.certPEM),
	}, false)
	if err != nil {
		return fmt.Errorf("stage 1 (getservercert): %w", err)
	}
	if _, err := hex.DecodeString(resp.PlainCert); err != nil || resp.PlainCert == "" {
		return fmt.Errorf("stage 1: invalid server cert in response")
	}

	// Stage 2: prove we know the PIN by encrypting a random challenge.
	clientChallenge := generateSalt()
	encChallenge, err := aes128ECBEncrypt(aesKey, clientChallenge)
	if err != nil {
		return err
	}
	resp, err = c.pairStage(false, "clientchallenge", map[string]string{
		"clientchallenge": hex.EncodeToString(encChallenge),
	}, false)
	if err != nil {
		return fmt.Errorf("stage 2 (clientchallenge): %w", err)
	}

	challengeRespData, err := hex.DecodeString(resp.ChallengeResponse)
	if err != nil {
		return fmt.Errorf("stage 2: invalid challengeresponse hex")
	}
	decrypted, err := aes128ECBDecrypt(aesKey, challengeRespData)
	if err != nil || len(decrypted) < 48 { // 32-byte hash + 16-byte server challenge
		return fmt.Errorf("stage 2: challenge response too short (wrong PIN?)")
	}
	serverChallenge := decrypted[32:48]

	// Stage 3: respond to the server's challenge with a hash that proves we
	// hold our own private key (ties our cert into the handshake) plus a
	// fresh secret we'll sign in stage 4.
	clientSecret := generateSalt()
	hashInput := append(append([]byte{}, serverChallenge...), c.id.cert.Signature...)
	hashInput = append(hashInput, clientSecret...)
	respHashArr := sha256.Sum256(hashInput)
	respHash := respHashArr[:]
	encHash, err := aes128ECBEncrypt(aesKey, respHash)
	if err != nil {
		return err
	}
	if _, err := c.pairStage(false, "serverchallengeresp", map[string]string{
		"serverchallengeresp": hex.EncodeToString(encHash),
	}, false); err != nil {
		return fmt.Errorf("stage 3 (serverchallengeresp, wrong PIN?): %w", err)
	}

	// Stage 4: send our secret plus an RSA signature over it, so Sunshine
	// can verify it came from the holder of the private key behind our cert.
	sig, err := signData(c.id.privateKey, clientSecret)
	if err != nil {
		return err
	}
	pairingSecret := append(append([]byte{}, clientSecret...), sig...)
	if _, err := c.pairStage(false, "clientpairingsecret", map[string]string{
		"clientpairingsecret": hex.EncodeToString(pairingSecret),
	}, false); err != nil {
		return fmt.Errorf("stage 4 (clientpairingsecret): %w", err)
	}

	// Stage 5: confirm pairing over HTTPS using the now-trusted client cert.
	if _, err := c.pairStage(false, "pairchallenge", nil, true); err != nil {
		return fmt.Errorf("stage 5 (pairchallenge): %w", err)
	}

	return nil
}

// serverInfo is the subset of /serverinfo's XML this package cares about.
type serverInfo struct {
	PairStatus             int    `xml:"PairStatus"`
	ServerCodecModeSupport int    `xml:"ServerCodecModeSupport"`
	AppVersion             string `xml:"appversion"`
}

func (c *nvhttpClient) getServerInfo(secure bool) (*serverInfo, error) {
	client := c.plainClient
	if secure {
		client = c.mtlsClient
	}
	resp, err := client.Get(c.buildURL(secure, "/serverinfo", nil))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var info serverInfo
	if err := xml.Unmarshal(body, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

type appEntry struct {
	Title string `xml:"AppTitle"`
	ID    int    `xml:"ID"`
}

// getAppList requires an already-paired mTLS connection.
func (c *nvhttpClient) getAppList() ([]appEntry, error) {
	resp, err := c.mtlsClient.Get(c.buildURL(true, "/applist", nil))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var root struct {
		Apps []appEntry `xml:"App"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	return root.Apps, nil
}

// launchResult carries what performRtspHandshake (moonlight-common-c) would
// call PSERVER_INFORMATION.rtspSessionUrl.
type launchResult struct {
	rtspHostPort string // host:port, "rtsp://" scheme already stripped
}

// launch starts appID on Sunshine (falling back to /resume if a session is
// already running -- mirrors client/internal/api/moonlight/client.go's
// Launch()/doLaunchOrResume(), which is the tested, known-good parameter set
// this package's production sibling already uses against this same Sunshine
// fork) and returns the RTSP session URL to hand to the RTSP handshake.
//
// rikey (16 bytes) becomes StreamConfig.remoteInputAesKey in
// moonlight-common-c terms: the AES-128-GCM key used for the ENet control
// channel's encrypted messages (see control.go). We generate it here, once,
// and it must be the exact same 16 bytes sent as "rikey" in this request.
func (c *nvhttpClient) launch(cfg Config, rikey []byte) (*launchResult, error) {
	params := map[string]string{
		"appid":                 fmt.Sprintf("%d", cfg.AppID),
		"mode":                  fmt.Sprintf("%dx%dx%d", cfg.Width, cfg.Height, cfg.FPS),
		"additionalContentType": "1",
		"sops":                  "0",
		"rikey":                 hex.EncodeToString(rikey),
		"rikeyid":               "1",
		"gc":                    "1",
		"localAudioPlayMode":    "2",
		"surroundAudioInfo":     "196610", // stereo (matches client.go's Launch default)
	}

	result, err := c.doLaunch("/launch", params)
	if err != nil {
		result, err = c.doLaunch("/resume", params)
		if err != nil {
			return nil, fmt.Errorf("both /launch and /resume failed: %w", err)
		}
	}
	return result, nil
}

func (c *nvhttpClient) doLaunch(path string, params map[string]string) (*launchResult, error) {
	resp, err := c.mtlsClient.Get(c.buildURL(true, path, params))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var root struct {
		SessionURL string `xml:"sessionUrl0"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("parse %s response: %w (body: %s)", path, err, body)
	}
	if root.SessionURL == "" {
		return nil, fmt.Errorf("%s: empty sessionUrl0 (body: %s)", path, body)
	}

	hostPort := strings.TrimPrefix(root.SessionURL, "rtsp://")
	hostPort = strings.TrimPrefix(hostPort, "rtspenc://")
	return &launchResult{rtspHostPort: hostPort}, nil
}

// cancel terminates whatever session Sunshine currently has running for us,
// via /cancel -- see client/internal/api/moonlight/client.go's Quit() doc
// comment for why it's "/cancel" and not the more obvious-looking "/quit".
func (c *nvhttpClient) cancel() error {
	resp, err := c.mtlsClient.Get(c.buildURL(true, "/cancel", nil))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
