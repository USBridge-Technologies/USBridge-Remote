package api

import (
	"encoding/json"
	"time"
)

// SetHostTextMode tells the agent how its USB broker brings the streamers'
// text in: "keys" (key presses, switching the host layout a character
// needs) or "clipboard" (pasted through the host clipboard). Older agents
// and hardware KVMs answer 404; text is then typed as keys there.
func (c *USBClient) SetHostTextMode(mode string) error {
	payload, _ := json.Marshal(map[string]string{"mode": mode})
	body, err := c.PostRawWithTimeout("/api/keyboard/text_mode", payload, 3*time.Second)
	var out struct {
		Mode string `json:"mode"`
	}
	return decodeAgentResponse(body, err, &out)
}
