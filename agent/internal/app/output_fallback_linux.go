//go:build linux

package app

import (
	"regexp"
	"strings"

	"usbridge_agent/internal/display"
)

var drmConnectorRe = regexp.MustCompile(`^(card\d+)-(.+)$`)

// disconnectedPinFallback handles a RustShine pin ("/dev/dri/cardN|CONNECTOR")
// whose connector is no longer connected in the kernel -- typically a vkms
// virtual monitor that was removed or forced off. The stream host's own device
// list keeps reporting such a connector, so the list-membership check in
// reconcileOutputName never notices, and capture then runs on nothing (black
// frames). Returns the first connected non-virtual output in the same
// "card|connector" form, or ok=false when the pin is fine or nothing usable
// is connected.
func disconnectedPinFallback(current string) (fallback string, ok bool) {
	card, conn, found := strings.Cut(current, "|")
	if !found {
		return "", false
	}
	cardName := card[strings.LastIndex(card, "/")+1:]
	connected := display.Connectors()
	var first string
	for _, c := range connected {
		m := drmConnectorRe.FindStringSubmatch(c.Name)
		if m == nil {
			continue
		}
		if m[1] == cardName && m[2] == conn {
			return "", false
		}
		if first == "" && !strings.HasPrefix(m[2], "Virtual-") {
			first = "/dev/dri/" + m[1] + "|" + m[2]
		}
	}
	if first == "" {
		return "", false
	}
	return first, true
}
