package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"usbridge-client/internal/syncconn"

	"fyne.io/fyne/v2/test"
)

// newTestAccountManager builds an AccountManager for a test WITHOUT going
// through NewAccountManager's load() step -- fyne's test.App.Storage()
// resolves to the real, shared os.TempDir() (see fyne.io/fyne/v2/test's
// testStorage.RootURI), not an isolated per-call directory, so calling
// load() here would pick up an account.json a PRIOR test in this same
// process left behind. Constructing the struct directly (legal: this test
// file is in the same package) sidesteps that; t.Cleanup below removes
// whatever save() does write, so this test doesn't leave litter in the
// real system temp dir either.
func newTestAccountManager(t *testing.T) *AccountManager {
	t.Helper()
	am := &AccountManager{app: test.NewApp()}
	t.Cleanup(func() { _ = os.Remove(filepath.Join(os.TempDir(), "account.json")) })
	return am
}

// newTestConnectionManager builds a *ConnectionManager with just the
// fields ResetSyncPassphrase/setSyncVersion touch -- `app` is required
// since setSyncVersion persists via cm.app.Storage() (see
// connection_manager_sync.go's syncStateStorageURI). Same os.TempDir()
// caveat as newTestAccountManager above applies to the persisted
// connections_sync_state.json file, hence the matching cleanup.
func newTestConnectionManager(t *testing.T, am *AccountManager, connections []SavedConnection) *ConnectionManager {
	t.Helper()
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(os.TempDir(), "connections_sync_state.json"))
		_ = os.Remove(filepath.Join(os.TempDir(), "connections.json"))
	})
	return &ConnectionManager{app: test.NewApp(), Account: am, connections: connections}
}

func connectionNames(conns []SavedConnection) []string {
	names := make([]string, 0, len(conns))
	for _, c := range conns {
		names = append(names, c.Name)
	}
	return names
}

func findConnection(conns []SavedConnection, name string) (SavedConnection, bool) {
	for _, c := range conns {
		if c.Name == name {
			return c, true
		}
	}
	return SavedConnection{}, false
}

func serveEncryptedConnections(t *testing.T, key []byte, version int, conns []SavedConnection) *httptest.Server {
	t.Helper()
	plaintext, err := json.Marshal(conns)
	if err != nil {
		t.Fatalf("marshal remote connections: %v", err)
	}
	ciphertext, nonce, err := syncconn.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sync/connections", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ciphertext": ciphertext, "nonce": nonce, "version": version})
	})
	mux.HandleFunc("PUT /v1/sync/connections", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{"version": version + 1})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	prev := syncconn.TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() { syncconn.TestSetBackendBaseURL(prev) })
	return srv
}

func serveEmptySyncAccount(t *testing.T, key []byte, onPush func(conns []SavedConnection)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sync/connections", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("PUT /v1/sync/connections", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ciphertext string `json:"ciphertext"`
			Nonce      string `json:"nonce"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		plaintext, err := syncconn.Decrypt(key, body.Ciphertext, body.Nonce)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var conns []SavedConnection
		if err := json.Unmarshal(plaintext, &conns); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if onPush != nil {
			onPush(conns)
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"version": 1})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	prev := syncconn.TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() { syncconn.TestSetBackendBaseURL(prev) })
	return srv
}

// fakeSyncBackendWithSeed mirrors usbridge-entitlement-backend's
// /v1/sync/:kind{,/meta} routes closely enough to exercise
// ResetSyncPassphrase end-to-end (see syncconn's own test file for the
// near-identical original), plus a test-only endpoint to seed a
// pre-existing stored version without going through a real Push --
// simulating data left behind under a since-forgotten passphrase.
func fakeSyncBackendWithSeed(t *testing.T) *httptest.Server {
	t.Helper()
	type record struct {
		Ciphertext string `json:"ciphertext"`
		Nonce      string `json:"nonce"`
		Version    int    `json:"version"`
	}
	var stored *record

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sync/connections/meta", func(w http.ResponseWriter, r *http.Request) {
		v := 0
		if stored != nil {
			v = stored.Version
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": v, "updated_at": nil})
	})
	mux.HandleFunc("PUT /v1/sync/connections", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ciphertext      string `json:"ciphertext"`
			Nonce           string `json:"nonce"`
			ExpectedVersion int    `json:"expected_version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		current := 0
		if stored != nil {
			current = stored.Version
		}
		if body.ExpectedVersion != current {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"current": stored})
			return
		}
		stored = &record{Ciphertext: body.Ciphertext, Nonce: body.Nonce, Version: current + 1}
		_ = json.NewEncoder(w).Encode(map[string]int{"version": stored.Version})
	})
	mux.HandleFunc("POST /__test/seed", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Version int `json:"version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		stored = &record{Ciphertext: "b2xk", Nonce: "bg==", Version: body.Version}
	})
	mux.HandleFunc("GET /__test/version", func(w http.ResponseWriter, r *http.Request) {
		v := 0
		if stored != nil {
			v = stored.Version
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"version": v})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	prev := syncconn.TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() { syncconn.TestSetBackendBaseURL(prev) })
	return srv
}

func seedStoredVersion(t *testing.T, srv *httptest.Server, version int) {
	t.Helper()
	body, _ := json.Marshal(map[string]int{"version": version})
	resp, err := http.Post(srv.URL+"/__test/seed", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("seedStoredVersion: %v", err)
	}
	resp.Body.Close()
}

func currentStoredVersion(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	resp, err := http.Get(srv.URL + "/__test/version")
	if err != nil {
		t.Fatalf("currentStoredVersion: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Version int `json:"version"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.Version
}

func TestResetSyncPassphrase_SucceedsAndDerivesANewKey(t *testing.T) {
	fakeSyncBackendWithSeed(t)

	am := newTestAccountManager(t)
	am.email = "a@b.com"
	am.accountToken = "tok123"
	cm := newTestConnectionManager(t, am, []SavedConnection{{Name: "office", Host: "10.0.0.5", MasterKey: "secret"}})

	if err := cm.ResetSyncPassphrase(context.Background(), "brand-new-passphrase"); err != nil {
		t.Fatalf("ResetSyncPassphrase: %v", err)
	}
	if !am.HasSyncKey() {
		t.Fatal("expected a new sync key to be set locally after reset")
	}
	if _, _, ok := am.SyncCredentials(); !ok {
		t.Fatal("expected SyncCredentials to report configured after reset")
	}
}

func TestResetSyncPassphrase_OverwritesEvenWithPreExistingUnreadableData(t *testing.T) {
	srv := fakeSyncBackendWithSeed(t)

	am := newTestAccountManager(t)
	am.email = "a@b.com"
	am.accountToken = "tok123"
	cm := newTestConnectionManager(t, am, []SavedConnection{{Name: "home", Host: "1.2.3.4", MasterKey: "s"}})

	// Simulate a blob already present at version 5, encrypted under some
	// OTHER (now-forgotten) passphrase this test never derives -- Reset
	// must succeed anyway (it never tries to decrypt it, see
	// ResetSyncPassphrase's own doc comment on why Meta, not Pull, is what
	// makes that possible).
	seedStoredVersion(t, srv, 5)

	if err := cm.ResetSyncPassphrase(context.Background(), "a-different-new-passphrase"); err != nil {
		t.Fatalf("ResetSyncPassphrase over pre-existing data: %v", err)
	}
	if got := currentStoredVersion(t, srv); got != 6 {
		t.Fatalf("expected reset to overwrite at version 6 (was 5), server is now at %d", got)
	}
}

func TestResetSyncPassphrase_NotLoggedInFails(t *testing.T) {
	fakeSyncBackendWithSeed(t)
	am := newTestAccountManager(t)
	cm := newTestConnectionManager(t, am, nil)

	if err := cm.ResetSyncPassphrase(context.Background(), "whatever"); err == nil {
		t.Fatal("expected an error when not logged in, got nil")
	}
}

// TestTrySyncPullAndMerge_DoesNotResurrectADeletionOnceAlreadySynced is the
// regression test for the actual bug reported live: delete a synced
// connection, and it comes back. Root cause was mergeSavedConnections
// treating "present locally, absent from the just-pulled remote copy" as
// always meaning "not pushed yet, keep it" -- which is also exactly what a
// STALE local copy of something already deleted (and successfully pushed
// away) looks like. Once this device has completed a real sync before
// (see loadPersistedSyncVersion), trySyncPullAndMerge must trust remote
// outright instead of re-merging a stale local leftover back in (and,
// worse, pushing it right back up).
func TestTrySyncPullAndMerge_DoesNotResurrectADeletionOnceAlreadySynced(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@b.com"
	am.accountToken = "tok123"
	am.SetSyncPassphrase("test-pass-112233")
	_, key, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected sync credentials to be configured")
	}

	// Remote already reflects a successful deletion -- "111" is gone,
	// only "112" remains, at version 2 (some earlier push removed it).
	remotePlaintext, _ := json.Marshal([]SavedConnection{{Name: "112", Host: "10.0.0.6", MasterKey: "k2"}})
	ciphertext, nonce, err := syncconn.Encrypt(key, remotePlaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	pushCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sync/connections/meta", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": 2, "updated_at": nil})
	})
	mux.HandleFunc("GET /v1/sync/connections", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ciphertext": ciphertext, "nonce": nonce, "version": 2})
	})
	mux.HandleFunc("PUT /v1/sync/connections", func(w http.ResponseWriter, r *http.Request) {
		pushCalled = true
		_ = json.NewEncoder(w).Encode(map[string]int{"version": 3})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	prev := syncconn.TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() { syncconn.TestSetBackendBaseURL(prev) })

	// A STALE local copy that still has the already-deleted "111" --
	// standing in for whatever caused it to still be present (the
	// specific trigger matters less than the underlying bug: this state
	// is possible, and must not resurrect/repropagate "111").
	cm := newTestConnectionManager(t, am, []SavedConnection{
		{Name: "111", Host: "1.1.1.1", MasterKey: "k1"},
		{Name: "112", Host: "10.0.0.6", MasterKey: "k2"},
	})
	// Mark this device as having already completed a real sync with
	// this account -- persist both the version and the keys last seen in
	// the blob so keepUnsyncedLocals can tell a stale leftover (111)
	// from a connection that was never uploaded.
	cm.persistSyncState(2, []string{
		connectionSyncKey(SavedConnection{Host: "1.1.1.1", MasterKey: "k1"}),
		connectionSyncKey(SavedConnection{Host: "10.0.0.6", MasterKey: "k2"}),
	})

	cm.trySyncPullAndMerge()

	if len(cm.connections) != 1 || cm.connections[0].Name != "112" {
		t.Fatalf("expected only remote's \"112\" to survive, got %+v", cm.connections)
	}
	if connectionOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatalf("112 was local on this device, origin should stay local, got %+v", cm.connections[0])
	}
	if pushCalled {
		t.Fatal("adopting remote wholesale must not also push \"111\" back up to the server")
	}
}

func TestTrySyncPullAndMerge_LogoutDropsCloudOverlay(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	_, key, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected sync credentials")
	}

	serveEncryptedConnections(t, key, 1, []SavedConnection{
		{Name: "Account A box", Host: "10.8.0.2", MasterKey: "cloud-a"},
	})
	cm := newTestConnectionManager(t, am, []SavedConnection{
		{Name: "This PC", Host: "192.168.1.10", MasterKey: "local-k", Origin: connectionOriginLocal},
	})
	cm.trySyncPullAndMerge()

	if len(cm.connections) != 2 {
		t.Fatalf("expected local+cloud after pull A, got %v", connectionNames(cm.connections))
	}
	cloud, ok := findConnection(cm.connections, "Account A box")
	if !ok || connectionOrigin(cloud) != connectionOriginCloud {
		t.Fatalf("Account A box should be cloud overlay, got %+v", cm.connections)
	}
	local, ok := findConnection(cm.connections, "This PC")
	if !ok || connectionOrigin(local) != connectionOriginLocal {
		t.Fatalf("This PC should stay local, got %+v", cm.connections)
	}

	am.Logout()
	cm.trySyncPullAndMerge()

	if len(cm.connections) != 1 {
		t.Fatalf("logout should leave only local, got %v", connectionNames(cm.connections))
	}
	if cm.connections[0].Name != "This PC" || connectionOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatalf("expected This PC local, got %+v", cm.connections[0])
	}
	if _, stillThere := findConnection(cm.connections, "Account A box"); stillThere {
		t.Fatal("cloud overlay from A survived logout")
	}
}

func TestTrySyncPullAndMerge_MatchingCloudBecomesLocalAndSurvivesLogout(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	_, key, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected sync credentials")
	}

	serveEncryptedConnections(t, key, 1, []SavedConnection{
		{Name: "Office cloud", Host: "100.64.1.2", MasterKey: "same-key", TailscaleHost: "100.64.1.2"},
	})
	cm := newTestConnectionManager(t, am, []SavedConnection{
		{Name: "Office", Host: "192.168.1.10", MasterKey: "same-key", InternalHost: "192.168.1.10", Origin: connectionOriginLocal},
	})
	cm.trySyncPullAndMerge()

	if len(cm.connections) != 1 {
		t.Fatalf("matching local+cloud must be one row, got %v", connectionNames(cm.connections))
	}
	got := cm.connections[0]
	if connectionOrigin(got) != connectionOriginLocal || got.Name != "Office" {
		t.Fatalf("expected local Office, got %+v", got)
	}
	if cm.connectionDisplayOrigin(got) != connectionOriginCloud {
		t.Fatal("logged in and in the blob must show Cloud, not Local")
	}

	am.Logout()
	cm.trySyncPullAndMerge()
	if len(cm.connections) != 1 || cm.connections[0].Name != "Office" || connectionOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatalf("local Office must survive logout, got %+v", cm.connections)
	}
	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatal("logged out must show Local")
	}
}

func TestTrySyncPullAndMerge_CreatedWhileLoggedInShowsLocalAfterLogout(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	cm := newTestConnectionManager(t, am, []SavedConnection{
		{Name: "New box", Host: "192.168.1.20", MasterKey: "new-k", Origin: connectionOriginLocal},
	})
	cm.addBlobKey(connectionSyncKey(cm.connections[0]))

	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginCloud {
		t.Fatal("created while logged in must show Cloud")
	}
	if len(cm.pushPayload()) != 1 {
		t.Fatalf("created while logged in must be in the blob, got %+v", cm.pushPayload())
	}

	am.Logout()
	cm.trySyncPullAndMerge()
	if len(cm.connections) != 1 || cm.connections[0].Name != "New box" {
		t.Fatalf("device copy must survive logout, got %+v", cm.connections)
	}
	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatal("after logout the same row must show Local")
	}
}

func TestTrySyncPullAndMerge_OptedOutLocalSurvivesRelogin(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	_, key, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected sync credentials")
	}

	conn := SavedConnection{Name: "Office", Host: "192.168.1.10", MasterKey: "same-key", Origin: connectionOriginLocal}
	cm := newTestConnectionManager(t, am, []SavedConnection{conn})
	cm.persistSyncState(1, []string{connectionSyncKey(conn)})
	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginCloud {
		t.Fatal("in-blob row should show Cloud before opt-out")
	}

	cm.updateConnectionOrigin(0, connectionOriginLocal)
	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatal("opt-out must show Local while still logged in")
	}

	am.Logout()
	cm.trySyncPullAndMerge()
	if len(cm.connections) != 1 || cm.connections[0].Name != "Office" {
		t.Fatalf("opted-out row must survive logout, got %+v", cm.connections)
	}

	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	serveEncryptedConnections(t, key, 2, []SavedConnection{})
	cm.trySyncPullAndMerge()

	if len(cm.connections) != 1 || cm.connections[0].Name != "Office" {
		t.Fatalf("opted-out local must still be there after re-login, got %+v", cm.connections)
	}
	if connectionOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatalf("must stay a device copy, got %+v", cm.connections[0])
	}
	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatal("re-login must keep the Local badge, not drop the row")
	}
}

func TestTrySyncPullAndMerge_DeletedLocalOnlyDoesNotComeBackFromBlob(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	_, key, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected sync credentials")
	}

	a := SavedConnection{Name: "A", Host: "10.0.0.1", MasterKey: "ka", Origin: connectionOriginLocal}
	b := SavedConnection{Name: "B", Host: "10.0.0.2", MasterKey: "kb", Origin: connectionOriginLocal}
	cm := newTestConnectionManager(t, am, []SavedConnection{a, b})
	cm.persistSyncState(1, []string{connectionSyncKey(a), connectionSyncKey(b)})
	cm.updateConnectionOrigin(0, connectionOriginLocal)
	cm.updateConnectionOrigin(1, connectionOriginLocal)

	am.Logout()
	cm.trySyncPullAndMerge()
	cm.connections = nil
	cm.saveConnectionsLocalOnly()
	if len(cm.connections) != 0 {
		t.Fatalf("expected empty after delete, got %+v", cm.connections)
	}

	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	serveEncryptedConnections(t, key, 2, []SavedConnection{
		{Name: "A cloud", Host: "10.0.0.1", MasterKey: "ka"},
		{Name: "B cloud", Host: "10.0.0.2", MasterKey: "kb"},
	})
	cm.trySyncPullAndMerge()
	if len(cm.connections) != 0 {
		t.Fatalf("deleted opted-out rows must not return from the blob, got %+v", cm.connections)
	}
}

func TestTrySyncPullAndMerge_DeletedCloudDeviceCopyComesBackFromBlob(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	_, key, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected sync credentials")
	}

	conn := SavedConnection{Name: "Office", Host: "192.168.1.10", MasterKey: "cloud-k", Origin: connectionOriginLocal}
	cm := newTestConnectionManager(t, am, []SavedConnection{conn})
	cm.addBlobKey(connectionSyncKey(conn))
	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginCloud {
		t.Fatal("created-in-cloud row should show Cloud")
	}

	am.Logout()
	cm.trySyncPullAndMerge()
	cm.connections = nil
	cm.saveConnectionsLocalOnly()

	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	serveEncryptedConnections(t, key, 2, []SavedConnection{
		{Name: "Office", Host: "192.168.1.10", MasterKey: "cloud-k"},
	})
	cm.trySyncPullAndMerge()
	if len(cm.connections) != 1 || cm.connections[0].Name != "Office" {
		t.Fatalf("cloud copy must come back after deleting the logged-out device row, got %+v", cm.connections)
	}
	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginCloud {
		t.Fatal("restored row must show Cloud, not Local")
	}
}

func TestTrySyncPullAndMerge_EmptyAccountBDoesNotReceiveAccountA(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	_, keyA, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected sync credentials")
	}

	serveEncryptedConnections(t, keyA, 1, []SavedConnection{
		{Name: "Account A box", Host: "10.8.0.2", MasterKey: "cloud-a"},
	})
	cm := newTestConnectionManager(t, am, []SavedConnection{
		{Name: "This PC", Host: "192.168.1.10", MasterKey: "local-k"},
	})
	cm.trySyncPullAndMerge()

	am.Logout()
	cm.trySyncPullAndMerge()

	am.email = "b@example.com"
	am.accountToken = "tokB"
	am.SetSyncPassphrase("test-pass-112233")
	_, keyB, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected B sync credentials")
	}

	var pushed []SavedConnection
	serveEmptySyncAccount(t, keyB, func(conns []SavedConnection) { pushed = append([]SavedConnection(nil), conns...) })
	cm.trySyncPullAndMerge()
	cm.doSyncPush()

	if _, leaked := findConnection(cm.connections, "Account A box"); leaked {
		t.Fatalf("A's cloud connection leaked into B login: %v", connectionNames(cm.connections))
	}
	if len(cm.connections) != 1 || cm.connections[0].Name != "This PC" {
		t.Fatalf("B empty blob should leave device-local This PC, got %v", connectionNames(cm.connections))
	}
	if _, leaked := findConnection(pushed, "Account A box"); leaked {
		t.Fatalf("pushed A's cloud connections into B: %+v", pushed)
	}
	if _, seeded := findConnection(pushed, "This PC"); seeded {
		t.Fatalf("local-only This PC must not be seeded into account B: %+v", pushed)
	}
}

func TestDefaultNewConnectionOrigin_CloudWhenLoggedInWithPassphrase(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	cm := newTestConnectionManager(t, am, nil)
	if cm.defaultNewConnectionOrigin() != connectionOriginCloud {
		t.Fatal("logged-in + passphrase should default new connections to cloud")
	}
}

func TestDefaultNewConnectionOrigin_LocalWhenAutoSyncOff(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	cm := newTestConnectionManager(t, am, nil)
	cm.SetAutoSyncNewConnections(false)
	if cm.defaultNewConnectionOrigin() != connectionOriginLocal {
		t.Fatal("auto-sync off must keep new connections local")
	}
	if cm.AutoSyncNewConnections() {
		t.Fatal("preference should stay off")
	}
	cm.SetAutoSyncNewConnections(true)
	if cm.defaultNewConnectionOrigin() != connectionOriginCloud {
		t.Fatal("auto-sync on must default new connections to cloud")
	}
}

func TestUpdateConnectionOrigin_LocalLeavesTheBlob(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	cm := newTestConnectionManager(t, am, []SavedConnection{
		{Name: "box", Host: "1.1.1.1", MasterKey: "k", Origin: connectionOriginLocal},
	})

	cm.updateConnectionOrigin(0, connectionOriginCloud)
	if connectionOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatalf("device copy must stay local on disk, got %+v", cm.connections[0])
	}
	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginCloud {
		t.Fatal("logged in and uploaded must show Cloud")
	}
	if len(cm.pushPayload()) != 1 {
		t.Fatalf("cloud row should be in the blob, got %+v", cm.pushPayload())
	}

	cm.updateConnectionOrigin(0, connectionOriginLocal)
	if connectionOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatalf("expected local, got %+v", cm.connections[0])
	}
	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatal("never in the blob must show Local")
	}
	if len(cm.pushPayload()) != 0 {
		t.Fatalf("local row must not stay in the blob, got %+v", cm.pushPayload())
	}
}

func TestConnectionDisplayOrigin_NeverUploadedStaysLocal(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	cm := newTestConnectionManager(t, am, []SavedConnection{
		{Name: "box", Host: "1.1.1.1", MasterKey: "k", Origin: connectionOriginLocal},
	})
	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatal("logged in but never uploaded must still show Local")
	}
}

func TestUpdateConnectionOrigin_CloudRequiresSyncCredentials(t *testing.T) {
	cm := newTestConnectionManager(t, newTestAccountManager(t), []SavedConnection{
		{Name: "box", Host: "1.1.1.1", MasterKey: "k", Origin: connectionOriginLocal},
	})
	cm.updateConnectionOrigin(0, connectionOriginCloud)
	if connectionOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatal("cloud must be rejected when sync is not configured")
	}
}

func TestTrySyncPullAndMerge_EmailChangeDoesNotAdoptByPreviousAccountVersion(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	cm := newTestConnectionManager(t, am, []SavedConnection{
		{Name: "Home", Host: "192.168.1.10", MasterKey: "local-k"},
	})
	cm.persistSyncState(5, []string{connectionSyncKey(SavedConnection{Host: "10.0.0.5", MasterKey: "a-secret"})})
	if !cm.hasSyncedWithCurrentAccount() {
		t.Fatal("account A should look already-synced")
	}

	am.email = "b@example.com"
	am.accountToken = "tokB"
	am.SetSyncPassphrase("test-pass-112233")
	if cm.hasSyncedWithCurrentAccount() {
		t.Fatal("a different account must not inherit A's sync version")
	}
	_, key, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected B credentials")
	}

	serveEncryptedConnections(t, key, 5, []SavedConnection{
		{Name: "B office", Host: "10.9.0.1", MasterKey: "b-secret"},
	})
	cm.trySyncPullAndMerge()

	home, ok := findConnection(cm.connections, "Home")
	if !ok {
		t.Fatalf("first login to B must keep device-local Home, got %v", connectionNames(cm.connections))
	}
	if connectionOrigin(home) != connectionOriginLocal {
		t.Fatalf("Home should stay local, got %+v", home)
	}
	bOffice, ok := findConnection(cm.connections, "B office")
	if !ok {
		t.Fatalf("expected B's blob overlay, got %v", connectionNames(cm.connections))
	}
	if connectionOrigin(bOffice) != connectionOriginCloud {
		t.Fatalf("B office should be cloud, got %+v", bOffice)
	}
}

func TestSaveListEditPanel_CloudEditPushesToBlobImmediately(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	_, key, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected sync credentials")
	}

	var pushed []SavedConnection
	serveEmptySyncAccount(t, key, func(conns []SavedConnection) {
		pushed = append([]SavedConnection(nil), conns...)
	})

	conn := SavedConnection{
		Name:         "Office",
		Host:         "1.1.1.1",
		InternalHost: "1.1.1.1",
		MasterKey:    "k",
		Origin:       connectionOriginLocal,
	}
	cm := newTestConnectionManager(t, am, []SavedConnection{conn})
	cm.addBlobKey(connectionSyncKey(conn))

	if !cm.saveListEditPanel(0, "Office renamed", "1.1.1.1", "100.64.1.2", "k") {
		t.Fatal("save should succeed")
	}
	if cm.connections[0].Name != "Office renamed" {
		t.Fatalf("device copy should have the new name, got %+v", cm.connections[0])
	}
	if cm.connections[0].TailscaleHost != "100.64.1.2" {
		t.Fatalf("device copy should have the new TS host, got %+v", cm.connections[0])
	}
	if connectionOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatalf("edit must stay a device copy, got %+v", cm.connections[0])
	}
	if cm.connectionDisplayOrigin(cm.connections[0]) != connectionOriginCloud {
		t.Fatal("still-synced row must keep showing Cloud")
	}
	if len(pushed) != 1 || pushed[0].Name != "Office renamed" || pushed[0].TailscaleHost != "100.64.1.2" {
		t.Fatalf("account blob must get the edit immediately, got %+v", pushed)
	}
}

func TestSaveListEditPanel_CloudOverlayEditPersistsAndPushes(t *testing.T) {
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tokA"
	am.SetSyncPassphrase("test-pass-112233")
	_, key, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected sync credentials")
	}

	var pushed []SavedConnection
	serveEmptySyncAccount(t, key, func(conns []SavedConnection) {
		pushed = append([]SavedConnection(nil), conns...)
	})

	cm := newTestConnectionManager(t, am, []SavedConnection{
		{Name: "From other device", Host: "2.2.2.2", InternalHost: "2.2.2.2", MasterKey: "overlay-k", Origin: connectionOriginCloud},
	})

	if !cm.saveListEditPanel(0, "Renamed overlay", "2.2.2.2", "", "overlay-k") {
		t.Fatal("save should succeed")
	}
	if connectionOrigin(cm.connections[0]) != connectionOriginLocal {
		t.Fatalf("saving an overlay must write a device copy, got %+v", cm.connections[0])
	}
	if locals := localConnections(cm.connections); len(locals) != 1 || locals[0].Name != "Renamed overlay" {
		t.Fatalf("edited overlay must survive logout on disk, got %+v", locals)
	}
	if len(pushed) != 1 || pushed[0].Name != "Renamed overlay" {
		t.Fatalf("account blob must get the overlay edit, got %+v", pushed)
	}
}
