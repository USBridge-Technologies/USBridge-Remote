//go:build (windows || linux || darwin) && !android

package analytics

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
)

func TestMeasurementProtocolAcceptsPayload(t *testing.T) {
	if os.Getenv("USBRIDGE_ANALYTICS_CHECK") == "" {
		t.Skip("set USBRIDGE_ANALYTICS_CHECK=1 to validate the desktop payload")
	}
	raw, err := measurementBody("11111111-1111-4111-8111-111111111111", "1700000000", "windows", eventConnectionStarted,
		param{key: "protocol", value: "tailscale"},
	)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "https://www.google-analytics.com/debug/mp/collect?" + url.Values{
		"measurement_id": {measurementID},
		"api_secret":     {measurementSecret},
	}.Encode()
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var parsed struct {
		ValidationMessages []struct {
			Description string `json:"description"`
			FieldPath   string `json:"fieldPath"`
		} `json:"validationMessages"`
	}
	if err := json.Unmarshal(msg, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.ValidationMessages) > 0 {
		t.Fatalf("validation: %+v", parsed.ValidationMessages)
	}
}
