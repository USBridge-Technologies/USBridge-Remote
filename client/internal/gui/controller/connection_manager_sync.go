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

const (
	connectionOriginLocal = "local"
	connectionOriginCloud = "cloud"
)

// syncPushDebounce collapses a burst of edits (typing a name, then a host,
// then a master key while adding one connection) into a single push --
// see this file's own doc comment on why requests need to stay cheap.
const syncPushDebounce = 3 * time.Second

// connectionSyncKey identifies "the same saved connection" across devices.
// The QR master key is the stable identity (LAN vs Tailscale host often
// differs between the local file and the account blob). Without a master
// key, fall back to a host string so empty-key rows still merge by address.
func connectionSyncKey(c SavedConnection) string {
	if mk := strings.TrimSpace(c.MasterKey); mk != "" {
		return "k|" + mk
	}
	host := strings.ToLower(strings.TrimSpace(c.Host))
	if host == "" {
		internal, ts := classifyConnectionHosts(c)
		host = strings.ToLower(fallbackText(internal, ts))
	}
	return "h|" + host
}

// coalesceSavedConnection is one card for a local+cloud match: keep the
// device row (survives logout) and fill in any empty addresses from the blob.
func coalesceSavedConnection(local, remote SavedConnection) SavedConnection {
	out := local
	if strings.TrimSpace(out.InternalHost) == "" {
		out.InternalHost = strings.TrimSpace(remote.InternalHost)
	}
	if strings.TrimSpace(out.TailscaleHost) == "" {
		out.TailscaleHost = strings.TrimSpace(remote.TailscaleHost)
	}
	if strings.TrimSpace(out.MasterKey) == "" {
		out.MasterKey = strings.TrimSpace(remote.MasterKey)
	}
	if strings.TrimSpace(out.RemoteOS) == "" {
		out.RemoteOS = strings.TrimSpace(remote.RemoteOS)
	}
	if strings.TrimSpace(out.RemoteProtocol) == "" {
		out.RemoteProtocol = strings.TrimSpace(remote.RemoteProtocol)
	}
	internal, ts := classifyConnectionHosts(out)
	if internal == "" && ts == "" {
		out.InternalHost, out.TailscaleHost = classifyConnectionHosts(remote)
	}
	out.Host = fallbackText(out.InternalHost, out.TailscaleHost, strings.TrimSpace(out.Host), strings.TrimSpace(remote.Host))
	return withOrigin(out, connectionOriginLocal)
}

func connectionOrigin(c SavedConnection) string {
	if strings.EqualFold(strings.TrimSpace(c.Origin), connectionOriginCloud) {
		return connectionOriginCloud
	}
	return connectionOriginLocal
}

func withOrigin(c SavedConnection, origin string) SavedConnection {
	c.Origin = origin
	return c
}

// localConnections is the subset that belongs on this device (and in
// connections.json). Cloud-pulled rows are dropped.
func localConnections(conns []SavedConnection) []SavedConnection {
	out := make([]SavedConnection, 0, len(conns))
	for _, c := range conns {
		if connectionOrigin(c) == connectionOriginCloud {
			continue
		}
		out = append(out, withOrigin(c, connectionOriginLocal))
	}
	return out
}

// connectionsForPush is the account blob: overlay-only cloud rows plus
// device rows whose keys are in blobKeys (explicitly uploaded / pulled).
// localOnly keys stay on this device even if they were previously in the
// blob. Origin is stripped so another device marks them as cloud on pull.
func connectionsForPush(conns []SavedConnection, blobKeys, localOnly map[string]bool) []SavedConnection {
	out := make([]SavedConnection, 0, len(conns))
	for _, c := range conns {
		key := connectionSyncKey(c)
		if localOnly[key] {
			continue
		}
		if connectionOrigin(c) != connectionOriginCloud && !blobKeys[key] {
			continue
		}
		cc := c
		cc.Origin = ""
		out = append(out, cc)
	}
	return out
}

func connectionSyncKeys(conns []SavedConnection) []string {
	keys := make([]string, 0, len(conns))
	seen := make(map[string]bool, len(conns))
	for _, c := range conns {
		k := connectionSyncKey(c)
		if seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	return keys
}

func syncedKeySet(keys []string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func appendUniqueKey(keys []string, key string) []string {
	if key == "" || syncedKeySet(keys)[key] {
		return keys
	}
	return append(append([]string(nil), keys...), key)
}

func removeKey(keys []string, key string) []string {
	if key == "" || len(keys) == 0 {
		return keys
	}
	next := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != key {
			next = append(next, k)
		}
	}
	return next
}

// mergeSavedConnections combines a just-pulled remote list with this
// device's own local list -- remote wins on any key both share (another
// device's more recent edit, e.g. a renamed entry or a resolved Tailscale
// host, takes precedence over this device's possibly-stale copy), but a
// local-only entry (never yet synced from this device) is preserved
// rather than dropped. Shared keys keep origin=local so they survive
// logout. changed reports whether the result actually differs
// from remote alone, i.e. whether there's anything new to push back.
func mergeSavedConnections(remote, local []SavedConnection) (merged []SavedConnection, changed bool) {
	localByKey := make(map[string]SavedConnection, len(local))
	for _, c := range local {
		if connectionOrigin(c) == connectionOriginCloud {
			continue
		}
		localByKey[connectionSyncKey(c)] = c
	}
	merged = make([]SavedConnection, 0, len(remote)+len(local))
	seen := make(map[string]bool, len(remote))
	for _, c := range remote {
		key := connectionSyncKey(c)
		seen[key] = true
		if loc, ok := localByKey[key]; ok {
			merged = append(merged, coalesceSavedConnection(loc, c))
		} else {
			merged = append(merged, withOrigin(c, connectionOriginCloud))
		}
	}
	for _, c := range local {
		if connectionOrigin(c) == connectionOriginCloud {
			continue
		}
		key := connectionSyncKey(c)
		if seen[key] {
			continue
		}
		merged = append(merged, withOrigin(c, connectionOriginLocal))
		seen[key] = true
		changed = true
	}
	return merged, changed
}

// adoptRemoteConnections replaces the in-memory list with remote, tagging
// origin=local only for keys this device already owned locally so logout
// keeps them. Remote-only rows stay cloud (memory overlay), except keys
// this device explicitly marked Local and then deleted -- those must not
// come back as an overlay on the next login.
func adoptRemoteConnections(remote, previousLocal []SavedConnection, localOnlyKeys []string) []SavedConnection {
	localByKey := make(map[string]SavedConnection, len(previousLocal))
	for _, c := range previousLocal {
		if connectionOrigin(c) == connectionOriginCloud {
			continue
		}
		localByKey[connectionSyncKey(c)] = c
	}
	localOnly := syncedKeySet(localOnlyKeys)
	out := make([]SavedConnection, 0, len(remote))
	for _, c := range remote {
		key := connectionSyncKey(c)
		if loc, ok := localByKey[key]; ok {
			out = append(out, coalesceSavedConnection(loc, c))
			continue
		}
		if localOnly[key] {
			continue
		}
		out = append(out, withOrigin(c, connectionOriginCloud))
	}
	return out
}

// keepUnsyncedLocals appends device-local rows that were never part of
// this account's last synced key set (created while logged out, or not
// yet pushed) and rows the user explicitly marked Local. Keys that were
// synced, not opted out, and missing from remote were deleted on another
// device and must not be resurrected. An empty syncedKeys list still
// keeps current locals -- that is "everything on this device was opted
// out of the blob", not "delete them all".
func keepUnsyncedLocals(previousLocal, adopted []SavedConnection, syncedKeys, localOnlyKeys []string) (out []SavedConnection, added bool) {
	seen := make(map[string]bool, len(adopted))
	for _, c := range adopted {
		seen[connectionSyncKey(c)] = true
	}
	previouslySynced := syncedKeySet(syncedKeys)
	localOnly := syncedKeySet(localOnlyKeys)
	out = append([]SavedConnection(nil), adopted...)
	for _, c := range previousLocal {
		if connectionOrigin(c) == connectionOriginCloud {
			continue
		}
		key := connectionSyncKey(c)
		if seen[key] {
			continue
		}
		if previouslySynced[key] && !localOnly[key] {
			continue
		}
		out = append(out, withOrigin(c, connectionOriginLocal))
		seen[key] = true
		added = true
	}
	return out, added
}

// syncStateFile persists the last version this device is known to have
// fully synced to for a given account -- separate from connections.json
// (which stays a plain array of local-only entries) and separate from
// account.json. Email-scoping stops account A's version from making a
// first login to B look like "already synced". SyncedKeys is the set of
// connectionSyncKey values last known to be in this account's blob, used
// to tell "new local, never uploaded" apart from "deleted remotely".
// LocalOnlyKeys is this device's explicit Local opt-out: those rows stay
// on disk across logout/login and are never pushed, even if they used to
// be in the blob.
type syncStateFile struct {
	AccountEmail  string   `json:"account_email,omitempty"`
	Version       int      `json:"version"`
	SyncedKeys    []string `json:"synced_keys,omitempty"`
	LocalOnlyKeys []string `json:"local_only_keys,omitempty"`
}

func (cm *ConnectionManager) syncStateStorageURI() fyne.URI {
	uri, err := storage.Child(cm.app.Storage().RootURI(), "connections_sync_state.json")
	if err != nil {
		u, _ := url.Parse("file://connections_sync_state.json")
		return storage.NewFileURI(u.String())
	}
	return uri
}

func (cm *ConnectionManager) accountEmail() string {
	if cm == nil || cm.Account == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(cm.Account.Email()))
}

func (cm *ConnectionManager) loadPersistedSyncState() syncStateFile {
	reader, err := storage.Reader(cm.syncStateStorageURI())
	if err != nil {
		return syncStateFile{}
	}
	defer reader.Close()
	var data []byte
	buf := make([]byte, 1024)
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
		return syncStateFile{}
	}
	return state
}

// loadPersistedSyncVersion is the version for the currently logged-in
// account, or 0 if this device has never completed a real sync round-trip
// with that account.
func (cm *ConnectionManager) loadPersistedSyncVersion() int {
	state := cm.loadPersistedSyncState()
	if !strings.EqualFold(strings.TrimSpace(state.AccountEmail), cm.accountEmail()) {
		return 0
	}
	return state.Version
}

func (cm *ConnectionManager) hasSyncedWithCurrentAccount() bool {
	return cm.accountEmail() != "" && cm.loadPersistedSyncVersion() > 0
}

// setSyncVersion updates the persisted version for the current account
// without changing SyncedKeys. persistSyncState is the write path for
// both in-memory cm.syncVersion and the on-disk file.
func (cm *ConnectionManager) setSyncVersion(version int) {
	cm.persistSyncState(version, cm.loadPersistedSyncState().SyncedKeys)
}

func (cm *ConnectionManager) persistSyncState(version int, keys []string) {
	cm.writeSyncState(version, keys, cm.preservedLocalOnlyKeys())
}

func (cm *ConnectionManager) preservedLocalOnlyKeys() []string {
	state := cm.loadPersistedSyncState()
	if cm.accountEmail() == "" || !strings.EqualFold(strings.TrimSpace(state.AccountEmail), cm.accountEmail()) {
		return nil
	}
	return append([]string(nil), state.LocalOnlyKeys...)
}

func (cm *ConnectionManager) writeSyncState(version int, keys, localOnly []string) {
	cm.syncMu.Lock()
	cm.syncVersion = version
	cm.syncMu.Unlock()

	data, err := json.Marshal(syncStateFile{
		AccountEmail:  cm.accountEmail(),
		Version:       version,
		SyncedKeys:    keys,
		LocalOnlyKeys: localOnly,
	})
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

func (cm *ConnectionManager) blobKeySet() map[string]bool {
	state := cm.loadPersistedSyncState()
	if cm.accountEmail() == "" || !strings.EqualFold(strings.TrimSpace(state.AccountEmail), cm.accountEmail()) {
		return map[string]bool{}
	}
	return syncedKeySet(state.SyncedKeys)
}

func (cm *ConnectionManager) localOnlyKeySet() map[string]bool {
	state := cm.loadPersistedSyncState()
	if cm.accountEmail() == "" || !strings.EqualFold(strings.TrimSpace(state.AccountEmail), cm.accountEmail()) {
		return map[string]bool{}
	}
	return syncedKeySet(state.LocalOnlyKeys)
}

func (cm *ConnectionManager) connectionDisplayOrigin(c SavedConnection) string {
	if !cm.canSyncConnections() {
		return connectionOriginLocal
	}
	if connectionOrigin(c) == connectionOriginCloud {
		return connectionOriginCloud
	}
	key := connectionSyncKey(c)
	if cm.localOnlyKeySet()[key] {
		return connectionOriginLocal
	}
	if cm.blobKeySet()[key] {
		return connectionOriginCloud
	}
	return connectionOriginLocal
}

func (cm *ConnectionManager) pushPayload() []SavedConnection {
	return connectionsForPush(cm.connections, cm.blobKeySet(), cm.localOnlyKeySet())
}

func (cm *ConnectionManager) addBlobKey(key string) {
	if key == "" || !cm.canSyncConnections() {
		return
	}
	state := cm.loadPersistedSyncState()
	keys := state.SyncedKeys
	localOnly := state.LocalOnlyKeys
	version := state.Version
	if !strings.EqualFold(strings.TrimSpace(state.AccountEmail), cm.accountEmail()) {
		keys = nil
		localOnly = nil
		version = 0
	}
	if syncedKeySet(keys)[key] && !syncedKeySet(localOnly)[key] {
		return
	}
	cm.writeSyncState(version, appendUniqueKey(keys, key), removeKey(localOnly, key))
}

func (cm *ConnectionManager) removeBlobKey(key string) {
	if key == "" || !cm.canSyncConnections() {
		return
	}
	state := cm.loadPersistedSyncState()
	keys := state.SyncedKeys
	localOnly := state.LocalOnlyKeys
	version := state.Version
	if !strings.EqualFold(strings.TrimSpace(state.AccountEmail), cm.accountEmail()) {
		keys = nil
		localOnly = nil
		version = 0
	}
	cm.writeSyncState(version, removeKey(keys, key), appendUniqueKey(localOnly, key))
}

// dropCloudConnections strips the account overlay so only this device's
// local connections remain -- the logout path. Cloud rows are not on
// disk; this also rewrites connections.json in case an older build leaked
// them there. Always refreshes the list: device copies stay, but the
// Local/Cloud badge must switch to Local once sync credentials are gone.
func (cm *ConnectionManager) dropCloudConnections() {
	locals := localConnections(cm.connections)
	if len(locals) != len(cm.connections) {
		cm.connections = locals
		cm.saveConnectionsLocalOnly()
	}
	fyne.Do(func() { cm.refreshConnectionsList() })
}

// trySyncPullAndMerge fetches this account's synced connections list (if
// sync is configured at all -- see AccountManager.SyncCredentials) and
// reconciles it with whatever's already saved locally. Called once at
// startup and again every time the account/passphrase state changes (see
// NewConnectionManager's AccountManager onChange callback). When
// credentials are missing (logout), the cloud overlay is dropped instead
// of leaving the previous account's rows in the list.
//
// Whether this ADDITIVELY MERGES with the local list or simply ADOPTS the
// remote list (plus never-uploaded locals) depends on hasSyncedWithCurrentAccount.
func (cm *ConnectionManager) trySyncPullAndMerge() {
	if cm.Account == nil {
		cm.dropCloudConnections()
		return
	}
	token, key, ok := cm.Account.SyncCredentials()
	if !ok {
		cm.dropCloudConnections()
		return
	}
	hasSyncedBefore := cm.hasSyncedWithCurrentAccount()
	state := cm.loadPersistedSyncState()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	plaintext, version, err := syncconn.Pull(ctx, token, connectionsSyncKind, key)
	if err != nil {
		if errors.Is(err, syncconn.ErrNoData) {
			// Nothing pushed for this account yet -- keep device-local
			// rows, drop any leftover overlay. Locals are not seeded into
			// an empty blob; the user opts a row into Cloud from the list.
			cm.connections = localConnections(cm.connections)
			fyne.Do(func() { cm.refreshConnectionsList() })
			cm.persistSyncState(0, nil)
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

	cm.syncMu.Lock()
	cm.syncLastError = ""
	cm.syncMu.Unlock()

	locals := localConnections(cm.connections)
	if hasSyncedBefore {
		adopted := adoptRemoteConnections(remote, locals, state.LocalOnlyKeys)
		adopted, _ = keepUnsyncedLocals(locals, adopted, state.SyncedKeys, state.LocalOnlyKeys)
		cm.connections = adopted
	} else {
		cm.connections, _ = mergeSavedConnections(remote, locals)
	}

	cm.persistSyncState(version, connectionSyncKeys(remote))
	fyne.Do(func() { cm.refreshConnectionsList() })
	cm.saveConnectionsLocalOnly()
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

// flushSyncPush sends any pending connections blob immediately -- used
// when a new connection is opted into Cloud, and just before logout so a
// 3s debounce cannot drop the upload after credentials are cleared.
func (cm *ConnectionManager) flushSyncPush() {
	if cm == nil || cm.Account == nil {
		return
	}
	if _, _, ok := cm.Account.SyncCredentials(); !ok {
		return
	}
	cm.syncMu.Lock()
	if cm.syncPushTimer != nil {
		cm.syncPushTimer.Stop()
		cm.syncPushTimer = nil
	}
	cm.syncMu.Unlock()
	cm.doSyncPush()
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

	payload := cm.pushPayload()
	plaintext, err := json.Marshal(payload)
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

	cm.persistSyncState(newVersion, connectionSyncKeys(payload))
	cm.syncMu.Lock()
	cm.syncLastError = ""
	cm.syncMu.Unlock()
}

// reconcileConflict merges the server's newer record (handed back by a 409
// response, see syncconn.ErrConflict) into this device's local list and
// re-persists -- which schedules exactly one retry push at the now-correct
// version via saveConnections's own hook.
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

	merged, _ := mergeSavedConnections(remote, localConnections(cm.connections))
	cm.connections = merged
	cm.persistSyncState(conflict.Conflict.Version, connectionSyncKeys(remote))
	fyne.Do(func() { cm.refreshConnectionsList() })
	cm.saveConnections()
}

// ResetSyncPassphrase is the "I forgot my sync passphrase" recovery path
// (see the account dialog's "Forgot passphrase? Reset it" button,
// main_window_account.go). Unlike the normal AccountManager.SetSyncPassphrase
// + trySyncPullAndMerge path, this deliberately does NOT try to pull
// and merge whatever's already synced.
func (cm *ConnectionManager) ResetSyncPassphrase(ctx context.Context, newPassphrase string) error {
	if cm.Account == nil {
		return fmt.Errorf("no account configured")
	}
	token, ok := cm.Account.AccountToken()
	if !ok {
		return fmt.Errorf("not logged in")
	}

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

	payload := cm.pushPayload()
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	newVersion, err := syncconn.Push(ctx, token, connectionsSyncKind, key, plaintext, version)
	if err != nil {
		return fmt.Errorf("could not overwrite the synced data under the new passphrase: %w", err)
	}

	cm.persistSyncState(newVersion, connectionSyncKeys(payload))
	cm.syncMu.Lock()
	cm.syncLastError = ""
	cm.syncMu.Unlock()
	return nil
}
