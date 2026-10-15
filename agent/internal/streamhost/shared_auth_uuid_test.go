package streamhost

import "testing"

func TestSunshineDeviceUUIDsAreUniqueUUIDs(t *testing.T) {
	// Three clients with the pre-3.0.122 id (their DER header), one with a real one, one
	// with none.
	header := "308202fd308201e5a003020102020818"
	clients := []trustedClient{
		{UniqueID: header, CertDER: []byte("a")},
		{UniqueID: header, CertDER: []byte("b")},
		{UniqueID: "B7013FEE-5CF3-D909-3592-E82C01B83220", CertDER: []byte("c")},
		{UniqueID: header, CertDER: []byte("d")},
		{CertDER: []byte("e")},
	}
	for i := range clients {
		clients[i].Fingerprint = fingerprintOf(clients[i].CertDER)
	}
	got := sunshineDeviceUUIDs(clients)
	seen := map[string]bool{}
	for i, u := range got {
		if !isCanonicalUUID(u) {
			t.Errorf("client %d: %q is not a UUID", i, u)
		}
		if seen[u] {
			t.Errorf("client %d: %q repeats", i, u)
		}
		seen[u] = true
	}
	if got[2] != "B7013FEE-5CF3-D909-3592-E82C01B83220" {
		t.Errorf("a unique id changed: %q", got[2])
	}
	if want := sunshineUUIDOut(clients[0].Fingerprint[:32]); got[0] != want {
		t.Errorf("duplicate id: got %q, want the fingerprint's %q", got[0], want)
	}
	// The same id once dashed and once bare is still a duplicate.
	same := []trustedClient{
		{UniqueID: "0123456789abcdef0123456789abcdef", CertDER: []byte("x")},
		{UniqueID: "01234567-89AB-CDEF-0123-456789ABCDEF", CertDER: []byte("y")},
	}
	for i := range same {
		same[i].Fingerprint = fingerprintOf(same[i].CertDER)
	}
	if u := sunshineDeviceUUIDs(same); u[0] == u[1] {
		t.Errorf("dashed and bare forms of one id both kept: %v", u)
	}
}

func TestSameClientIDIgnoresDashesAndCase(t *testing.T) {
	if !sameClientID("0123456789abcdef0123456789abcdef", "01234567-89AB-CDEF-0123-456789ABCDEF") {
		t.Error("dashed and bare forms differ")
	}
	if sameClientID("", "") || sameClientID("a", "b") {
		t.Error("empty or different ids match")
	}
}
