// Package syncconn implements zero-knowledge sync of the client's saved
// connections list (controller.SavedConnection, including each device's
// MasterKey -- a real credential) against usbridge-entitlement-backend's
// blob storage (see that repo's src/syncBlob.ts).
//
// "Zero-knowledge" here means specifically: the backend never sees, and
// this package never sends it, either the plaintext connections list OR
// the key that decrypts it. What actually ties a decryption key to an
// account is a SEPARATE secret from the account's Google login -- a sync
// passphrase the human sets on their first device and re-enters on every
// additional one (see DeriveKey) -- because Google login only proves an
// email address; it hands this client nothing the backend doesn't already
// know too, so it can't be the encryption secret without the backend
// trivially being able to decrypt everything it stores.
//
// Key derivation is fully local and deterministic: DeriveKey(email,
// passphrase) always produces the same 32-byte key for the same two
// inputs, on any device, with no network round trip and no salt to fetch
// or store server-side (the salt is itself derived from the email, which
// is not secret). AES-256-GCM then encrypts/decrypts the connections list
// with that key entirely client-side -- Push/Pull below only ever move the
// resulting ciphertext.
package syncconn

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

var backendBaseURL = "https://usbridge-entitlement.fatkulinamir80.workers.dev"

// TestSetBackendBaseURL points every call in this package at url for the
// duration of a test, and returns the previous value so the caller can
// restore it -- mirrors internal/account's own function of the same name.
func TestSetBackendBaseURL(url string) string {
	prev := backendBaseURL
	backendBaseURL = url
	return prev
}

// saltInfo is a fixed, non-secret domain-separation string -- part of why
// the derived salt is unique to (this app, this purpose, this account)
// rather than colliding with some other product's Argon2 usage of the same
// email, not a source of secrecy on its own (the salt's whole job is to
// stop precomputed rainbow-table attacks across many accounts, which it
// still does: it depends on email, so no two accounts share a salt).
const saltInfo = "usbridge-sync-connections-v1"

// DeriveKey turns (email, passphrase) into a 32-byte AES-256 key, fully
// locally and deterministically -- the same two inputs always yield the
// same key on any device, which is exactly what lets a second device join
// sync with nothing more than the human re-typing the same passphrase.
// Argon2id parameters (time=3, memory=64MiB, threads=4) follow the RFC
// 9106 "second recommended" browser/interactive profile -- deliberately
// not the more expensive "first recommended" one, since this runs
// synchronously on the GUI thread's goroutine and only needs to resist
// offline brute-forcing of a leaked ciphertext blob, not protect a
// server-side credential store.
func DeriveKey(email, passphrase string) []byte {
	salt := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email)) + ":" + saltInfo))
	return argon2.IDKey([]byte(passphrase), salt[:], 3, 64*1024, 4, 32)
}

// Encrypt seals plaintext under key with a fresh random nonce, returning
// both as standard base64 -- the shape usbridge-entitlement-backend's
// /v1/sync/connections PUT body expects (ciphertext, nonce).
func Encrypt(key, plaintext []byte) (ciphertextB64, nonceB64 string, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), base64.StdEncoding.EncodeToString(nonce), nil
}

// Decrypt is Encrypt's inverse. A wrong passphrase (hence wrong key)
// fails here with an authentication error, never silently produces
// garbage -- AES-GCM is authenticated, so this doubles as "is this the
// right passphrase" verification with no separate check needed.
func Decrypt(key []byte, ciphertextB64, nonceB64 string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("syncconn: bad ciphertext encoding: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return nil, fmt.Errorf("syncconn: bad nonce encoding: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("syncconn: decrypt failed (wrong passphrase, or data was tampered with): %w", err)
	}
	return plaintext, nil
}

// ErrNoData means the account has never pushed anything for this kind yet
// -- not an error condition for the caller, just "nothing to merge".
var ErrNoData = errors.New("syncconn: no synced data yet")

// ErrConflict is Push's result when the account token's expectedVersion no
// longer matches what the server has -- another device pushed since this
// caller last pulled. Conflict carries that newer server record
// (still-encrypted; the caller decrypts it with the same key) so the
// caller can merge and retry rather than losing data either direction.
type ErrConflict struct {
	Conflict *Blob
}

func (e *ErrConflict) Error() string { return "syncconn: version conflict -- pull and retry" }

// Blob is one account's encrypted connections list plus the version/kind
// bookkeeping the backend needs for optimistic concurrency (see
// usbridge-entitlement-backend's syncBlob.ts).
type Blob struct {
	Ciphertext string
	Nonce      string
	Version    int
	UpdatedAt  int64
}

const httpTimeout = 10 * time.Second

func httpClient() *http.Client { return &http.Client{Timeout: httpTimeout} }

// Meta fetches just {version, updated_at} for `kind` -- cheap enough to
// call before every full Pull to decide whether one is even needed (see
// this package's doc comment on why this matters: minimizing request
// volume/cost against the backend's KV storage).
func Meta(ctx context.Context, accountToken, kind string) (version int, updatedAt int64, err error) {
	var out struct {
		Version   int    `json:"version"`
		UpdatedAt *int64 `json:"updated_at"`
	}
	if err := doJSONWithStatus(ctx, http.MethodGet, "/v1/sync/"+kind+"/meta", nil, accountToken, &out); err != nil {
		return 0, 0, err
	}
	if out.UpdatedAt != nil {
		updatedAt = *out.UpdatedAt
	}
	return out.Version, updatedAt, nil
}

// Pull fetches and decrypts the current blob for `kind` -- ErrNoData if
// nothing has ever been pushed for this account/kind yet.
func Pull(ctx context.Context, accountToken, kind string, key []byte) (plaintext []byte, version int, err error) {
	var out struct {
		Ciphertext string `json:"ciphertext"`
		Nonce      string `json:"nonce"`
		Version    int    `json:"version"`
	}
	statusErr := doJSONWithStatus(ctx, http.MethodGet, "/v1/sync/"+kind, nil, accountToken, &out)
	if statusErr != nil {
		if statusErr.status == http.StatusNotFound {
			return nil, 0, ErrNoData
		}
		return nil, 0, statusErr
	}
	plaintext, err = Decrypt(key, out.Ciphertext, out.Nonce)
	if err != nil {
		return nil, 0, err
	}
	return plaintext, out.Version, nil
}

// Push encrypts plaintext and stores it for `kind`, iff expectedVersion
// (from the caller's last Meta/Pull) still matches what the server has --
// see ErrConflict for what happens otherwise. Returns the new version on
// success.
func Push(ctx context.Context, accountToken, kind string, key, plaintext []byte, expectedVersion int) (newVersion int, err error) {
	ciphertext, nonce, err := Encrypt(key, plaintext)
	if err != nil {
		return 0, err
	}
	reqBody, _ := json.Marshal(map[string]any{
		"ciphertext":       ciphertext,
		"nonce":            nonce,
		"expected_version": expectedVersion,
	})

	var out struct {
		Version int `json:"version"`
	}
	statusErr := doJSONWithStatus(ctx, http.MethodPut, "/v1/sync/"+kind, reqBody, accountToken, &out)
	if statusErr != nil {
		if statusErr.status == http.StatusConflict {
			var conflictBody struct {
				Current *struct {
					Ciphertext string `json:"ciphertext"`
					Nonce      string `json:"nonce"`
					Version    int    `json:"version"`
					UpdatedAt  int64  `json:"updated_at"`
				} `json:"current"`
			}
			_ = json.Unmarshal(statusErr.body, &conflictBody)
			var conflict *Blob
			if conflictBody.Current != nil {
				conflict = &Blob{
					Ciphertext: conflictBody.Current.Ciphertext,
					Nonce:      conflictBody.Current.Nonce,
					Version:    conflictBody.Current.Version,
					UpdatedAt:  conflictBody.Current.UpdatedAt,
				}
			}
			return 0, &ErrConflict{Conflict: conflict}
		}
		return 0, statusErr
	}
	return out.Version, nil
}

type httpStatusError struct {
	status int
	body   []byte
	path   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("syncconn: %s: HTTP %d: %s", e.path, e.status, truncate(e.body))
}

func doJSONWithStatus(ctx context.Context, method, path string, body []byte, bearer string, out any) *httpStatusError {
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, backendBaseURL+path, reader)
	if err != nil {
		return &httpStatusError{status: 0, body: []byte(err.Error()), path: path}
	}
	req.Header.Set("User-Agent", "usbridge-client-syncconn")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := httpClient().Do(req)
	if err != nil {
		return &httpStatusError{status: 0, body: []byte(err.Error()), path: path}
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return &httpStatusError{status: resp.StatusCode, body: []byte(err.Error()), path: path}
	}
	if resp.StatusCode != http.StatusOK {
		return &httpStatusError{status: resp.StatusCode, body: respBody, path: path}
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return &httpStatusError{status: resp.StatusCode, body: []byte(err.Error()), path: path}
		}
	}
	return nil
}

func truncate(b []byte) string {
	const max = 300
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
