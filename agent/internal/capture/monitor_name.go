package capture

import (
	"strings"

	"usbridge_agent/internal/displaypower"
)

// monitorName is a monitor's EDID name, or "Display N" from its GDI name when it has
// none (a VM's display reports its GDI name, "\\.\DISPLAY1", as its name).
func monitorName(m displaypower.Monitor) string {
	if m.Name != "" && !strings.HasPrefix(m.Name, `\\.\`) {
		return m.Name
	}
	if n, ok := strings.CutPrefix(strings.ToUpper(m.GDIName), `\\.\DISPLAY`); ok && n != "" {
		return "Display " + n
	}
	if m.Name != "" {
		return m.Name
	}
	return "Display"
}
