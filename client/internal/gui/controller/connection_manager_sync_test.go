package controller

import "testing"

func TestMergeSavedConnections_MatchKeepsLocalName(t *testing.T) {
	remote := []SavedConnection{{Name: "Office (renamed elsewhere)", Host: "10.0.0.5", MasterKey: "secret1"}}
	local := []SavedConnection{{Name: "Office", Host: "10.0.0.5", MasterKey: "secret1"}}

	merged, changed := mergeSavedConnections(remote, local)
	if changed {
		t.Fatal("nothing local-only was added, changed should be false")
	}
	if len(merged) != 1 || merged[0].Name != "Office" {
		t.Fatalf("expected the local card to be kept, got %+v", merged)
	}
	if connectionOrigin(merged[0]) != connectionOriginLocal {
		t.Fatalf("shared key must stay local so logout keeps it, got origin %q", merged[0].Origin)
	}
}

func TestMergeSavedConnections_SameMasterKeyDifferentHostIsOneLocal(t *testing.T) {
	remote := []SavedConnection{{Name: "Office cloud", Host: "100.64.1.2", MasterKey: "secret1", TailscaleHost: "100.64.1.2"}}
	local := []SavedConnection{{Name: "Office", Host: "192.168.1.10", MasterKey: "secret1", InternalHost: "192.168.1.10"}}

	merged, changed := mergeSavedConnections(remote, local)
	if changed {
		t.Fatal("a matching local+cloud pair must collapse to one row")
	}
	if len(merged) != 1 {
		t.Fatalf("expected one row, got %d: %+v", len(merged), merged)
	}
	if connectionOrigin(merged[0]) != connectionOriginLocal {
		t.Fatalf("match must be local, got %+v", merged[0])
	}
	if merged[0].Name != "Office" {
		t.Fatalf("local name should be kept, got %q", merged[0].Name)
	}
	if merged[0].InternalHost != "192.168.1.10" {
		t.Fatalf("local LAN should be kept, got %+v", merged[0])
	}
	if merged[0].TailscaleHost != "100.64.1.2" {
		t.Fatalf("cloud TS host should fill in, got %+v", merged[0])
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
	for _, c := range merged {
		if connectionOrigin(c) != connectionOriginLocal {
			t.Fatalf("device-local rows must keep origin=local, got %+v", c)
		}
	}
}

func TestMergeSavedConnections_RemoteOnlyIsCloudOrigin(t *testing.T) {
	remote := []SavedConnection{
		{Name: "Office", Host: "10.0.0.5", MasterKey: "secret1"},
		{Name: "Account box", Host: "10.8.0.2", MasterKey: "secret-cloud"},
	}
	local := []SavedConnection{{Name: "Office", Host: "10.0.0.5", MasterKey: "secret1"}}

	merged, changed := mergeSavedConnections(remote, local)
	if changed {
		t.Fatal("nothing local-only was added, changed should be false")
	}
	if len(merged) != 2 {
		t.Fatalf("expected office + account box, got %+v", merged)
	}
	byName := map[string]SavedConnection{}
	for _, c := range merged {
		byName[c.Name] = c
	}
	if connectionOrigin(byName["Office"]) != connectionOriginLocal {
		t.Fatalf("shared office should stay local, got %+v", byName["Office"])
	}
	if connectionOrigin(byName["Account box"]) != connectionOriginCloud {
		t.Fatalf("remote-only row should be cloud overlay, got %+v", byName["Account box"])
	}
}

func TestLocalConnections_DropsCloudAndTreatsEmptyOriginAsLocal(t *testing.T) {
	got := localConnections([]SavedConnection{
		{Name: "pc", Host: "1.1.1.1", MasterKey: "a"},
		{Name: "cloud", Host: "2.2.2.2", MasterKey: "b", Origin: connectionOriginCloud},
		{Name: "legacy", Host: "3.3.3.3", MasterKey: "c"},
	})
	if len(got) != 2 {
		t.Fatalf("expected local+legacy, got %+v", got)
	}
	for _, c := range got {
		if c.Name == "cloud" {
			t.Fatal("cloud overlay leaked into the local subset")
		}
		if connectionOrigin(c) != connectionOriginLocal {
			t.Fatalf("local subset must be tagged local, got %+v", c)
		}
	}
}

func TestConnectionsForPush_OnlyCloudRowsWithoutOrigin(t *testing.T) {
	localPC := SavedConnection{Name: "pc", Host: "1.1.1.1", MasterKey: "a", Origin: connectionOriginLocal}
	overlay := SavedConnection{Name: "cloud", Host: "2.2.2.2", MasterKey: "b", Origin: connectionOriginCloud}
	conns := []SavedConnection{localPC, overlay}

	got := connectionsForPush(conns, nil, nil)
	if len(got) != 1 || got[0].Name != "cloud" {
		t.Fatalf("without blob keys, push payload must be overlay-only, got %+v", got)
	}
	if got[0].Origin != "" {
		t.Fatalf("origin must not go into the account blob, got %+v", got[0])
	}

	got = connectionsForPush(conns, map[string]bool{connectionSyncKey(localPC): true}, nil)
	if len(got) != 2 {
		t.Fatalf("locals in SyncedKeys must be pushed with overlay rows, got %+v", got)
	}
	for _, c := range got {
		if c.Origin != "" {
			t.Fatalf("origin must not go into the account blob, got %+v", c)
		}
	}
}

func TestConnectionOriginFromBadge(t *testing.T) {
	if connectionOriginFromBadge("Cloud") != connectionOriginCloud {
		t.Fatal("Cloud badge should map to cloud origin")
	}
	if connectionOriginFromBadge("cloud") != connectionOriginCloud {
		t.Fatal("raw origin value should map to cloud")
	}
	if connectionOriginFromBadge("Local") != connectionOriginLocal {
		t.Fatal("Local badge should map to local origin")
	}
}

func TestDefaultNewConnectionOrigin_LocalWithoutCredentials(t *testing.T) {
	cm := &ConnectionManager{Account: &AccountManager{}}
	if cm.defaultNewConnectionOrigin() != connectionOriginLocal {
		t.Fatal("without credentials, new connections must stay local")
	}
}

func TestAdoptRemoteConnections_TagsByPreviousLocal(t *testing.T) {
	remote := []SavedConnection{
		{Name: "shared", Host: "10.0.0.5", MasterKey: "k1"},
		{Name: "account-only", Host: "10.8.0.2", MasterKey: "k2"},
	}
	previous := []SavedConnection{{Name: "shared", Host: "10.0.0.5", MasterKey: "k1"}}
	got := adoptRemoteConnections(remote, previous, nil)
	if len(got) != 2 {
		t.Fatalf("expected both remote rows, got %+v", got)
	}
	byName := map[string]SavedConnection{}
	for _, c := range got {
		byName[c.Name] = c
	}
	if connectionOrigin(byName["shared"]) != connectionOriginLocal {
		t.Fatalf("previously-local key must stay local, got %+v", byName["shared"])
	}
	if connectionOrigin(byName["account-only"]) != connectionOriginCloud {
		t.Fatalf("remote-only key must be cloud, got %+v", byName["account-only"])
	}
}

func TestAdoptRemoteConnections_SkipsOptedOutRemoteOnly(t *testing.T) {
	remote := []SavedConnection{
		{Name: "deleted-here", Host: "1.1.1.1", MasterKey: "k1"},
		{Name: "still-cloud", Host: "2.2.2.2", MasterKey: "k2"},
	}
	optedOut := []string{connectionSyncKey(remote[0])}
	got := adoptRemoteConnections(remote, nil, optedOut)
	if len(got) != 1 || got[0].Name != "still-cloud" {
		t.Fatalf("opted-out row with no device copy must not overlay, got %+v", got)
	}
	if connectionOrigin(got[0]) != connectionOriginCloud {
		t.Fatalf("remaining remote-only row must stay cloud overlay, got %+v", got[0])
	}
}

func TestKeepUnsyncedLocals_KeepsNeverSyncedAndDropsPreviouslySynced(t *testing.T) {
	previous := []SavedConnection{
		{Name: "deleted-elsewhere", Host: "1.1.1.1", MasterKey: "k1"},
		{Name: "added-offline", Host: "192.168.0.9", MasterKey: "k3"},
	}
	adopted := []SavedConnection{{Name: "still-there", Host: "10.0.0.6", MasterKey: "k2", Origin: connectionOriginLocal}}
	synced := []string{
		connectionSyncKey(SavedConnection{Host: "1.1.1.1", MasterKey: "k1"}),
		connectionSyncKey(SavedConnection{Host: "10.0.0.6", MasterKey: "k2"}),
	}

	got, added := keepUnsyncedLocals(previous, adopted, synced, nil)
	if !added {
		t.Fatal("offline local should have been appended")
	}
	names := map[string]bool{}
	for _, c := range got {
		names[c.Name] = true
		if c.Name == "deleted-elsewhere" {
			t.Fatal("previously synced deletion was resurrected")
		}
	}
	if !names["still-there"] || !names["added-offline"] {
		t.Fatalf("expected still-there + added-offline, got %+v", got)
	}
}

func TestKeepUnsyncedLocals_EmptySyncedKeysKeepsLocals(t *testing.T) {
	previous := []SavedConnection{{Name: "mine", Host: "1.1.1.1", MasterKey: "k", Origin: connectionOriginLocal}}
	got, added := keepUnsyncedLocals(previous, nil, nil, nil)
	if !added || len(got) != 1 || got[0].Name != "mine" {
		t.Fatalf("empty synced key list must keep device locals, got %+v", got)
	}
}

func TestKeepUnsyncedLocals_LocalOnlySurvivesPreviouslySynced(t *testing.T) {
	previous := []SavedConnection{{Name: "opted-out", Host: "1.1.1.1", MasterKey: "k1"}}
	key := connectionSyncKey(previous[0])
	got, added := keepUnsyncedLocals(previous, nil, []string{key}, []string{key})
	if !added || len(got) != 1 || got[0].Name != "opted-out" {
		t.Fatalf("explicit Local must survive a missing remote row, got %+v", got)
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
