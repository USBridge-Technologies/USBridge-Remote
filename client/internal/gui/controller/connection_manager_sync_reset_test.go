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
	cm := &ConnectionManager{Account: am, connections: []SavedConnection{{Name: "office", Host: "10.0.0.5", MasterKey: "secret"}}}

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
	cm := &ConnectionManager{Account: am, connections: []SavedConnection{{Name: "home", Host: "1.2.3.4", MasterKey: "s"}}}

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
	cm := &ConnectionManager{Account: am}

	if err := cm.ResetSyncPassphrase(context.Background(), "whatever"); err == nil {
		t.Fatal("expected an error when not logged in, got nil")
	}
}
