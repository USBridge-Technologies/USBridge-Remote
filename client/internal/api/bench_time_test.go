package api

import "testing"

// The agent answers with the bare object, no {success, data}: taking it for the wrapped
// form failed every reading, and no benchmark had latency figures.
func TestParseBenchTimeTakesTheAgentsBareResponse(t *testing.T) {
	us, err := parseBenchTime([]byte(`{"us":1794170000000000}` + "\n"))
	if err != nil || us != 1794170000000000 {
		t.Fatalf("%d %v", us, err)
	}
	for _, bad := range []string{`{}`, `{"success":false,"error":"nope"}`, `not json`} {
		if _, err := parseBenchTime([]byte(bad)); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}
