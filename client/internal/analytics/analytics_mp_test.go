package analytics

import (
	"encoding/json"
	"testing"
)

func TestMeasurementBodyKeepsEventNamesAndSkipsEmptyFields(t *testing.T) {
	raw, err := measurementBody("client-1", "1700000000", "windows", eventAddConnection,
		param{key: "source", value: "manual"},
		param{key: "protocol", value: ""},
	)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		ClientID string `json:"client_id"`
		Events   []struct {
			Name   string         `json:"name"`
			Params map[string]any `json:"params"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.ClientID != "client-1" || len(body.Events) != 1 {
		t.Fatalf("body = %+v", body)
	}
	event := body.Events[0]
	if event.Name != "add_connection" {
		t.Fatalf("name = %s", event.Name)
	}
	if event.Params["source"] != "manual" || event.Params["platform"] != "windows" {
		t.Fatalf("params = %+v", event.Params)
	}
	if _, ok := event.Params["protocol"]; ok {
		t.Fatal("empty protocol was included")
	}
	if event.Params["engagement_time_msec"] == nil || event.Params["session_id"] != "1700000000" {
		t.Fatalf("params = %+v", event.Params)
	}
}
