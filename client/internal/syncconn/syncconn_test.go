package syncconn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeriveKey_DeterministicAndSensitiveToBothInputs(t *testing.T) {
	k1 := DeriveKey("some.customer@gmail.com", "correct horse battery staple")
	k2 := DeriveKey("some.customer@gmail.com", "correct horse battery staple")
	if string(k1) != string(k2) {
		t.Fatal("DeriveKey is not deterministic for the same (email, passphrase)")
	}

	wrongPass := DeriveKey("some.customer@gmail.com", "a different passphrase")
	if string(k1) == string(wrongPass) {
		t.Fatal("DeriveKey ignored the passphrase")
	}

	wrongEmail := DeriveKey("someone.else@gmail.com", "correct horse battery staple")
	if string(k1) == string(wrongEmail) {
		t.Fatal("DeriveKey ignored the email -- two different accounts using the same passphrase would collide")
	}

	if len(k1) != 32 {
		t.Fatalf("DeriveKey returned %d bytes, want 32 (AES-256)", len(k1))
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := DeriveKey("a@b.com", "hunter2")
	plaintext := []byte(`[{"name":"office","host":"10.0.0.5","master_key":"secret"}]`)

	ciphertext, nonce, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ciphertext == "" || nonce == "" {
		t.Fatal("Encrypt returned an empty ciphertext or nonce")
	}

	got, err := Decrypt(key, ciphertext, nonce)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("Decrypt round-trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestDecrypt_WrongPassphraseFailsRatherThanProducingGarbage(t *testing.T) {
	rightKey := DeriveKey("a@b.com", "hunter2")
	wrongKey := DeriveKey("a@b.com", "hunter3")
	ciphertext, nonce, err := Encrypt(rightKey, []byte("secret data"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if _, err := Decrypt(wrongKey, ciphertext, nonce); err == nil {
		t.Fatal("Decrypt with the wrong key must fail (AES-GCM authentication), not silently return wrong plaintext")
	}
}

// fakeSyncServer mimics enough of usbridge-entitlement-backend's
// /v1/sync/:kind{,/meta} routes (see src/syncBlob.ts) for Pull/Push/Meta to
// be exercised end-to-end against something other than a live deployment.
func fakeSyncServer(t *testing.T) *httptest.Server {
	t.Helper()
	type record struct {
		Ciphertext string `json:"ciphertext"`
		Nonce      string `json:"nonce"`
		Version    int    `json:"version"`
		UpdatedAt  int64  `json:"updated_at"`
	}
	var stored *record

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sync/connections/meta", func(w http.ResponseWriter, r *http.Request) {
		if stored == nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 0, "updated_at": nil})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": stored.Version, "updated_at": stored.UpdatedAt})
	})
	mux.HandleFunc("GET /v1/sync/connections", func(w http.ResponseWriter, r *http.Request) {
		if stored == nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no synced data yet"})
			return
		}
		_ = json.NewEncoder(w).Encode(stored)
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
		stored = &record{Ciphertext: body.Ciphertext, Nonce: body.Nonce, Version: current + 1, UpdatedAt: 1}
		_ = json.NewEncoder(w).Encode(map[string]int{"version": stored.Version})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	prev := TestSetBackendBaseURL(srv.URL)
	t.Cleanup(func() { TestSetBackendBaseURL(prev) })
	return srv
}

func TestPull_NoDataYetReturnsErrNoData(t *testing.T) {
	fakeSyncServer(t)
	key := DeriveKey("a@b.com", "hunter2")
	_, _, err := Pull(context.Background(), "sometoken", "connections", key)
	if err != ErrNoData {
		t.Fatalf("Pull with nothing ever pushed = %v, want ErrNoData", err)
	}
}

func TestPushThenPull_RoundTrip(t *testing.T) {
	fakeSyncServer(t)
	key := DeriveKey("a@b.com", "hunter2")
	plaintext := []byte(`[{"name":"office"}]`)

	newVersion, err := Push(context.Background(), "sometoken", "connections", key, plaintext, 0)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if newVersion != 1 {
		t.Fatalf("Push returned version %d, want 1", newVersion)
	}

	got, version, err := Pull(context.Background(), "sometoken", "connections", key)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if version != 1 {
		t.Fatalf("Pull returned version %d, want 1", version)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("Pull round-trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestPush_StaleExpectedVersionIsAConflict(t *testing.T) {
	fakeSyncServer(t)
	key := DeriveKey("a@b.com", "hunter2")

	if _, err := Push(context.Background(), "tok", "connections", key, []byte("first"), 0); err != nil {
		t.Fatalf("first push: %v", err)
	}

	_, err := Push(context.Background(), "tok", "connections", key, []byte("stale write"), 0)
	if err == nil {
		t.Fatal("expected a conflict error, got nil")
	}
	var conflict *ErrConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected *ErrConflict, got %T: %v", err, err)
	}
	if conflict.Conflict == nil || conflict.Conflict.Version != 1 {
		t.Fatalf("conflict should carry the current (version 1) record, got %+v", conflict.Conflict)
	}
}
