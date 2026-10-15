package streamhost

// Shared Moonlight pairing identity/trust across all three stream-host
// backends (Sunshine, rust-shine, punktfunk).
//
// The problem this solves: Sunshine, rust-shine ("usbridge-streamer"), and
// punktfunk-host are three independent processes. Each generates its own
// self-signed TLS server identity (cert+key) and keeps its own list of
// trusted client certificates, in three different on-disk formats. A
// Moonlight client's pairing is mutual TLS bound to the exact server
// certificate it saw during the original pairing handshake -- so switching
// which backend is active used to look, to a Moonlight client, like talking
// to an entirely different host, even on the same machine/IP, forcing a
// fresh PIN every time.
//
// The fix: maintain one canonical identity (RSA-2048 self-signed X.509,
// confirmed byte-compatible with all three -- Sunshine's real on-disk
// pkey.pem on this machine is PKCS8 "BEGIN PRIVATE KEY", identical to what
// rust-shine's crypto.rs and punktfunk's gamestream/cert.rs both produce,
// since all three deliberately mirror Sunshine's own crypto.cpp scheme) plus
// one canonical trust list (keyed by the SHA-256 fingerprint of each
// client's certificate DER -- the one identifier every backend's native
// format can derive, confirmed from punktfunk's own fingerprint = hex(sha256(der))
// scheme in mgmt/clients.rs), and project both into each backend's own
// expected files before it starts.
//
// This package deliberately never talks to Sunshine's, rust-shine's, or
// punktfunk's HTTP admin APIs to establish trust -- there is no such
// endpoint on any of them (pairing trust can only be established through
// the real PIN handshake, or by the backend loading its trust file at
// startup). Instead this edits each backend's own trust file directly, the
// same file that backend itself reads at startup -- which is also what
// makes RemoveTrustedClientEverywhere a reliable fix for a backend whose own
// admin-API unpair call doesn't actually persist (confirmed symptom reported
// for Sunshine): the rewritten file is authoritative regardless of whether
// that HTTP call did anything.
import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// sharedIdentityDir is where the canonical identity and trust list live --
// a sibling of sunshine/, rustshine/, and punktfunk/ under the agent's own
// stateDir, not inside any one backend's own directory, so it's obviously
// not "owned" by any single backend.
func sharedIdentityDir(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, "identity")
}

func sharedCertPath(stateDir string) string {
	if dir := sharedIdentityDir(stateDir); dir != "" {
		return filepath.Join(dir, "cert.pem")
	}
	return ""
}

func sharedKeyPath(stateDir string) string {
	if dir := sharedIdentityDir(stateDir); dir != "" {
		return filepath.Join(dir, "key.pem")
	}
	return ""
}

func sharedUUIDPath(stateDir string) string {
	if dir := sharedIdentityDir(stateDir); dir != "" {
		return filepath.Join(dir, "server_uuid.txt")
	}
	return ""
}

func trustStorePath(stateDir string) string {
	if dir := sharedIdentityDir(stateDir); dir != "" {
		return filepath.Join(dir, "trusted_clients.json")
	}
	return ""
}

// trustedClient is one paired Moonlight client in the canonical store.
// Fingerprint is the primary key (SHA-256 hex of the client cert's DER
// bytes) -- the one identifier every backend's native format can produce,
// even punktfunk's, which persists no uniqueid at rest at all. UniqueID and
// Name are best-effort metadata carried along for the two backends that do
// use a uniqueid (Sunshine, rust-shine); never used as the dedup key because
// punktfunk never supplies one.
type trustedClient struct {
	Fingerprint string `json:"fingerprint"`
	UniqueID    string `json:"uniqueid,omitempty"`
	Name        string `json:"name,omitempty"`
	Enabled     string `json:"enabled,omitempty"` // Sunshine's "true"/"false" string, preserved verbatim if a backend ever sets it
	CertDER     []byte `json:"cert_der"`
	// HostUUID is the uuid Sunshine's own file gives this client (what its client list
	// and unpair calls use) when it differs from UniqueID; read from that file only.
	HostUUID string `json:"-"`
}

// trustStoreFile is the canonical store's on-disk shape.
//
// Removed is a tombstone list of fingerprints explicitly unpaired via
// RemoveTrustedClientEverywhere. Without it, reconcileTrustStore's
// add-only union logic (see its doc comment) would immediately re-import a
// removed client the next time it reconciles against a backend whose own
// admin-API unpair call didn't actually persist the removal to its file --
// exactly the failure mode reported live for Sunshine. A fingerprint once
// tombstoned here is never re-added by reconciliation; only pairing a
// *new* PIN ceremony for that same device (which produces the same cert,
// since Moonlight clients reuse their own persisted keypair) would
// re-introduce it, which is correct -- that's a deliberate re-pair, not a
// resurrection of a stale file.
type trustStoreFile struct {
	Clients []trustedClient `json:"clients"`
	Removed []string        `json:"removed,omitempty"`
}

func fingerprintOf(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// pemToDER decodes a single PEM certificate block to raw DER. Returns an
// error for empty input or anything that isn't a parseable CERTIFICATE PEM
// block, rather than silently returning nil -- a bad/legacy entry in one of
// these files must never masquerade as a valid-but-empty cert and get
// propagated everywhere as such.
func pemToDER(certPEM string) ([]byte, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || len(block.Bytes) == 0 {
		return nil, fmt.Errorf("not a valid PEM certificate block")
	}
	return block.Bytes, nil
}

func derToPEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// loadCanonicalStore reads the canonical trust store, tolerating a missing
// or corrupt file as "empty" (logged, never fatal) -- the same discipline
// every native-format reader in shared_auth_formats.go follows: a bad trust
// file degrading to "nobody's paired yet" is recoverable (clients re-pair);
// treating it as fatal would be worse, since it would block every backend
// from starting at all.
func loadCanonicalStore(stateDir string) trustStoreFile {
	path := trustStorePath(stateDir)
	if path == "" {
		return trustStoreFile{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return trustStoreFile{}
	}
	var store trustStoreFile
	if err := json.Unmarshal(data, &store); err != nil {
		log.Printf("[shared-auth] %s is corrupt (%v) -- treating as empty", path, err)
		return trustStoreFile{}
	}
	return store
}

func saveCanonicalStore(stateDir string, store trustStoreFile) error {
	path := trustStorePath(stateDir)
	if path == "" {
		return nil
	}
	// Stable ordering so repeated reconciles produce byte-identical output
	// when nothing actually changed (easier to diff/debug, and avoids
	// spurious mtime churn from map/slice ordering alone).
	sort.Slice(store.Clients, func(i, j int) bool { return store.Clients[i].Fingerprint < store.Clients[j].Fingerprint })
	sort.Strings(store.Removed)
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteLocked(path, data, 0o600)
}

// mergeClient adds c to list, or if its fingerprint is already present,
// fills in any metadata the existing entry is missing from c (never
// overwrites a non-empty field with an empty one -- a backend that doesn't
// supply a uniqueid, e.g. punktfunk, must never blank out a uniqueid another
// backend already recorded for the same cert).
func mergeClient(list []trustedClient, c trustedClient) []trustedClient {
	for i, existing := range list {
		if existing.Fingerprint != c.Fingerprint {
			continue
		}
		if existing.UniqueID == "" {
			existing.UniqueID = c.UniqueID
		}
		if existing.Name == "" {
			existing.Name = c.Name
		}
		if existing.Enabled == "" {
			existing.Enabled = c.Enabled
		}
		if len(existing.CertDER) == 0 {
			existing.CertDER = c.CertDER
		}
		list[i] = existing
		return list
	}
	return append(list, c)
}

func removeByFingerprint(list []trustedClient, fp string) ([]trustedClient, bool) {
	out := make([]trustedClient, 0, len(list))
	removed := false
	for _, c := range list {
		if c.Fingerprint == fp {
			removed = true
			continue
		}
		out = append(out, c)
	}
	return out, removed
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func removeString(list []string, s string) ([]string, bool) {
	out := make([]string, 0, len(list))
	removed := false
	for _, v := range list {
		if v == s {
			removed = true
			continue
		}
		out = append(out, v)
	}
	return out, removed
}

// atomicWriteLocked writes data to path via the same tmp-file-then-rename
// pattern (and the same in-process + cross-process locking) upsertConfigKey
// uses for sunshine.conf/rust-shine's config file -- see configfile.go's
// lock doc comments for why both locks are needed. Reused here rather than
// duplicated: every file this package touches (the canonical store, and
// each backend's native trust file) needs the identical atomicity guarantee
// for the identical reason -- concurrent admin-API calls, or a thin-client
// GUI and the headless daemon both touching the same file.
func atomicWriteLocked(path string, data []byte, perm os.FileMode) error {
	if path == "" {
		return os.ErrNotExist
	}
	lock := configFileLock(path)
	lock.Lock()
	defer lock.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	unlockCrossProcess := acquireCrossProcessLock(path)
	defer unlockCrossProcess()

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// --- Shared TLS identity -----------------------------------------------

// generateIdentity mints a fresh self-signed RSA-2048 X.509 identity, the
// same shape as Sunshine's crypto.cpp / rust-shine's crypto.rs /
// punktfunk's gamestream/cert.rs (all three explicitly mirror this scheme --
// see their own doc comments). 20-year validity matches rust-shine's own
// stated rationale: avoid a client whose clock is behind this build date
// treating the cert as not-yet-valid.
func generateIdentity() (certPEM, keyPEM []byte, err error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate RSA-2048 key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "USBridge"},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("self-sign certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal PKCS8 key: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// readIdentityPair loads a cert+key PEM pair from disk, succeeding only if
// both files exist and are non-empty -- a half-written pair (e.g. a crash
// between writing cert.pem and key.pem) must never be adopted as valid.
func readIdentityPair(certPath, keyPath string) (certPEM, keyPEM []byte, ok bool) {
	c, err := os.ReadFile(certPath)
	if err != nil || len(strings.TrimSpace(string(c))) == 0 {
		return nil, nil, false
	}
	k, err := os.ReadFile(keyPath)
	if err != nil || len(strings.TrimSpace(string(k))) == 0 {
		return nil, nil, false
	}
	// Validate structurally -- a cert.pem that exists but doesn't actually
	// parse (truncated write, wrong file entirely) must fall through to the
	// next adoption candidate instead of being handed to every backend as
	// its new identity.
	if _, err := tls.X509KeyPair(c, k); err != nil {
		return nil, nil, false
	}
	return c, k, true
}

func dashifyUUID(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(s) != 32 {
		return ""
	}
	return strings.ToUpper(s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32])
}

// adoptExistingIdentity looks for an identity one of the three backends
// already established, in priority order (Sunshine first -- it's
// NewDefault's unconditional boot choice, see that function's doc comment,
// so on any pre-existing install it's the one most likely to already carry
// real paired devices, exactly as the real sunshine_state.json captured live
// on this machine during development shows: 4 real paired clients). Adopting
// rather than generating fresh is what makes rolling out this feature on an
// existing install safe -- generating a brand new identity here would change
// the one thing every already-paired Moonlight client is pinned to, forcing
// every single one of them to re-pair, which is the exact regression this
// whole feature exists to prevent.
func adoptExistingIdentity(stateDir string) (certPEM, keyPEM []byte, serverUUID string, ok bool) {
	sunDir := filepath.Join(stateDir, "sunshine")
	if c, k, ok := readIdentityPair(filepath.Join(sunDir, "cert.pem"), filepath.Join(sunDir, "pkey.pem")); ok {
		uuid := readSunshineServerUUID(filepath.Join(sunDir, "sunshine_state.json"))
		if uuid == "" {
			uuid, _ = randomUUID()
		}
		return c, k, strings.ToUpper(uuid), true
	}

	rsDir := filepath.Join(stateDir, "rustshine")
	if c, k, ok := readIdentityPair(filepath.Join(rsDir, "server_cert.pem"), filepath.Join(rsDir, "server_key.pem")); ok {
		uuid := strings.TrimSpace(readFileOrEmpty(filepath.Join(rsDir, "server_uuid.txt")))
		if uuid == "" {
			uuid, _ = randomUUID()
		}
		return c, k, uuid, true
	}

	pfDir := filepath.Join(stateDir, "punktfunk")
	if c, k, ok := readIdentityPair(filepath.Join(pfDir, "cert.pem"), filepath.Join(pfDir, "key.pem")); ok {
		uuid := dashifyUUID(readFileOrEmpty(filepath.Join(pfDir, "uniqueid")))
		if uuid == "" {
			uuid, _ = randomUUID()
		}
		return c, k, uuid, true
	}

	return nil, nil, "", false
}

func readFileOrEmpty(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// readSunshineServerUUID extracts root.uniqueid from sunshine_state.json
// without needing the full named_devices parsing machinery in
// shared_auth_formats.go -- used only during one-time identity adoption.
func readSunshineServerUUID(stateFile string) string {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}
	var probe struct {
		Root struct {
			UniqueID string `json:"uniqueid"`
		} `json:"root"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return ""
	}
	return probe.Root.UniqueID
}

// EnsureSharedIdentity returns the canonical identity for stateDir,
// generating (fresh install) or adopting (existing install, see
// adoptExistingIdentity) one on first call and persisting it under
// sharedIdentityDir from then on. Safe to call on every backend Start() --
// cheap once the canonical files exist.
func EnsureSharedIdentity(stateDir string) (certPEM, keyPEM []byte, serverUUID string, err error) {
	if stateDir == "" {
		return nil, nil, "", fmt.Errorf("empty stateDir")
	}
	if c, k, ok := readIdentityPair(sharedCertPath(stateDir), sharedKeyPath(stateDir)); ok {
		uuid := strings.TrimSpace(readFileOrEmpty(sharedUUIDPath(stateDir)))
		if uuid == "" {
			// Identity exists but the uuid sidecar is missing/corrupt --
			// regenerate just the uuid rather than discarding a perfectly
			// good, already-trusted-by-real-clients identity over it.
			uuid, err = randomUUID()
			if err != nil {
				return nil, nil, "", err
			}
			uuid = strings.ToUpper(uuid)
			if werr := atomicWriteLocked(sharedUUIDPath(stateDir), []byte(uuid), 0o644); werr != nil {
				log.Printf("[shared-auth] warning: could not persist regenerated server uuid: %v", werr)
			}
		}
		return c, k, uuid, nil
	}

	var uuid string
	if c, k, u, ok := adoptExistingIdentity(stateDir); ok {
		certPEM, keyPEM, uuid = c, k, u
		log.Printf("[shared-auth] adopted existing backend identity as the shared one (uuid=%s)", uuid)
	} else {
		certPEM, keyPEM, err = generateIdentity()
		if err != nil {
			return nil, nil, "", err
		}
		uuid, err = randomUUID()
		if err != nil {
			return nil, nil, "", err
		}
		uuid = strings.ToUpper(uuid)
		log.Printf("[shared-auth] generated a fresh shared identity (uuid=%s)", uuid)
	}

	if err := atomicWriteLocked(sharedCertPath(stateDir), certPEM, 0o644); err != nil {
		return nil, nil, "", err
	}
	if err := atomicWriteLocked(sharedKeyPath(stateDir), keyPEM, 0o600); err != nil {
		return nil, nil, "", err
	}
	if err := atomicWriteLocked(sharedUUIDPath(stateDir), []byte(uuid), 0o644); err != nil {
		return nil, nil, "", err
	}
	return certPEM, keyPEM, uuid, nil
}

// ReconcileSharedAuth provisions the canonical TLS identity into this
// backend's own files and converges its trust store with the other two
// backends', before it starts. Called at the top of every backend's
// Start() (see sunshine_backend.go, rustshine_backend.go,
// punktfunk_backend.go) -- idempotent and cheap, so running it on every
// single Start() (not just the first time) is deliberate: it's what makes a
// backend switch converge trust automatically, and what self-heals a
// canonical store that fell behind (e.g. the agent crashed between a
// pairing event and its sync).
//
// Never returns an error to a caller that would abort Start() over it --
// every call site logs and continues, matching this codebase's existing
// discipline for non-essential startup steps (e.g. ensureSunshineStateFile).
// A failure here means "switching backends might ask for a PIN again this
// one time," never "the stream host failed to start."
func ReconcileSharedAuth(stateDir string) {
	if stateDir == "" {
		return
	}
	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		log.Printf("[shared-auth] could not establish shared identity: %v", err)
		return
	}

	if err := provisionSunshineIdentity(stateDir); err != nil {
		log.Printf("[shared-auth] could not provision sunshine identity: %v", err)
	}
	if err := provisionRustshineIdentity(stateDir); err != nil {
		log.Printf("[shared-auth] could not provision rustshine identity: %v", err)
	}
	if err := provisionPunktfunkIdentity(stateDir); err != nil {
		log.Printf("[shared-auth] could not provision punktfunk identity: %v", err)
	}

	store := loadCanonicalStore(stateDir)
	canonical := store.Clients

	// Read-merge-write each backend's native trust file in turn, folding
	// anything new it reveals back into canonical before moving to the
	// next one. A single top-to-bottom pass can still miss an entry the
	// *last* backend reveals (the earlier two already finished writing by
	// then), so run one extra propagation pass whenever the second or
	// third backend actually revealed something new.
	importedSun, err := projectToSunshine(stateDir, serverUUID, canonical, store.Removed)
	if err != nil {
		log.Printf("[shared-auth] sunshine trust projection failed: %v", err)
	}
	for _, c := range importedSun {
		canonical = mergeClient(canonical, c)
	}

	importedRS, err := projectToRustshine(stateDir, canonical, store.Removed)
	if err != nil {
		log.Printf("[shared-auth] rustshine trust projection failed: %v", err)
	}
	for _, c := range importedRS {
		canonical = mergeClient(canonical, c)
	}

	importedPF, err := projectToPunktfunk(stateDir, canonical, store.Removed)
	if err != nil {
		log.Printf("[shared-auth] punktfunk trust projection failed: %v", err)
	}
	for _, c := range importedPF {
		canonical = mergeClient(canonical, c)
	}

	if len(importedRS) > 0 || len(importedPF) > 0 {
		if _, err := projectToSunshine(stateDir, serverUUID, canonical, store.Removed); err != nil {
			log.Printf("[shared-auth] sunshine trust re-projection failed: %v", err)
		}
	}
	if len(importedPF) > 0 {
		if _, err := projectToRustshine(stateDir, canonical, store.Removed); err != nil {
			log.Printf("[shared-auth] rustshine trust re-projection failed: %v", err)
		}
	}

	store.Clients = canonical
	if err := saveCanonicalStore(stateDir, store); err != nil {
		log.Printf("[shared-auth] could not persist canonical trust store: %v", err)
	}
}

// TrustedFingerprints snapshots the fingerprints backend's own native trust
// file currently holds. SubmitMoonlightPIN takes one right before relaying
// a PIN, so AwaitPairingAndSync can tell the client that pairing adds apart
// from everything that was already there.
func TrustedFingerprints(stateDir, backend string) map[string]bool {
	out := map[string]bool{}
	for _, c := range readNativeTrusted(stateDir, backend) {
		out[c.Fingerprint] = true
	}
	return out
}

func readNativeTrusted(stateDir, backend string) []trustedClient {
	var clients []trustedClient
	switch backend {
	case "sunshine":
		_, clients, _ = readSunshineTrusted(stateDir)
	case "rustshine":
		clients, _ = readRustshineTrusted(stateDir)
	case "punktfunk":
		clients, _ = readPunktfunkTrusted(stateDir)
	}
	return clients
}

// pairingSyncPoll is how often AwaitPairingAndSync re-reads the active
// backend's trust file while the client finishes the pairing handshake.
var pairingSyncPoll = 250 * time.Millisecond

// AwaitPairingAndSync waits, up to timeout, for a client that is not in
// before to show up in backend's own trust file, then runs SyncAfterPair.
//
// It has to wait: SubmitPIN returns as soon as the host accepts the PIN,
// while the client still has the clientchallenge..pairchallenge stages to
// go, and the host writes the new certificate to disk only after those.
// Calling SyncAfterPair straight after SubmitPIN (what this code used to
// do) always saw the file without the new client, so the pairing never
// reached the canonical store, never lifted a tombstone, and the next
// backend Start()'s ReconcileSharedAuth stripped it out of every file
// again -- including the backend it had just been paired on.
func AwaitPairingAndSync(stateDir, backend string, before map[string]bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if SyncAfterPair(stateDir, backend, before) {
			return true
		}
		if time.Now().After(deadline) {
			log.Printf("[shared-auth] no new client appeared in %s's trust file within %s of the PIN; not propagated", backend, timeout)
			return false
		}
		time.Sleep(pairingSyncPoll)
	}
}

// SyncAfterPair propagates a client that just paired against activeBackend
// to the other two backends immediately, without waiting for either of
// them to next Start() and run ReconcileSharedAuth itself. It reports
// whether activeBackend's file holds a client that is not in before.
//
// before is the fingerprint set activeBackend's file held before the PIN
// was relayed (TrustedFingerprints); nil treats every entry as new. Only a
// new entry -- one that appeared in direct response to a pairing ceremony
// -- may lift a tombstone (see RemoveTrustedClientEverywhere's doc
// comment): if the operator unpairs a device and later genuinely re-pairs
// it (Moonlight clients reuse their persisted keypair, so the certificate
// is identical), that is a deliberate re-pair. An entry that was already
// in the file before the PIN is a stale leftover and stays tombstoned,
// the same rule ReconcileSharedAuth's passive reconcile follows.
func SyncAfterPair(stateDir, activeBackend string, before map[string]bool) bool {
	if stateDir == "" {
		return false
	}
	switch activeBackend {
	case "sunshine", "rustshine", "punktfunk":
	default:
		return false
	}
	current := readNativeTrusted(stateDir, activeBackend)

	store := loadCanonicalStore(stateDir)
	sawNew := false
	changed := false
	for _, c := range current {
		isNew := !before[c.Fingerprint]
		if isNew {
			sawNew = true
			if removedList, ok := removeString(store.Removed, c.Fingerprint); ok {
				store.Removed = removedList
				changed = true
			}
		} else if containsString(store.Removed, c.Fingerprint) {
			continue
		}
		before := len(store.Clients)
		store.Clients = mergeClient(store.Clients, c)
		if len(store.Clients) != before {
			changed = true
		}
	}
	if !changed {
		return sawNew
	}
	if err := saveCanonicalStore(stateDir, store); err != nil {
		log.Printf("[shared-auth] could not persist canonical trust store after pairing: %v", err)
		return sawNew
	}

	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		log.Printf("[shared-auth] could not resolve shared identity after pairing: %v", err)
		return sawNew
	}
	if activeBackend != "sunshine" {
		if _, err := projectToSunshine(stateDir, serverUUID, store.Clients, store.Removed); err != nil {
			log.Printf("[shared-auth] sunshine trust propagation after pairing failed: %v", err)
		}
	}
	if activeBackend != "rustshine" {
		if _, err := projectToRustshine(stateDir, store.Clients, store.Removed); err != nil {
			log.Printf("[shared-auth] rustshine trust propagation after pairing failed: %v", err)
		}
	}
	if activeBackend != "punktfunk" {
		if _, err := projectToPunktfunk(stateDir, store.Clients, store.Removed); err != nil {
			log.Printf("[shared-auth] punktfunk trust propagation after pairing failed: %v", err)
		}
	}
	if sawNew {
		log.Printf("[shared-auth] client paired on %s propagated to the other backends", activeBackend)
	}
	return sawNew
}

// RemoveTrustedClientEverywhere unpairs identifier (meaning depends on
// activeBackend -- see each backend's UnpairClient doc comment: a
// Sunshine/rust-shine uniqueid, or a punktfunk certificate fingerprint) from
// the canonical store and force-rewrites all three backends' native trust
// files to exclude it -- a strict overwrite (writeXTrusted, not
// projectToX's read-merge-write), never a union, since the whole point is
// removal: unioning against a backend whose own file still has the stale
// entry (the reported Sunshine bug) would just re-import the very thing
// being removed.
//
// activeBackend's own native file is resolved against first (even when the
// canonical store has never heard of this fingerprint before -- e.g. this
// client paired before this feature existed), so the fingerprint can always
// be recovered from *some* file, and the tombstone below applies even to a
// client this layer never synced in the first place.
func RemoveTrustedClientEverywhere(stateDir, activeBackend, identifier string) error {
	if stateDir == "" {
		return fmt.Errorf("empty stateDir")
	}

	fp := resolveFingerprint(stateDir, activeBackend, identifier)
	if fp == "" {
		return fmt.Errorf("could not resolve %q to a known client certificate", identifier)
	}

	store := loadCanonicalStore(stateDir)
	store.Clients, _ = removeByFingerprint(store.Clients, fp)
	if !containsString(store.Removed, fp) {
		store.Removed = append(store.Removed, fp)
	}
	if err := saveCanonicalStore(stateDir, store); err != nil {
		return fmt.Errorf("persist canonical store: %w", err)
	}

	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		return fmt.Errorf("resolve shared identity: %w", err)
	}

	var errs []string
	if err := writeSunshineTrusted(stateDir, serverUUID, store.Clients); err != nil {
		errs = append(errs, "sunshine: "+err.Error())
	}
	if err := writeRustshineTrusted(stateDir, store.Clients); err != nil {
		errs = append(errs, "rustshine: "+err.Error())
	}
	if err := writePunktfunkTrusted(stateDir, store.Clients); err != nil {
		errs = append(errs, "punktfunk: "+err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("partial unpair failure: %s", strings.Join(errs, "; "))
	}
	return nil
}

// sameClientID compares two client ids the way every host means them: Sunshine and
// Vibepollo keep a dashed UUID, RustShine and Moonlight the bare 32 hex digits, in
// either case.
func sameClientID(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "-", "")) }
	return norm(a) == norm(b)
}

// resolveFingerprint turns a backend-specific unpair identifier into the
// canonical fingerprint key. Checked in this order: the active backend's
// own native file (authoritative, works even for clients predating this
// feature) first, then the canonical store (covers a client that was
// already removed from the active backend's file by a prior, partially
// successful attempt), then finally identifier itself (punktfunk's
// UnpairClient already receives a fingerprint directly, so this is the
// normal path for it, not a fallback guess).
func resolveFingerprint(stateDir, activeBackend, identifier string) string {
	switch activeBackend {
	case "sunshine":
		_, clients, err := readSunshineTrusted(stateDir)
		if err == nil {
			for _, c := range clients {
				if sameClientID(c.UniqueID, identifier) || sameClientID(c.HostUUID, identifier) {
					return c.Fingerprint
				}
			}
		}
	case "rustshine":
		clients, err := readRustshineTrusted(stateDir)
		if err == nil {
			for _, c := range clients {
				if sameClientID(c.UniqueID, identifier) {
					return c.Fingerprint
				}
			}
		}
	case "punktfunk":
		clients, err := readPunktfunkTrusted(stateDir)
		if err == nil {
			for _, c := range clients {
				if c.Fingerprint == strings.ToLower(identifier) {
					return c.Fingerprint
				}
			}
		}
	}

	store := loadCanonicalStore(stateDir)
	for _, c := range store.Clients {
		if sameClientID(c.UniqueID, identifier) || c.Fingerprint == strings.ToLower(identifier) {
			return c.Fingerprint
		}
	}

	if len(identifier) == 64 {
		if _, err := hex.DecodeString(identifier); err == nil {
			return strings.ToLower(identifier)
		}
	}
	return ""
}
