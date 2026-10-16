package analytics

import "encoding/json"

// measurementBody is one Measurement Protocol event. engagement_time_msec
// is required for the event to show up in Analytics reports. An empty
// parameter is left out so a missing protocol does not become a blank row.
func measurementBody(clientID, sessionID, platform, name string, fields ...param) ([]byte, error) {
	params := map[string]any{
		"platform":             platform,
		"session_id":           sessionID,
		"engagement_time_msec": 1,
	}
	for _, field := range fields {
		if field.key == "" || field.value == "" {
			continue
		}
		params[field.key] = field.value
	}
	return json.Marshal(struct {
		ClientID string `json:"client_id"`
		Events   []struct {
			Name   string         `json:"name"`
			Params map[string]any `json:"params"`
		} `json:"events"`
	}{
		ClientID: clientID,
		Events: []struct {
			Name   string         `json:"name"`
			Params map[string]any `json:"params"`
		}{{
			Name:   name,
			Params: params,
		}},
	})
}
