package controller

import "testing"

func TestMergeSavedConnections_RemoteWinsOnSharedKey(t *testing.T) {
	remote := []SavedConnection{{Name: "Office (renamed elsewhere)", Host: "10.0.0.5", MasterKey: "secret1"}}
	local := []SavedConnection{{Name: "Office", Host: "10.0.0.5", MasterKey: "secret1"}}

	merged, changed := mergeSavedConnections(remote, local)
	if changed {
		t.Fatal("nothing local-only was added, changed should be false")
	}
	if len(merged) != 1 || merged[0].Name != "Office (renamed elsewhere)" {
		t.Fatalf("expected remote's copy to win, got %+v", merged)
	}
}

func TestMergeSavedConnections_PreservesLocalOnlyEntries(t *testing.T) {
	remote := []SavedConnection{{Name: "Office", Host: "10.0.0.5", MasterKey: "secret1"}}
	local := []SavedConnection{
		{Name: "Office", Host: "10.0.0.5", MasterKey: "secret1"},
		{Name: "Home lab (never synced yet)", Host: "192.168.1.50", MasterKey: "secret2"},
	}

	merged, changed := mergeSavedConnections(remote, local)
	if !changed {
		t.Fatal("a local-only entry was added -- changed should be true")
	}
	if len(merged) != 2 {
		t.Fatalf("expected both entries preserved, got %d: %+v", len(merged), merged)
	}
	found := false
	for _, c := range merged {
		if c.Name == "Home lab (never synced yet)" {
			found = true
		}
	}
	if !found {
		t.Fatal("local-only entry was dropped instead of preserved")
	}
}

func TestMergeSavedConnections_HostMatchIsCaseInsensitive(t *testing.T) {
	remote := []SavedConnection{{Name: "Office", Host: "MyBox.local", MasterKey: "secret1"}}
	local := []SavedConnection{{Name: "Office (local name)", Host: "mybox.local", MasterKey: "secret1"}}

	merged, _ := mergeSavedConnections(remote, local)
	if len(merged) != 1 {
		t.Fatalf("expected the two entries to be treated as the same connection, got %d: %+v", len(merged), merged)
	}
}

func TestMergeSavedConnections_DifferentMasterKeySameHostIsTwoEntries(t *testing.T) {
	// Same address, different API secret -- e.g. the box was reset and
	// re-paired with a new master key -- must NOT be silently collapsed
	// into one entry (that would drop a real, distinct credential).
	remote := []SavedConnection{{Name: "Office (old key)", Host: "10.0.0.5", MasterKey: "secret1"}}
	local := []SavedConnection{{Name: "Office (new key)", Host: "10.0.0.5", MasterKey: "secret2"}}

	merged, changed := mergeSavedConnections(remote, local)
	if !changed {
		t.Fatal("a distinct local entry was added -- changed should be true")
	}
	if len(merged) != 2 {
		t.Fatalf("expected two distinct entries, got %d: %+v", len(merged), merged)
	}
}
