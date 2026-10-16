//go:build (windows || linux || darwin) && !android

package analytics

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"github.com/google/uuid"
)

const (
	// Desktop stream created for Windows, Linux and macOS. The secret is
	// the Measurement Protocol key for that stream, the same kind of
	// client config the Android build keeps in google-services.json.
	measurementID     = "G-S2CB1XBMXD"
	measurementSecret = "5t54rqYjQHSZtsrdppPWSQ"
	clientIDPrefKey   = "analytics.client_id"
)

var (
	clientOnce sync.Once
	clientID   string
	sessionID  = strconv.FormatInt(time.Now().Unix(), 10)
	httpClient = &http.Client{Timeout: 4 * time.Second}
)

func emit(name string, a, b param) {
	go func() {
		defer func() { _ = recover() }()
		send(name, a, b)
	}()
}

func send(name string, fields ...param) {
	body, err := measurementBody(desktopClientID(), sessionID, runtime.GOOS, name, fields...)
	if err != nil || len(body) == 0 {
		return
	}
	endpoint := "https://www.google-analytics.com/mp/collect?" + url.Values{
		"measurement_id": {measurementID},
		"api_secret":     {measurementSecret},
	}.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

func desktopClientID() string {
	clientOnce.Do(func() {
		defer func() { _ = recover() }()
		clientID = loadOrCreateClientID()
	})
	if clientID == "" {
		clientID = uuid.NewString()
	}
	return clientID
}

func loadOrCreateClientID() string {
	app := fyne.CurrentApp()
	if app == nil || app.Preferences() == nil {
		return uuid.NewString()
	}
	if id := app.Preferences().String(clientIDPrefKey); id != "" {
		return id
	}
	id := uuid.NewString()
	app.Preferences().SetString(clientIDPrefKey, id)
	return id
}
