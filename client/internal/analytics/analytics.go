// Package analytics records product events. The call sites are shared.
// Android sends them through Firebase. Windows, Linux and macOS send the
// same event names to the desktop Analytics stream. Every other platform
// drops them. A call cannot fail a save or a connection and does not wait
// on the network.
package analytics

const (
	// SourceManual is a connection typed into the Add Connection dialog.
	SourceManual = "manual"
	// SourceQR is a connection saved from a scanned QR code.
	SourceQR = "qr"
	// SourceUSB is a connection saved by Over USB.
	SourceUSB = "usb"
	// SourceLink is a connection saved from a usbridge:// link.
	SourceLink = "link"

	eventAddConnection     = "add_connection"
	eventConnectionStarted = "connection_started"
)

type param struct {
	key   string
	value string
}

// AddConnection records that a person saved a new device.
// An empty source means this save was not a person adding a device
// (for example an automatic Tailscale-host remember) and is ignored.
func AddConnection(source, protocol string) {
	source = cleanToken(source)
	if source == "" {
		return
	}
	emit(eventAddConnection, param{key: "source", value: source}, param{key: "protocol", value: cleanToken(protocol)})
}

// ConnectionStarted records that a person reached a live session
// with a saved device. Automatic recovery does not call this.
func ConnectionStarted(protocol string) {
	emit(eventConnectionStarted, param{key: "protocol", value: cleanToken(protocol)}, param{})
}

// cleanToken keeps Firebase parameter values to a short token.
// Addresses, names and keys never belong in these fields.
func cleanToken(value string) string {
	out := make([]byte, 0, len(value))
	for i := 0; i < len(value) && len(out) < 40; i++ {
		c := value[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+'a'-'A')
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
			out = append(out, c)
		}
	}
	return string(out)
}
