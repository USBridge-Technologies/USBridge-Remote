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

// syncPullPollInterval is how often a logged-in device checks the account
// blob for connections pushed from elsewhere. Without this, new/updated
// cloud rows only appear after app restart or an account-state change
// (login / passphrase / logout). Meta is cheap; a full Pull runs only when
// the remote version advanced. One minute is enough while the connections
// page is open without hammering the backend.
const syncPullPollInterval = time.Minute

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

// connectionAddresses is every non-empty host a row can be reached at,
// lowercased -- used to tell two rows that share a master key apart.
func connectionAddresses(c SavedConnection) []string {
	var out []string
	for _, h := range []string{c.Host, c.InternalHost, c.TailscaleHost} {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			out = append(out, h)
		}
	}
	return out
}

func connectionsShareAddress(a, b SavedConnection) bool {
	aa := connectionAddresses(a)
	for _, hb := range connectionAddresses(b) {
		for _, ha := range aa {
			if ha == hb {
				return true
			}
		}
	}
	return false
}

// localMatcher pairs remote rows with this device's rows one-to-one. The
// sync key is the master key, and one agent can legitimately be saved
// twice (LAN IP and *.device.usbridge.io, say). Matching by key alone
// paired both remote rows with the same local one, turning two distinct
// rows into two copies of one -- and each round trip pushed the copies.
type localMatcher struct {
	locals []SavedConnection
	byKey  map[string][]int
	used   []bool
}

func newLocalMatcher(locals []SavedConnection) *localMatcher {
	m := &localMatcher{byKey: map[string][]int{}}
	for _, c := range locals {
		if connectionOrigin(c) == connectionOriginCloud {
			continue
		}
		m.byKey[connectionSyncKey(c)] = append(m.byKey[connectionSyncKey(c)], len(m.locals))
		m.locals = append(m.locals, c)
	}
	m.used = make([]bool, len(m.locals))
	return m
}

// match returns the local row index for remote (-1: none), or dup=true when
// remote is a repeat of a row already matched (same key, same address).
func (m *localMatcher) match(remote SavedConnection) (idx int, dup bool) {
	cands := m.byKey[connectionSyncKey(remote)]
	for _, i := range cands {
		if !m.used[i] && connectionsShareAddress(m.locals[i], remote) {
			m.used[i] = true
			return i, false
		}
	}
	for _, i := range cands {
		if m.used[i] && connectionsShareAddress(m.locals[i], remote) {
			return -1, true
		}
	}
	for _, i := range cands {
		if !m.used[i] {
			m.used[i] = true
			return i, false
		}
	}
	return -1, false
}

// overlaySeen drops exact repeats among remote-only rows (same key, same
// address) -- a blob polluted by older builds carries such copies.
type overlaySeen []SavedConnection

func (o *overlaySeen) add(c SavedConnection) bool {
	key := connectionSyncKey(c)
	for _, prev := range *o {
		if connectionSyncKey(prev) == key && (connectionsShareAddress(prev, c) || len(connectionAddresses(c)) == 0) {
			return false
		}
	}
	*o = append(*o, c)
	return true
}

// mergeSavedConnections combines a just-pulled remote list with this
// device's own local list -- a row both sides have becomes one card that
// keeps this device's fields (see coalesceSavedConnection), and a
// local-only entry (never yet synced from this device) is preserved
// rather than dropped. Shared keys keep origin=local so they survive
// logout. changed reports whether the result actually differs
// from remote alone, i.e. whether there's anything new to push back.
func mergeSavedConnections(remote, local []SavedConnection) (merged []SavedConnection, changed bool) {
	m := newLocalMatcher(local)
	merged = make([]SavedConnection, 0, len(remote)+len(local))
	var overlay overlaySeen
	for _, c := range remote {
		idx, dup := m.match(c)
		switch {
		case dup:
		case idx >= 0:
			merged = append(merged, coalesceSavedConnection(m.locals[idx], c))
		case overlay.add(c):
			merged = append(merged, withOrigin(c, connectionOriginCloud))
		}
	}
	for i, c := range m.locals {
		if m.used[i] || containsSameRow(merged, c) {
			continue
		}
		merged = append(merged, withOrigin(c, connectionOriginLocal))
		changed = true
	}
	return merged, changed
}

// containsSameRow: conns already has c (same key and a shared address, or
// same key when c has no address at all).
func containsSameRow(conns []SavedConnection, c SavedConnection) bool {
	key := connectionSyncKey(c)
	noAddr := len(connectionAddresses(c)) == 0
	for _, o := range conns {
		if connectionSyncKey(o) == key && (noAddr || connectionsShareAddress(o, c)) {
			return true
		}
	}
	return false
}

// adoptRemoteConnections replaces the in-memory list with remote, tagging
// origin=local only for rows this device already owned locally so logout
// keeps them. Remote-only rows stay cloud (memory overlay), except keys
// this device explicitly marked Local and then deleted -- those must not
// come back as an overlay on the next login.
func adoptRemoteConnections(remote, previousLocal []SavedConnection, localOnlyKeys []string) []SavedConnection {
	m := newLocalMatcher(previousLocal)
	localOnly := syncedKeySet(localOnlyKeys)
	out := make([]SavedConnection, 0, len(remote))
	var overlay overlaySeen
	for _, c := range remote {
		idx, dup := m.match(c)
		if dup {
			continue
		}
		if idx >= 0 {
			out = append(out, coalesceSavedConnection(m.locals[idx], c))
			continue
		}
		if localOnly[connectionSyncKey(c)] {
			continue
		}
		if overlay.add(c) {
			out = append(out, withOrigin(c, connectionOriginCloud))
		}
	}
	return out
}

// keepUnsyncedLocals appends device-local rows that are not in adopted:
// rows never part of this account's last synced key set (created while
// logged out, or not yet confirmed by the server), rows the user marked
// Local, and siblings of a key the blob still has (a second row for the
// same agent). Keys that were synced, not opted out, and missing from
// remote were deleted on another device and must not be resurrected. An
// empty syncedKeys list still keeps current locals -- that is "everything
// on this device was opted out of the blob", not "delete them all".
func keepUnsyncedLocals(previousLocal, adopted []SavedConnection, syncedKeys, localOnlyKeys []string) (out []SavedConnection, added bool) {
	inAdopted := make(map[string]bool, len(adopted))
	for _, c := range adopted {
		inAdopted[connectionSyncKey(c)] = true
	}
	previouslySynced := syncedKeySet(syncedKeys)
	localOnly := syncedKeySet(localOnlyKeys)
	out = append([]SavedConnection(nil), adopted...)
	for _, c := range previousLocal {
		if connectionOrigin(c) == connectionOriginCloud {
			continue
		}
		if containsSameRow(out, c) {
			continue
		}
		key := connectionSyncKey(c)
		if !inAdopted[key] && previouslySynced[key] && !localOnly[key] {
			continue
		}
		out = append(out, withOrigin(c, connectionOriginLocal))
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
//
// SyncedKeys only ever holds what the server confirmed (a pull, or a push
// that returned 200). Local intent that hasn't reached the server yet lives
// in PendingAdds / PendingDeletes / Dirty, and survives restarts: a failed
// push (offline, expired token) used to record the new key as "synced", so
// the next pull read it as "deleted on another device" and dropped it, and
// a deletion that never reached the server came back on the next pull.
type syncStateFile struct {
	AccountEmail   string   `json:"account_email,omitempty"`
	Version        int      `json:"version"`
	SyncedKeys     []string `json:"synced_keys,omitempty"`
	LocalOnlyKeys  []string `json:"local_only_keys,omitempty"`
	PendingAdds    []string `json:"pending_adds,omitempty"`
	PendingDeletes []string `json:"pending_deletes,omitempty"`
	Dirty          bool     `json:"dirty,omitempty"`
}

func (s syncStateFile) hasPending() bool {
	return s.Dirty || len(s.PendingAdds) > 0 || len(s.PendingDeletes) > 0
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
	cm.updateSyncState(func(st *syncStateFile) { st.Version = version })
}

// persistSyncState records a server-confirmed version and key set. Pending
// local intent and LocalOnlyKeys are kept.
func (cm *ConnectionManager) persistSyncState(version int, keys []string) {
	cm.updateSyncState(func(st *syncStateFile) {
		st.Version = version
		st.SyncedKeys = keys
	})
}

// currentSyncState is the persisted state for the logged-in account, or a
// zero state when the file belongs to another account (or none).
func (cm *ConnectionManager) currentSyncState() syncStateFile {
	state := cm.loadPersistedSyncState()
	if cm.accountEmail() == "" || !strings.EqualFold(strings.TrimSpace(state.AccountEmail), cm.accountEmail()) {
		return syncStateFile{}
	}
	return state
}

// updateSyncState is the single read-modify-write path for the sync state
// file (and in-memory cm.syncVersion).
func (cm *ConnectionManager) updateSyncState(mutate func(*syncStateFile)) syncStateFile {
	cm.syncMu.Lock()
	defer cm.syncMu.Unlock()
	state := cm.currentSyncState()
	mutate(&state)
	state.AccountEmail = cm.accountEmail()
	cm.syncVersion = state.Version

	data, err := json.Marshal(state)
	if err != nil {
		return state
	}
	writer, err := storage.Writer(cm.syncStateStorageURI())
	if err != nil {
		logrus.Warnf("connection sync: could not persist sync state: %v", err)
		return state
	}
	defer writer.Close()
	_, _ = writer.Write(data)
	return state
}

func (cm *ConnectionManager) writeSyncState(version int, keys, localOnly []string) {
	cm.updateSyncState(func(st *syncStateFile) {
		st.Version = version
		st.SyncedKeys = keys
		st.LocalOnlyKeys = localOnly
	})
}

func subtractKeys(keys []string, drop map[string]bool) []string {
	if len(keys) == 0 || len(drop) == 0 {
		return keys
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if !drop[k] {
			out = append(out, k)
		}
	}
	return out
}

// blobKeySet is what this device wants in the account blob: the
// server-confirmed keys plus pending adds, minus pending deletes.
func (cm *ConnectionManager) blobKeySet() map[string]bool {
	state := cm.currentSyncState()
	set := syncedKeySet(state.SyncedKeys)
	for _, k := range state.PendingAdds {
		set[k] = true
	}
	for _, k := range state.PendingDeletes {
		delete(set, k)
	}
	return set
}

func (cm *ConnectionManager) localOnlyKeySet() map[string]bool {
	return syncedKeySet(cm.currentSyncState().LocalOnlyKeys)
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

// commitConnectionEdit writes an edited row to the device copy. If the
// row is Cloud in the UI, the new fields are pushed to the account blob
// immediately -- a debounced saveConnections alone left the blob stale,
// so logout/login restored the old copy unless the user toggled
// Local then Cloud to force a rewrite.
func (cm *ConnectionManager) commitConnectionEdit(idx int, updated SavedConnection) {
	if idx < 0 || idx >= len(cm.connections) {
		return
	}
	old := cm.connections[idx]
	oldKey := connectionSyncKey(old)
	wasCloud := cm.connectionDisplayOrigin(old) == connectionOriginCloud
	updated.Origin = connectionOriginLocal
	cm.connections[idx] = updated
	if wasCloud {
		newKey := connectionSyncKey(updated)
		if oldKey != newKey {
			cm.removeBlobKey(oldKey)
		}
		cm.addBlobKey(newKey)
	}
	cm.saveConnections()
	if wasCloud {
		cm.flushSyncPush()
	}
}

func (cm *ConnectionManager) addBlobKey(key string) {
	if key == "" || !cm.canSyncConnections() {
		return
	}
	cm.updateSyncState(func(st *syncStateFile) {
		st.PendingAdds = appendUniqueKey(st.PendingAdds, key)
		st.PendingDeletes = removeKey(st.PendingDeletes, key)
		st.LocalOnlyKeys = removeKey(st.LocalOnlyKeys, key)
		st.Dirty = true
	})
}

func (cm *ConnectionManager) removeBlobKey(key string) {
	if key == "" || !cm.canSyncConnections() {
		return
	}
	cm.updateSyncState(func(st *syncStateFile) {
		st.PendingAdds = removeKey(st.PendingAdds, key)
		st.LocalOnlyKeys = appendUniqueKey(st.LocalOnlyKeys, key)
		st.Dirty = true
	})
}

// noteConnectionDeleted records that a Cloud row was deleted on this
// device, so a pull that runs before the push lands (or after it failed)
// doesn't bring it back. Call after removing the row from cm.connections.
// A key still used by another row (same agent saved twice) stays.
func (cm *ConnectionManager) noteConnectionDeleted(deleted SavedConnection) {
	if !cm.canSyncConnections() {
		return
	}
	key := connectionSyncKey(deleted)
	for _, c := range cm.connections {
		if connectionSyncKey(c) == key {
			return
		}
	}
	if !cm.blobKeySet()[key] {
		return
	}
	cm.updateSyncState(func(st *syncStateFile) {
		st.PendingAdds = removeKey(st.PendingAdds, key)
		st.PendingDeletes = appendUniqueKey(st.PendingDeletes, key)
		st.Dirty = true
	})
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

// setSyncError records the last sync failure (and logs it once per
// distinct message, so a minute poll against a dead network stays quiet).
func (cm *ConnectionManager) setSyncError(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	cm.syncMu.Lock()
	changed := cm.syncLastError != msg
	cm.syncLastError = msg
	cm.syncMu.Unlock()
	if changed && msg != "" {
		logrus.Warnf("connection sync: %s", msg)
	}
}

// SyncLastError is the last sync failure, "" when the last round trip worked.
func (cm *ConnectionManager) SyncLastError() string {
	if cm == nil {
		return ""
	}
	cm.syncMu.Lock()
	defer cm.syncMu.Unlock()
	return cm.syncLastError
}

// withAuthRetry runs call with the current token and, if the backend says
// 401, renews the token once (AccountManager.RefreshToken) and retries.
func (cm *ConnectionManager) withAuthRetry(ctx context.Context, token string, call func(token string) error) error {
	err := call(token)
	if err == nil || !errors.Is(err, syncconn.ErrUnauthorized) {
		return err
	}
	fresh, rerr := cm.Account.RefreshToken(ctx, token)
	if rerr != nil {
		return fmt.Errorf("%w (token refresh: %v)", err, rerr)
	}
	return call(fresh)
}

// trySyncPullAndMerge fetches this account's synced connections list (if
// sync is configured at all -- see AccountManager.SyncCredentials) and
// reconciles it with whatever's already saved locally, then pushes any
// local change the server hasn't confirmed yet. Called at startup, every
// time the account/passphrase state changes (see NewConnectionManager's
// AccountManager onChange callback), and by the poll when the remote
// version moves. When credentials are missing (logout), the cloud overlay
// is dropped instead of leaving the previous account's rows in the list.
//
// Whether this ADDITIVELY MERGES with the local list or simply ADOPTS the
// remote list (plus never-uploaded locals) depends on hasSyncedWithCurrentAccount.
func (cm *ConnectionManager) trySyncPullAndMerge() {
	if cm.Account == nil {
		cm.dropCloudConnections()
		return
	}
	if _, _, ok := cm.Account.SyncCredentials(); !ok {
		cm.dropCloudConnections()
		return
	}
	cm.syncOpMu.Lock()
	defer cm.syncOpMu.Unlock()
	if cm.pullLocked() && cm.currentSyncState().hasPending() {
		cm.pushLocked()
	}
}

// pullLocked is one Pull + apply. Returns false when nothing was applied
// (no credentials, network/auth/decrypt failure). Caller holds syncOpMu.
func (cm *ConnectionManager) pullLocked() bool {
	token, key, ok := cm.Account.SyncCredentials()
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var plaintext []byte
	var version int
	err := cm.withAuthRetry(ctx, token, func(tok string) error {
		var perr error
		plaintext, version, perr = syncconn.Pull(ctx, tok, connectionsSyncKind, key)
		return perr
	})
	if err != nil {
		if errors.Is(err, syncconn.ErrNoData) {
			// Nothing pushed for this account yet -- keep device-local
			// rows, drop any leftover overlay. Locals are not seeded into
			// an empty blob; the user opts a row into Cloud from the list
			// (pending adds then go up via the push that follows).
			cm.connections = localConnections(cm.connections)
			fyne.Do(func() { cm.refreshConnectionsList() })
			cm.updateSyncState(func(st *syncStateFile) {
				st.Version = 0
				st.SyncedKeys = nil
				st.PendingDeletes = nil
			})
			cm.setSyncError(nil)
			return true
		}
		cm.setSyncError(fmt.Errorf("pull failed: %w", err))
		return false
	}

	var remote []SavedConnection
	if err := json.Unmarshal(plaintext, &remote); err != nil {
		cm.setSyncError(fmt.Errorf("decrypted payload was not valid JSON: %w", err))
		return false
	}
	cm.setSyncError(nil)
	cm.applyRemoteLocked(remote, version)
	return true
}

// applyRemoteLocked reconciles a server copy (from a pull or a 409) with
// this device's rows and pending intent. Caller holds syncOpMu.
func (cm *ConnectionManager) applyRemoteLocked(remote []SavedConnection, version int) {
	hasSyncedBefore := cm.hasSyncedWithCurrentAccount()
	state := cm.currentSyncState()

	pendingDeletes := syncedKeySet(state.PendingDeletes)
	visible := make([]SavedConnection, 0, len(remote))
	for _, c := range remote {
		if !pendingDeletes[connectionSyncKey(c)] {
			visible = append(visible, c)
		}
	}

	locals := localConnections(cm.connections)
	if hasSyncedBefore {
		// A pending add was never confirmed, so its absence from remote
		// is not a remote deletion.
		confirmed := subtractKeys(state.SyncedKeys, syncedKeySet(state.PendingAdds))
		adopted := adoptRemoteConnections(visible, locals, state.LocalOnlyKeys)
		adopted, _ = keepUnsyncedLocals(locals, adopted, confirmed, state.LocalOnlyKeys)
		cm.connections = adopted
	} else {
		cm.connections, _ = mergeSavedConnections(visible, locals)
	}

	remoteKeys := connectionSyncKeys(remote)
	inRemote := syncedKeySet(remoteKeys)
	cm.updateSyncState(func(st *syncStateFile) {
		st.Version = version
		st.SyncedKeys = remoteKeys
		// A delete the server already reflects is done.
		next := st.PendingDeletes[:0:0]
		for _, k := range st.PendingDeletes {
			if inRemote[k] {
				next = append(next, k)
			}
		}
		st.PendingDeletes = next
	})
	fyne.Do(func() { cm.refreshConnectionsList() })
	cm.saveConnectionsLocalOnly()
}

// scheduleSyncPush debounces a push after a local edit (see
// connection_manager_storage.go's saveConnections, which calls this) --
// no-ops instantly if sync isn't configured, so every existing call site
// stays free to call it unconditionally. The edit is marked dirty on disk
// first, so it is retried (poll, next start) even if this push fails.
func (cm *ConnectionManager) scheduleSyncPush() {
	if cm.Account == nil {
		return
	}
	if _, _, ok := cm.Account.SyncCredentials(); !ok {
		return
	}
	cm.markSyncDirty()

	cm.syncMu.Lock()
	defer cm.syncMu.Unlock()
	if cm.syncPushTimer != nil {
		cm.syncPushTimer.Stop()
	}
	cm.syncPushTimer = time.AfterFunc(syncPushDebounce, cm.doSyncPush)
}

func (cm *ConnectionManager) markSyncDirty() {
	cm.syncMu.Lock()
	cm.syncEditGen++
	cm.syncMu.Unlock()
	if !cm.currentSyncState().Dirty {
		cm.updateSyncState(func(st *syncStateFile) { st.Dirty = true })
	}
}

// startConnectionsSyncPolling periodically checks whether another device
// updated the account connections blob and pulls when the version advances,
// and retries any local change that hasn't reached the server.
// Complements the one-shot pull at startup / on account-state change.
func (cm *ConnectionManager) startConnectionsSyncPolling() {
	if cm.syncPollStop != nil {
		close(cm.syncPollStop)
	}
	cm.syncPollStop = make(chan struct{})
	stop := cm.syncPollStop
	go func() {
		ticker := time.NewTicker(syncPullPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				cm.pollConnectionsSync()
			}
		}
	}()
}

// pollConnectionsSync is the ticker body: renew the token if it is close to
// expiring, Meta, Pull only when remote is ahead of this device's last
// known version, and push when local changes are still pending.
func (cm *ConnectionManager) pollConnectionsSync() {
	if cm.Account == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cm.Account.RefreshTokenIfStale(ctx)

	token, _, ok := cm.Account.SyncCredentials()
	if !ok {
		return
	}
	cm.syncOpMu.Lock()
	defer cm.syncOpMu.Unlock()

	cm.syncMu.Lock()
	known := cm.syncVersion
	cm.syncMu.Unlock()

	var version int
	err := cm.withAuthRetry(ctx, token, func(tok string) error {
		var merr error
		version, _, merr = syncconn.Meta(ctx, tok, connectionsSyncKind)
		return merr
	})
	if err != nil {
		cm.setSyncError(fmt.Errorf("poll meta failed: %w", err))
		return
	}
	if version != known {
		// Ahead: another device pushed. Behind: the blob was reset or this
		// device's state is stale -- either way the server copy wins.
		logrus.Infof("connection sync: remote version %d != local %d -- pulling", version, known)
		if !cm.pullLocked() {
			return
		}
	} else {
		cm.setSyncError(nil)
	}
	if cm.currentSyncState().hasPending() {
		cm.pushLocked()
	}
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
	cm.syncOpMu.Lock()
	defer cm.syncOpMu.Unlock()
	cm.pushLocked()
}

// syncPushMaxConflicts bounds the 409 -> merge -> retry loop when another
// device keeps writing at the same moment.
const syncPushMaxConflicts = 3

// pushLocked uploads this device's view of the blob, merging the server
// copy and retrying on a version conflict. On success the pending intent
// that went up is cleared; on failure it stays on disk for the next poll.
// Caller holds syncOpMu.
func (cm *ConnectionManager) pushLocked() {
	for attempt := 0; attempt <= syncPushMaxConflicts; attempt++ {
		token, key, ok := cm.Account.SyncCredentials()
		if !ok {
			return
		}

		cm.syncMu.Lock()
		expectedVersion := cm.syncVersion
		gen := cm.syncEditGen
		cm.syncMu.Unlock()
		sent := cm.currentSyncState()

		payload := cm.pushPayload()
		plaintext, err := json.Marshal(payload)
		if err != nil {
			cm.setSyncError(fmt.Errorf("marshal failed: %w", err))
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var newVersion int
		err = cm.withAuthRetry(ctx, token, func(tok string) error {
			var perr error
			newVersion, perr = syncconn.Push(ctx, tok, connectionsSyncKind, key, plaintext, expectedVersion)
			return perr
		})
		cancel()
		if err != nil {
			var conflict *syncconn.ErrConflict
			if errors.As(err, &conflict) {
				logrus.Infof("connection sync: version conflict (another device synced first) -- merging and retrying")
				if !cm.applyConflictLocked(conflict) {
					return
				}
				continue
			}
			cm.setSyncError(fmt.Errorf("push failed: %w", err))
			return
		}

		cm.syncMu.Lock()
		editedSince := cm.syncEditGen != gen
		cm.syncMu.Unlock()
		sentAdds := syncedKeySet(sent.PendingAdds)
		sentDeletes := syncedKeySet(sent.PendingDeletes)
		cm.updateSyncState(func(st *syncStateFile) {
			st.Version = newVersion
			st.SyncedKeys = connectionSyncKeys(payload)
			st.PendingAdds = subtractKeys(st.PendingAdds, sentAdds)
			st.PendingDeletes = subtractKeys(st.PendingDeletes, sentDeletes)
			st.Dirty = editedSince
		})
		cm.setSyncError(nil)
		return
	}
	cm.setSyncError(fmt.Errorf("push gave up after %d version conflicts", syncPushMaxConflicts))
}

// applyConflictLocked merges the server's newer record (handed back by a
// 409 response, see syncconn.ErrConflict) the same way a pull would, so
// the retry carries both this device's pending changes and the other
// device's. Returns false when the retry can't help (wrong passphrase).
func (cm *ConnectionManager) applyConflictLocked(conflict *syncconn.ErrConflict) bool {
	if conflict == nil || conflict.Conflict == nil {
		// 409 with no record: the blob is gone, start from version 0.
		cm.setSyncVersion(0)
		return true
	}
	_, key, ok := cm.Account.SyncCredentials()
	if !ok {
		return false
	}
	plaintext, err := syncconn.Decrypt(key, conflict.Conflict.Ciphertext, conflict.Conflict.Nonce)
	if err != nil {
		cm.setSyncError(fmt.Errorf("sync passphrase does not match this account's synced data: %w", err))
		return false
	}
	var remote []SavedConnection
	if err := json.Unmarshal(plaintext, &remote); err != nil {
		cm.setSyncError(fmt.Errorf("conflicting copy was not valid JSON: %w", err))
		return false
	}
	cm.applyRemoteLocked(remote, conflict.Conflict.Version)
	return true
}

// ApplySyncPassphrase is the account dialog's "Set passphrase" button: it
// checks the passphrase against the account's synced blob before storing
// it, so a wrong one is reported (syncconn.ErrWrongPassphrase) and never
// saved. Any other pull failure (offline, 5xx) still stores the key -- the
// normal pull/poll path retries -- and is returned so the caller can show it.
func (cm *ConnectionManager) ApplySyncPassphrase(ctx context.Context, passphrase string) error {
	if cm.Account == nil {
		return fmt.Errorf("no account configured")
	}
	token, ok := cm.Account.AccountToken()
	if !ok {
		return fmt.Errorf("not logged in")
	}
	key := syncconn.DeriveKey(cm.Account.Email(), passphrase)
	err := cm.withAuthRetry(ctx, token, func(tok string) error {
		_, _, perr := syncconn.Pull(ctx, tok, connectionsSyncKind, key)
		return perr
	})
	if errors.Is(err, syncconn.ErrWrongPassphrase) {
		return err
	}
	cm.Account.SetSyncPassphrase(passphrase)
	if err != nil && !errors.Is(err, syncconn.ErrNoData) {
		return err
	}
	return nil
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

	cm.updateSyncState(func(st *syncStateFile) {
		st.Version = newVersion
		st.SyncedKeys = connectionSyncKeys(payload)
		st.PendingAdds = nil
		st.PendingDeletes = nil
		st.Dirty = false
	})
	cm.setSyncError(nil)
	return nil
}
