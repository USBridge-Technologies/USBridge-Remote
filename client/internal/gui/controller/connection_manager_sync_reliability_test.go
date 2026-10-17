package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"usbridge-client/internal/account"
	"usbridge-client/internal/syncconn"
)

// fakeSyncServer is a stateful stand-in for the backend's /v1/sync and
// /v1/account/token/refresh routes, with switches to make pushes fail and
// to reject tokens -- the failure modes that used to lose or resurrect rows.
type fakeSyncServer struct {
	t   *testing.T
	key []byte

	mu          sync.Mutex
	version     int
	ciphertext  string
	nonce       string
	failPush    bool
	validTokens map[string]bool
	refreshable map[string]string // old token -> new token
	pushes      int
}

func newFakeSyncServer(t *testing.T, key []byte, validToken string) *fakeSyncServer {
	t.Helper()
	f := &fakeSyncServer{t: t, key: key, validTokens: map[string]bool{validToken: true}, refreshable: map[string]string{}}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		ok := f.validTokens[tok]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"not authenticated"}`))
		}
		return ok
	}
	mux.HandleFunc("GET /v1/sync/connections/meta", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"version": f.version, "updated_at": nil})
	})
	mux.HandleFunc("GET /v1/sync/connections", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.version == 0 {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ciphertext": f.ciphertext, "nonce": f.nonce, "version": f.version})
	})
	mux.HandleFunc("PUT /v1/sync/connections", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		var body struct {
			Ciphertext      string `json:"ciphertext"`
			Nonce           string `json:"nonce"`
			ExpectedVersion int    `json:"expected_version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.failPush {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		if body.ExpectedVersion != f.version {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"current": map[string]any{"ciphertext": f.ciphertext, "nonce": f.nonce, "version": f.version}})
			return
		}
		f.version++
		f.ciphertext, f.nonce = body.Ciphertext, body.Nonce
		f.pushes++
		_ = json.NewEncoder(w).Encode(map[string]int{"version": f.version})
	})
	mux.HandleFunc("POST /v1/account/token/refresh", func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		defer f.mu.Unlock()
		next, ok := f.refreshable[tok]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"not authenticated"}`))
			return
		}
		f.validTokens[next] = true
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "a@example.com", "account_token": next, "expires_in": 2592000})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	prevSync := syncconn.TestSetBackendBaseURL(srv.URL)
	prevAcct := account.TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() {
		syncconn.TestSetBackendBaseURL(prevSync)
		account.TestSetBackendBaseURL(prevAcct)
	})
	return f
}

// seed stores conns as if another device had pushed them.
func (f *fakeSyncServer) seed(conns []SavedConnection) {
	f.t.Helper()
	plaintext, _ := json.Marshal(conns)
	ct, nonce, err := syncconn.Encrypt(f.key, plaintext)
	if err != nil {
		f.t.Fatal(err)
	}
	f.mu.Lock()
	f.version++
	f.ciphertext, f.nonce = ct, nonce
	f.mu.Unlock()
}

func (f *fakeSyncServer) stored() []SavedConnection {
	f.t.Helper()
	f.mu.Lock()
	ct, nonce, v := f.ciphertext, f.nonce, f.version
	f.mu.Unlock()
	if v == 0 {
		return nil
	}
	plaintext, err := syncconn.Decrypt(f.key, ct, nonce)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []SavedConnection
	_ = json.Unmarshal(plaintext, &out)
	return out
}

func (f *fakeSyncServer) setFailPush(on bool) {
	f.mu.Lock()
	f.failPush = on
	f.mu.Unlock()
}

func loggedInSyncPair(t *testing.T, conns []SavedConnection) (*AccountManager, *ConnectionManager, *fakeSyncServer) {
	t.Helper()
	am := newTestAccountManager(t)
	am.email = "a@example.com"
	am.accountToken = "tok1"
	am.SetSyncPassphrase("test-pass-112233")
	_, key, ok := am.SyncCredentials()
	if !ok {
		t.Fatal("expected sync credentials")
	}
	srv := newFakeSyncServer(t, key, "tok1")
	cm := newTestConnectionManager(t, am, conns)
	return am, cm, srv
}

// A connection added while the push fails must survive the next pull
// (it used to be recorded as synced, read as "deleted elsewhere" and
// dropped), and must reach the server on the next successful round.
func TestSyncReliability_FailedPushNewConnectionSurvivesPullAndUploadsLater(t *testing.T) {
	_, cm, srv := loggedInSyncPair(t, nil)
	srv.seed([]SavedConnection{{Name: "Old", Host: "10.0.0.1", MasterKey: "k-old"}})
	cm.trySyncPullAndMerge()
	if len(cm.connections) != 1 {
		t.Fatalf("expected the seeded row, got %+v", cm.connections)
	}

	srv.setFailPush(true)
	cm.SaveConnection("New", "10.0.0.2", "", "k-new", "auto", "", false, "")
	if got := cm.currentSyncState(); len(got.PendingAdds) != 1 {
		t.Fatalf("new row must be pending, state %+v", got)
	}

	// Another device pushes in the meantime; this device polls.
	srv.seed([]SavedConnection{{Name: "Old", Host: "10.0.0.1", MasterKey: "k-old"}, {Name: "Other", Host: "10.0.0.3", MasterKey: "k-other"}})
	cm.pollConnectionsSync()
	if _, ok := findConnection(cm.connections, "New"); !ok {
		t.Fatalf("unconfirmed new row was dropped by the pull: %+v", connectionNames(cm.connections))
	}
	if _, ok := findConnection(cm.connections, "Other"); !ok {
		t.Fatalf("remote addition missing: %+v", connectionNames(cm.connections))
	}

	srv.setFailPush(false)
	cm.pollConnectionsSync()
	names := connectionNames(srv.stored())
	if len(names) != 3 {
		t.Fatalf("server should now have Old, Other, New; got %v", names)
	}
	if st := cm.currentSyncState(); st.hasPending() {
		t.Fatalf("pending state should be clear after a successful push: %+v", st)
	}
}

// Deleting a Cloud row while the push fails must not resurrect it on the
// next pull, and the deletion must reach the server later.
func TestSyncReliability_FailedPushDeletionIsNotResurrected(t *testing.T) {
	_, cm, srv := loggedInSyncPair(t, nil)
	srv.seed([]SavedConnection{{Name: "Keep", Host: "10.0.0.1", MasterKey: "k1"}, {Name: "Gone", Host: "10.0.0.2", MasterKey: "k2"}})
	cm.trySyncPullAndMerge()

	srv.setFailPush(true)
	idx := -1
	for i, c := range cm.connections {
		if c.Name == "Gone" {
			idx = i
		}
	}
	deleted := cm.connections[idx]
	cm.connections = append(cm.connections[:idx], cm.connections[idx+1:]...)
	cm.noteConnectionDeleted(deleted)
	cm.saveConnections()
	cm.flushSyncPush()

	cm.trySyncPullAndMerge()
	if _, ok := findConnection(cm.connections, "Gone"); ok {
		t.Fatalf("deleted row came back from the server copy: %v", connectionNames(cm.connections))
	}

	srv.setFailPush(false)
	cm.pollConnectionsSync()
	if names := connectionNames(srv.stored()); len(names) != 1 || names[0] != "Keep" {
		t.Fatalf("deletion should have reached the server, got %v", names)
	}
}

// A row deleted on another device disappears here on the next poll.
func TestSyncReliability_RemoteDeletionPropagates(t *testing.T) {
	_, cm, srv := loggedInSyncPair(t, nil)
	srv.seed([]SavedConnection{{Name: "A", Host: "10.0.0.1", MasterKey: "k1"}, {Name: "B", Host: "10.0.0.2", MasterKey: "k2"}})
	cm.trySyncPullAndMerge()
	cm.SaveConnection("C", "10.0.0.3", "", "k3", "auto", "", false, "")
	if n := len(srv.stored()); n != 3 {
		t.Fatalf("expected 3 rows on server after add, got %d", n)
	}

	srv.seed([]SavedConnection{{Name: "C", Host: "10.0.0.3", MasterKey: "k3"}})
	cm.pollConnectionsSync()
	if names := connectionNames(cm.connections); len(names) != 1 || names[0] != "C" {
		t.Fatalf("remote deletions should propagate, got %v", names)
	}
}

// An expired token is renewed transparently and the sync goes through.
func TestSyncReliability_ExpiredTokenIsRefreshedAndRetried(t *testing.T) {
	am, cm, srv := loggedInSyncPair(t, nil)
	srv.seed([]SavedConnection{{Name: "A", Host: "10.0.0.1", MasterKey: "k1"}})
	srv.mu.Lock()
	delete(srv.validTokens, "tok1")
	srv.refreshable["tok1"] = "tok2"
	srv.mu.Unlock()

	cm.trySyncPullAndMerge()
	if len(cm.connections) != 1 {
		t.Fatalf("pull should succeed after refresh, got %+v", cm.connections)
	}
	if tok, _ := am.AccountToken(); tok != "tok2" {
		t.Fatalf("token should be replaced, got %q", tok)
	}
	if am.SessionExpired() || cm.SyncLastError() != "" {
		t.Fatalf("no error expected: expired=%v err=%q", am.SessionExpired(), cm.SyncLastError())
	}
}

// When refresh is refused too, the account says so instead of looking
// logged in while nothing syncs.
func TestSyncReliability_UnrefreshableTokenMarksSessionExpired(t *testing.T) {
	am, cm, srv := loggedInSyncPair(t, nil)
	srv.seed([]SavedConnection{{Name: "A", Host: "10.0.0.1", MasterKey: "k1"}})
	srv.mu.Lock()
	delete(srv.validTokens, "tok1")
	srv.mu.Unlock()

	cm.pollConnectionsSync()
	if !am.SessionExpired() {
		t.Fatal("session must be marked expired")
	}
	if am.LastError() == "" || cm.SyncLastError() == "" {
		t.Fatalf("errors must be surfaced: account=%q sync=%q", am.LastError(), cm.SyncLastError())
	}
}

// Two rows for the same agent (same master key, LAN + relay host) stay two
// rows, and a blob carrying repeated copies doesn't multiply them.
func TestSyncReliability_SharedMasterKeyRowsStayDistinctAndDeduped(t *testing.T) {
	lan := SavedConnection{Name: "LAN", Host: "192.168.200.15", InternalHost: "192.168.200.15", MasterKey: "shared", Origin: connectionOriginLocal}
	relay := SavedConnection{Name: "Relay", Host: "abc.device.usbridge.io", InternalHost: "abc.device.usbridge.io", MasterKey: "shared", Origin: connectionOriginLocal}
	_, cm, srv := loggedInSyncPair(t, []SavedConnection{lan, relay})
	srv.seed([]SavedConnection{lan, relay, lan, relay})
	cm.persistSyncState(1, []string{connectionSyncKey(lan)})

	cm.trySyncPullAndMerge()
	if names := connectionNames(cm.connections); len(names) != 2 {
		t.Fatalf("expected exactly LAN + Relay, got %v", names)
	}
	if _, ok := findConnection(cm.connections, "Relay"); !ok {
		t.Fatalf("relay row lost: %v", connectionNames(cm.connections))
	}

	// The relay row was never in the blob; it must survive a pull that
	// lacks it, because its key is still present.
	srv.seed([]SavedConnection{lan})
	cm.pollConnectionsSync()
	if _, ok := findConnection(cm.connections, "Relay"); !ok {
		t.Fatalf("sibling row sharing a key was dropped: %v", connectionNames(cm.connections))
	}
}

func TestDedupeSavedConnections_DropsExactRepeatsOnly(t *testing.T) {
	a := SavedConnection{Name: "Connection 1", Host: "x.device.usbridge.io", InternalHost: "x.device.usbridge.io", TailscaleHost: "100.1.1.1", MasterKey: "k"}
	b := SavedConnection{Name: "Connection 8", Host: "192.168.200.15", InternalHost: "192.168.200.15", MasterKey: "k"}
	got := dedupeSavedConnections([]SavedConnection{a, a, b, b})
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %v", connectionNames(got))
	}
}

func TestTokenExpiry_ParsesBackendTokenShape(t *testing.T) {
	exp, ok := account.TokenExpiry("ZmF0a3VsaW5hbWlyODBAZ21haWwuY29t.1791316262.sig")
	if !ok || exp.Unix() != 1791316262 {
		t.Fatalf("got %v %v", exp, ok)
	}
	if _, ok := account.TokenExpiry("garbage"); ok {
		t.Fatal("garbage must not parse")
	}
}
