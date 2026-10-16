//go:build darwin

package streamhost

import (
	"strconv"
	"testing"

	"usbridge_agent/internal/monitors"
)

// Runs against the machine's real displays: every agent index must be
// written as that display's CGDirectDisplayID and read back as the index.
func TestMacOutputNameRoundTrip(t *testing.T) {
	ids := monitors.ActiveDisplayIDs()
	if len(ids) == 0 {
		t.Skip("no active displays")
	}
	for i, id := range ids {
		idx := strconv.Itoa(i)
		got := macDisplayIDForIndex(idx)
		if got != strconv.FormatUint(uint64(id), 10) {
			t.Errorf("index %d written as %q, want display ID %d", i, got, id)
		}
		if back := macIndexForDisplayID(got); back != idx {
			t.Errorf("display ID %s read back as %q, want %q", got, back, idx)
		}
	}
	if got := macDisplayIDForIndex("virtual-ish"); got != "virtual-ish" {
		t.Errorf("non-index passed through as %q", got)
	}
}
