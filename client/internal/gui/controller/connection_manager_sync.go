package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"usbridge-client/internal/syncconn"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	"github.com/sirupsen/logrus"
)

// connectionsSyncKind is the "kind" segment usbridge-entitlement-backend's
// /v1/sync/:kind routes expect (see src/syncBlob.ts) -- a fixed allowlisted
// value there, "connections" being the only one that currently exists.
const connectionsSyncKind = "connections"

// syncPushDebounce collapses a burst of edits (typing a name, then a host,
// then a master key while adding one connection) into a single push --
// see this file's own doc comment on why requests need to stay cheap.
const syncPushDebounce = 3 * time.Second

// connectionSyncKey identifies "the same saved connection" across devices
// for merge purposes -- Host (SaveConnection's own fallbackText of
// InternalHost/TailscaleHost) plus MasterKey, lowercased: two devices that
// both saved the same physical USBridge box (same address, same API
// secret from its QR code) should merge into one entry, not two, even if
// they gave it different display Names.
func connectionSyncKey(c SavedConnection) string {
	return strings.ToLower(strings.TrimSpace(c.Host)) + "|" + c.MasterKey
}

// mergeSavedConnections combines a just-pulled remote list with this
// device's own local list -- remote wins on any key both share (another
// device's more recent edit, e.g. a renamed entry or a resolved Tailscale
// host, takes precedence over this device's possibly-stale copy), but a
// local-only entry (never yet synced from this device) is preserved
// rather than dropped. changed reports whether the result actually differs
// from remote alone, i.e. whether there's anything new to push back.
func mergeSavedConnections(remote, local []SavedConnection) (merged []SavedConnection, changed bool) {
	merged = make([]SavedConnection, 0, len(remote)+len(local))
	seen := make(map[string]bool, len(remote))
	for _, c := range remote {
		merged = append(merged, c)
		seen[connectionSyncKey(c)] = true
	}
	for _, c := range local {
		key := connectionSyncKey(c)
		if seen[key] {
			continue
		}
		merged = append(merged, c)
		seen[key] = true
		changed = true
	}
	return merged, changed
}

// syncStateFile persists JUST the last version this device is known to
// have fully synced to -- separate from connections.json (which stays a
// plain array on disk for backward compatibility with every existing
// install) and separate from account.json (that's the account/key, not
// per-kind sync bookkeeping; a future second synced "kind" would want its
// own version, not share this one). See trySyncPullAndMerge's doc comment
// for why THIS surviving an app restart is what actually fixes the
// resurrected-deletion bug it describes.
type syncStateFile struct {
	Version int `json:"version"`
}

func (cm *ConnectionManager) syncStateStorageURI() fyne.URI {
	uri, err := storage.Child(cm.app.Storage().RootURI(), "connections_sync_state.json")
	if err != nil {
		u, _ := url.Parse("file://connections_sync_state.json")
		return storage.NewFileURI(u.String())
	}
	return uri
}

// loadPersistedSyncVersion reads back whatever setSyncVersion last wrote --
// 0 if this device has never completed a real sync round-trip (a fresh
// install, or one that's never logged in/set a passphrase).
func (cm *ConnectionManager) loadPersistedSyncVersion() int {
	reader, err := storage.Reader(cm.syncStateStorageURI())
	if err != nil {
		return 0
	}
	defer reader.Close()
	var data []byte
	buf := make([]byte, 256)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	var state syncStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return 0
	}
	return state.Version
}

// setSyncVersion is the ONLY place cm.syncVersion should ever be written --
// every call site that used to assign it directly now goes through this,
// so the persisted copy (loadPersistedSyncVersion) can never drift from
// the in-memory one across an app restart.
func (cm *ConnectionManager) setSyncVersion(version int) {
	cm.syncMu.Lock()
	cm.syncVersion = version
	cm.syncMu.Unlock()

	data, err := json.Marshal(syncStateFile{Version: version})
	if err != nil {
		return
	}
	writer, err := storage.Writer(cm.syncStateStorageURI())
	if err != nil {
		logrus.Warnf("connection sync: could not persist sync version: %v", err)
		return
	}
	defer writer.Close()
	_, _ = writer.Write(data)
}

// trySyncPullAndMerge fetches this account's synced connections list (if
// sync is configured at all -- see AccountManager.SyncCredentials) and
// reconciles it with whatever's already saved locally. Called once at
// startup and again every time the account/passphrase state changes (see
// NewConnectionManager's AccountManager onChange callback) -- deliberately
// NOT on a recurring timer: nothing here polls the backend continuously,
// keeping this feature's request volume tied to actual human actions
// (logging in, setting a passphrase, editing the connections list) rather
// than a background loop.
//
// Whether this ADDITIVELY MERGES with the local list or simply ADOPTS the
// remote list wholesale depends on loadPersistedSyncVersion: additive
// merge only ever happens the FIRST time this device joins sync (no
// persisted version yet) -- appropriate there, since a fresh install
// joining an established account, or an existing device's pre-existing
// local-only list joining sync for the first time, both want their
// existing local entries preserved rather than discarded. Every
// SUBSEQUENT pull on an already-synced device instead treats remote as
// simply authoritative and replaces the local list outright. This is the
// fix for a real bug: additive merge has no way to tell "this device
// added something locally that hasn't been pushed yet" apart from "this
// device deleted something that's still sitting in a stale local copy" --
// both look identical (present locally, absent from remote). Once a
// device has completed one real sync, the second case is far more likely
// (a delete whose debounced push simply hadn't landed yet by the time
// this pull ran), and additive merge was resurrecting deleted entries
// right back onto the server on every subsequent pull -- forever, since
// nothing ever removed them again.
func (cm *ConnectionManager) trySyncPullAndMerge() {
	if cm.Account == nil {
		return
	}
	token, key, ok := cm.Account.SyncCredentials()
	if !ok {
		return
	}
	hasSyncedBefore := cm.loadPersistedSyncVersion() > 0

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	plaintext, version, err := syncconn.Pull(ctx, token, connectionsSyncKind, key)
	if err != nil {
		if errors.Is(err, syncconn.ErrNoData) {
			// Nothing pushed for this account yet -- seed it with whatever
			// this device already has locally (a harmless no-op if this
			// device also has nothing saved).
			cm.setSyncVersion(0)
			cm.scheduleSyncPush()
			return
		}
		logrus.Warnf("connection sync: pull failed: %v", err)
		cm.syncMu.Lock()
		cm.syncLastError = err.Error()
		cm.syncMu.Unlock()
		return
	}

	var remote []SavedConnection
	if err := json.Unmarshal(plaintext, &remote); err != nil {
		logrus.Errorf("connection sync: decrypted payload was not valid JSON: %v", err)
		return
	}

	cm.setSyncVersion(version)
	cm.syncMu.Lock()
	cm.syncLastError = ""
	cm.syncMu.Unlock()

	if hasSyncedBefore {
		cm.connections = remote
		fyne.Do(func() { cm.refreshConnectionsList() })
		cm.saveConnectionsLocalOnly()
		return
	}

	merged, changed := mergeSavedConnections(remote, cm.connections)
	cm.connections = merged
	fyne.Do(func() { cm.refreshConnectionsList() })
	// Persists the merged result locally; if this device contributed
	// local-only entries (changed == true) saveConnections's own hook
	// pushes them straight back up so every other device picks them up
	// too, without waiting for the next unrelated edit.
	if changed {
		cm.saveConnections()
	} else {
		cm.saveConnectionsLocalOnly()
	}
}

// scheduleSyncPush debounces a push after a local edit (see
// connection_manager_storage.go's saveConnections, which calls this) --
// no-ops instantly if sync isn't configured, so every existing call site
// stays free to call it unconditionally.
func (cm *ConnectionManager) scheduleSyncPush() {
	if cm.Account == nil {
		return
	}
	if _, _, ok := cm.Account.SyncCredentials(); !ok {
		return
	}

	cm.syncMu.Lock()
	defer cm.syncMu.Unlock()
	if cm.syncPushTimer != nil {
		cm.syncPushTimer.Stop()
	}
	cm.syncPushTimer = time.AfterFunc(syncPushDebounce, cm.doSyncPush)
}

func (cm *ConnectionManager) doSyncPush() {
	if cm.Account == nil {
		return
	}
	token, key, ok := cm.Account.SyncCredentials()
	if !ok {
		return
	}

	cm.syncMu.Lock()
	expectedVersion := cm.syncVersion
	cm.syncMu.Unlock()

	plaintext, err := json.Marshal(cm.connections)
	if err != nil {
		logrus.Errorf("connection sync: marshal failed: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	newVersion, err := syncconn.Push(ctx, token, connectionsSyncKind, key, plaintext, expectedVersion)
	if err != nil {
		var conflict *syncconn.ErrConflict
		if errors.As(err, &conflict) {
			logrus.Warnf("connection sync: version conflict (another device synced first) -- reconciling")
			cm.reconcileConflict(conflict)
			return
		}
		logrus.Warnf("connection sync: push failed: %v", err)
		cm.syncMu.Lock()
		cm.syncLastError = err.Error()
		cm.syncMu.Unlock()
		return
	}

	cm.setSyncVersion(newVersion)
	cm.syncMu.Lock()
	cm.syncLastError = ""
	cm.syncMu.Unlock()
}

// reconcileConflict merges the server's newer record (handed back by a 409
// response, see syncconn.ErrConflict) into this device's local list and
// re-persists -- which schedules exactly one retry push at the now-correct
// version via saveConnections's own hook. Two devices would have to keep
// racing each other on every single push to loop here indefinitely; a
// personal list of saved servers edited from a couple of devices is not
// going to do that in practice, so no bounded-retry machinery beyond this.
func (cm *ConnectionManager) reconcileConflict(conflict *syncconn.ErrConflict) {
	if conflict == nil || conflict.Conflict == nil {
		return
	}
	_, key, ok := cm.Account.SyncCredentials()
	if !ok {
		return
	}
	plaintext, err := syncconn.Decrypt(key, conflict.Conflict.Ciphertext, conflict.Conflict.Nonce)
	if err != nil {
		logrus.Warnf("connection sync: could not decrypt the server's conflicting copy (wrong passphrase?): %v", err)
		cm.syncMu.Lock()
		cm.syncLastError = "sync passphrase does not match this account's synced data"
		cm.syncMu.Unlock()
		return
	}
	var remote []SavedConnection
	if err := json.Unmarshal(plaintext, &remote); err != nil {
		return
	}

	merged, _ := mergeSavedConnections(remote, cm.connections)
	cm.connections = merged
	cm.setSyncVersion(conflict.Conflict.Version)
	fyne.Do(func() { cm.refreshConnectionsList() })
	cm.saveConnections()
}

// ResetSyncPassphrase is the "I forgot my sync passphrase" recovery path
// (see the account dialog's "Forgot passphrase? Reset it" button,
// main_window_account.go). Unlike the normal AccountManager.SetSyncPassphrase
// + trySyncPullAndMerge path (used when joining sync for the first time,
// including on a second device with the CORRECT existing passphrase --
// see that flow's own doc comment), this deliberately does NOT try to pull
// and merge whatever's already synced: by definition, if the passphrase is
// forgotten, nothing on this device can ever decrypt that old blob again,
// so it's permanently orphaned the moment a new passphrase is chosen. This
// makes that explicit and immediate -- this device's own current
// connections list overwrites the account's synced copy under the new
// key -- rather than leaving the account stuck in the permanent
// decrypt-conflict loop reconcileConflict would otherwise hit forever
// (every future pull/push failing against ciphertext nobody can open
// anymore).
//
// Meta (not Pull) is what makes this possible without the old passphrase
// at all: it only reports {version, updated_at}, never touching
// ciphertext, so the current version number needed for Push's
// optimistic-concurrency check is available regardless of which key (if
// any) can decrypt what's currently stored there.
func (cm *ConnectionManager) ResetSyncPassphrase(ctx context.Context, newPassphrase string) error {
	if cm.Account == nil {
		return fmt.Errorf("no account configured")
	}
	token, ok := cm.Account.AccountToken()
	if !ok {
		return fmt.Errorf("not logged in")
	}

	// Forget the old (now-useless) key before deriving the new one, so
	// nothing else briefly observes a half-updated state.
	cm.Account.ClearSyncKey()
	cm.Account.SetSyncPassphrase(newPassphrase)
	_, key, ok := cm.Account.SyncCredentials()
	if !ok {
		return fmt.Errorf("failed to derive the new sync key")
	}

	version, _, err := syncconn.Meta(ctx, token, connectionsSyncKind)
	if err != nil {
		return fmt.Errorf("could not check the current sync version: %w", err)
	}

	plaintext, err := json.Marshal(cm.connections)
	if err != nil {
		return err
	}
	newVersion, err := syncconn.Push(ctx, token, connectionsSyncKind, key, plaintext, version)
	if err != nil {
		// A conflict here would mean another device pushed between the
		// Meta call above and this one -- rare for a personal connections
		// list, and the human can just click "Reset" again to retry
		// against whatever version is current now.
		return fmt.Errorf("could not overwrite the synced data under the new passphrase: %w", err)
	}

	cm.setSyncVersion(newVersion)
	cm.syncMu.Lock()
	cm.syncLastError = ""
	cm.syncMu.Unlock()
	return nil
}
