package hwid

import "testing"

func TestGet(t *testing.T) {
	id, err := Get()
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if len(id) != 64 {
		t.Fatalf("expected a 64-char hex sha256, got %d chars: %q", len(id), id)
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("id contains non-hex character %q: %q", c, id)
		}
	}
}

func TestGetIsStableAndCached(t *testing.T) {
	id1, err := Get()
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	id2, err := Get()
	if err != nil {
		t.Fatalf("Get() failed on second call: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("Get() returned different ids across calls: %q vs %q", id1, id2)
	}
}
