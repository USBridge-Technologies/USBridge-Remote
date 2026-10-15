package streamhost

// Per-backend native trust-file and identity-file formats, confirmed
// against: a real sunshine_state.json captured live from this agent's own
// data directory (4 real paired devices -- the exact shape asserted in the
// tests), rust-shine's crates/usbridge-streamer-proto/src/pairing.rs
// (trusted_clients_pem/parse_trusted_clients_pem), and punktfunk's
// crates/punktfunk-host/src/gamestream/mod.rs (paired_path/load_paired/
// save_paired) and mgmt/clients.rs (fingerprint = hex(sha256(der))).
import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// --- Sunshine: sunshine_state.json --------------------------------------
//
// Real on-disk shape (field names confirmed live, not guessed):
//
//	{
//	  "username": "...", "salt": "...", "password": "...",
//	  "root": {
//	    "uniqueid": "C064935C-8F6E-E601-D90C-41DCF049EFA9",
//	    "named_devices": [
//	      {"name": "", "cert": "-----BEGIN CERTIFICATE-----...", "uuid": "...", "enabled": "true"}
//	    ]
//	  }
//	}
//
// Read/written as generic maps (not a fixed struct) so any field this code
// doesn't know about -- Sunshine's own, or a future one -- round-trips
// untouched instead of being silently dropped on the next write.

type sunshineNamedDevice struct {
	Name    string `json:"name"`
	Cert    string `json:"cert"`
	UUID    string `json:"uuid"`
	Enabled string `json:"enabled"`
}

func sunshineStatePath(stateDir string) string {
	return (&sunshineBackend{stateDir: stateDir}).credentialsFilePath()
}

func sunshineIdentityPaths(stateDir string) (certPath, keyPath string) {
	b := &sunshineBackend{stateDir: stateDir}
	return b.certPath(), b.pkeyPath()
}

// readSunshineTrusted reads sunshine_state.json's paired-client list.
// Returns ("", nil, nil) for a missing or corrupt file -- never an error a
// caller would treat as fatal; see loadCanonicalStore's identical
// discipline.
func readSunshineTrusted(stateDir string) (serverUUID string, clients []trustedClient, err error) {
	return readSunshineTrustedFile(sunshineStatePath(stateDir))
}

// readSunshineTrustedFile is readSunshineTrusted for any sunshine_state.json.
func readSunshineTrustedFile(path string) (serverUUID string, clients []trustedClient, err error) {
	if path == "" {
		return "", nil, nil
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		return "", nil, nil
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil {
		return "", nil, nil
	}
	rootRaw, ok := top["root"]
	if !ok {
		return "", nil, nil
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(rootRaw, &root) != nil {
		return "", nil, nil
	}
	if uidRaw, ok := root["uniqueid"]; ok {
		_ = json.Unmarshal(uidRaw, &serverUUID)
	}
	devicesRaw, ok := root["named_devices"]
	if !ok {
		return serverUUID, nil, nil
	}
	var devices []sunshineNamedDevice
	if json.Unmarshal(devicesRaw, &devices) != nil {
		return serverUUID, nil, nil
	}
	for _, d := range devices {
		der, derErr := pemToDER(d.Cert)
		if derErr != nil {
			// A malformed cert entry must never block every other valid
			// one from round-tripping -- skip just this one.
			continue
		}
		clients = append(clients, trustedClient{
			Fingerprint: fingerprintOf(der),
			UniqueID:    d.UUID,
			Name:        d.Name,
			Enabled:     d.Enabled,
			CertDER:     der,
		})
	}
	return serverUUID, clients, nil
}

// A paired client's id is the 32 hex digits the Moonlight client sends as its uniqueid.
// Sunshine keeps any string in a named device's `uuid`, but Vibepollo (a Sunshine fork) refuses
// the whole state file unless every device uuid is a canonical 8-4-4-4-12 UUID -- and then
// stops its HTTP interface ("durable pairing state could not be loaded"). So the id goes
// to the file dashed; Sunshine itself accepts either form.
func sunshineUUIDOut(id string) string {
	if len(id) != 32 || !isHex(id) {
		return id
	}
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// defaultSunshineStateTemplate mirrors sunshineBackend.ensureSunshineStateFile's
// own bootstrap template (sunshine_backend.go) -- duplicated deliberately
// rather than imported, since that method pre-creates the file before
// Sunshine's own --creds call writes real username/password into it; this
// package's writer runs independently and must never clobber those once set.
func defaultSunshineStateTemplate(serverUUID string) map[string]json.RawMessage {
	rootBytes, _ := json.Marshal(map[string]any{
		"uniqueid":      serverUUID,
		"named_devices": []sunshineNamedDevice{},
	})
	top := map[string]json.RawMessage{
		"username": json.RawMessage(`"sunshine"`),
		"salt":     json.RawMessage(`""`),
		"password": json.RawMessage(`""`),
		"root":     json.RawMessage(rootBytes),
	}
	return top
}

// writeSunshineTrusted force-overwrites sunshine_state.json's
// root.named_devices to be exactly clients (strict overwrite, not a merge --
// see RemoveTrustedClientEverywhere's doc comment for why removal needs
// this instead of projectToSunshine's read-merge-write), and root.uniqueid
// to serverUUID. Every other top-level and root field (username, salt,
// password, and anything neither this code nor ensureSunshineStateFile
// knows about) is preserved byte-for-byte from the existing file.
func writeSunshineTrusted(stateDir, serverUUID string, clients []trustedClient) error {
	path := sunshineStatePath(stateDir)
	if path == "" {
		return nil
	}

	var top map[string]json.RawMessage
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &top) != nil || len(top) == 0 {
			top = nil
		}
	}
	if top == nil {
		top = defaultSunshineStateTemplate(serverUUID)
	}

	var root map[string]json.RawMessage
	if rootRaw, ok := top["root"]; ok {
		_ = json.Unmarshal(rootRaw, &root)
	}
	if root == nil {
		root = map[string]json.RawMessage{}
	}

	uidBytes, _ := json.Marshal(serverUUID)
	root["uniqueid"] = uidBytes

	devices := make([]sunshineNamedDevice, 0, len(clients))
	for _, c := range clients {
		enabled := c.Enabled
		if enabled == "" {
			enabled = "true"
		}
		devices = append(devices, sunshineNamedDevice{
			Name:    c.Name,
			Cert:    derToPEM(c.CertDER),
			UUID:    sunshineUUIDOut(c.UniqueID),
			Enabled: enabled,
		})
	}
	// Stable order (by uuid) so an unrelated re-provision doesn't produce
	// unnecessary diffs/log noise.
	sort.Slice(devices, func(i, j int) bool { return devices[i].UUID < devices[j].UUID })
	devicesBytes, err := json.Marshal(devices)
	if err != nil {
		return err
	}
	root["named_devices"] = devicesBytes

	rootBytes, err := json.Marshal(root)
	if err != nil {
		return err
	}
	top["root"] = rootBytes

	out, err := json.MarshalIndent(top, "", "    ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return atomicWriteLocked(path, out, 0o644)
}

// projectToSunshine reads Sunshine's current trust file, computes which of
// its entries canonical doesn't have yet (imported), writes back the union
// of canonical+imported, and returns imported so the caller can fold it
// into the authoritative canonical store before projecting to the other two
// backends. removed is the tombstone list (see trustStoreFile's doc
// comment) -- any fingerprint in it is excluded from both the import and
// the write, so a client already explicitly unpaired is never resurrected
// just because Sunshine's own file still (incorrectly) has it.
func projectToSunshine(stateDir, serverUUID string, canonical []trustedClient, removed []string) (imported []trustedClient, err error) {
	existingUUID, existing, _ := readSunshineTrusted(stateDir)
	if serverUUID == "" {
		serverUUID = existingUUID
	}

	have := make(map[string]bool, len(canonical))
	for _, c := range canonical {
		have[c.Fingerprint] = true
	}
	isRemoved := make(map[string]bool, len(removed))
	for _, fp := range removed {
		isRemoved[fp] = true
	}

	merged := append([]trustedClient(nil), canonical...)
	for _, c := range existing {
		if isRemoved[c.Fingerprint] {
			continue
		}
		if !have[c.Fingerprint] {
			imported = append(imported, c)
			have[c.Fingerprint] = true
		}
		merged = mergeClient(merged, c)
	}

	if err := writeSunshineTrusted(stateDir, serverUUID, merged); err != nil {
		return nil, err
	}
	return imported, nil
}

func provisionSunshineIdentity(stateDir string) error {
	certPEM, keyPEM, _, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		return err
	}
	certPath, keyPath := sunshineIdentityPaths(stateDir)
	if certPath == "" || keyPath == "" {
		return nil
	}
	if err := atomicWriteLocked(certPath, certPEM, 0o644); err != nil {
		return err
	}
	return atomicWriteLocked(keyPath, keyPEM, 0o600)
}

// --- rust-shine: trusted_clients.pem + server_cert.pem/server_key.pem --
//
// Format confirmed from PairingManager::trusted_clients_pem /
// parse_trusted_clients_pem (rust-shine's pairing.rs): each client is a
// standard PEM CERTIFICATE block, preceded by a "# uniqueid:<id>" comment
// line. main.rs's load_or_generate_identity/load_or_generate_server_uuid
// read server_cert.pem/server_key.pem/server_uuid.txt directly under
// --state-dir.

func rustshineStateSubdir(stateDir string) string {
	return filepath.Join(stateDir, "rustshine")
}

func rustshineTrustedClientsPath(stateDir string) string {
	return filepath.Join(rustshineStateSubdir(stateDir), "trusted_clients.pem")
}

func rustshineIdentityPaths(stateDir string) (certPath, keyPath, uuidPath string) {
	dir := rustshineStateSubdir(stateDir)
	return filepath.Join(dir, "server_cert.pem"), filepath.Join(dir, "server_key.pem"), filepath.Join(dir, "server_uuid.txt")
}

func readRustshineTrusted(stateDir string) (clients []trustedClient, err error) {
	data, rerr := os.ReadFile(rustshineTrustedClientsPath(stateDir))
	if rerr != nil {
		return nil, nil
	}
	uniqueids, blocks := parseTrustedClientsPEM(data)
	for i, der := range blocks {
		uid := ""
		if i < len(uniqueids) {
			uid = uniqueids[i]
		}
		clients = append(clients, trustedClient{
			Fingerprint: fingerprintOf(der),
			UniqueID:    uid,
			CertDER:     der,
		})
	}
	return clients, nil
}

// parseTrustedClientsPEM mirrors rust-shine's
// PairingManager::parse_trusted_clients_pem exactly: scans for "#
// uniqueid:<id>" comment lines, each immediately preceding the PEM
// CERTIFICATE block it names.
func parseTrustedClientsPEM(data []byte) (uniqueids []string, certsDER [][]byte) {
	lines := strings.Split(string(data), "\n")
	pending := ""
	var buf strings.Builder
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if id, ok := strings.CutPrefix(strings.TrimSpace(trimmed), "# uniqueid:"); ok {
			pending = id
			continue
		}
		if strings.HasPrefix(trimmed, "-----BEGIN CERTIFICATE-----") {
			inBlock = true
			buf.Reset()
		}
		if inBlock {
			buf.WriteString(trimmed)
			buf.WriteByte('\n')
		}
		if strings.HasPrefix(trimmed, "-----END CERTIFICATE-----") && inBlock {
			inBlock = false
			if der, err := pemToDER(buf.String()); err == nil {
				certsDER = append(certsDER, der)
				uniqueids = append(uniqueids, pending)
			}
			pending = ""
		}
	}
	return uniqueids, certsDER
}

func encodeTrustedClientsPEM(clients []trustedClient) []byte {
	var out strings.Builder
	sorted := append([]trustedClient(nil), clients...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Fingerprint < sorted[j].Fingerprint })
	for _, c := range sorted {
		out.WriteString("# uniqueid:" + c.UniqueID + "\n")
		out.WriteString(derToPEM(c.CertDER))
	}
	return []byte(out.String())
}

func writeRustshineTrusted(stateDir string, clients []trustedClient) error {
	return atomicWriteLocked(rustshineTrustedClientsPath(stateDir), encodeTrustedClientsPEM(clients), 0o600)
}

func projectToRustshine(stateDir string, canonical []trustedClient, removed []string) (imported []trustedClient, err error) {
	existing, _ := readRustshineTrusted(stateDir)

	have := make(map[string]bool, len(canonical))
	for _, c := range canonical {
		have[c.Fingerprint] = true
	}
	isRemoved := make(map[string]bool, len(removed))
	for _, fp := range removed {
		isRemoved[fp] = true
	}

	merged := append([]trustedClient(nil), canonical...)
	for _, c := range existing {
		if isRemoved[c.Fingerprint] {
			continue
		}
		if !have[c.Fingerprint] {
			imported = append(imported, c)
			have[c.Fingerprint] = true
		}
		merged = mergeClient(merged, c)
	}

	if err := writeRustshineTrusted(stateDir, merged); err != nil {
		return nil, err
	}
	return imported, nil
}

func provisionRustshineIdentity(stateDir string) error {
	certPEM, keyPEM, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		return err
	}
	certPath, keyPath, uuidPath := rustshineIdentityPaths(stateDir)
	if err := atomicWriteLocked(certPath, certPEM, 0o644); err != nil {
		return err
	}
	if err := atomicWriteLocked(keyPath, keyPEM, 0o600); err != nil {
		return err
	}
	return atomicWriteLocked(uuidPath, []byte(serverUUID), 0o644)
}

// --- punktfunk: paired.json + client-labels.json + cert.pem/key.pem/uniqueid --
//
// Format confirmed from punktfunk-host's gamestream/mod.rs (paired_path:
// Vec<Vec<u8>> of raw cert DER, serde_json default encoding = array of
// byte-number arrays, no base64) and mgmt/clients.rs (fingerprint =
// hex(sha256(der)), label store keyed by that same lowercase hex string).
// gamestream/cert.rs's ServerIdentity::load_or_create reads cert.pem/key.pem
// from the same config dir; host.rs's load_or_create_uniqueid reads
// "uniqueid" verbatim (it generates dash-less hex itself, but any string
// works -- see provisionPunktfunkIdentity).

func punktfunkConfigSubdir(stateDir string) string {
	return (&punktfunkBackend{stateDir: stateDir}).punktfunkConfigDir()
}

func punktfunkPairedPath(stateDir string) string {
	return filepath.Join(punktfunkConfigSubdir(stateDir), "paired.json")
}

func punktfunkLabelsPath(stateDir string) string {
	return filepath.Join(punktfunkConfigSubdir(stateDir), "client-labels.json")
}

func punktfunkIdentityPaths(stateDir string) (certPath, keyPath, uuidPath string) {
	dir := punktfunkConfigSubdir(stateDir)
	return filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"), filepath.Join(dir, "uniqueid")
}

func readPunktfunkTrusted(stateDir string) (clients []trustedClient, err error) {
	data, rerr := os.ReadFile(punktfunkPairedPath(stateDir))
	if rerr != nil {
		return nil, nil
	}
	// NOT []byte: Go's encoding/json special-cases a []byte destination to
	// expect a base64 JSON *string*. punktfunk-host's own
	// serde_json::to_vec(Vec<Vec<u8>>) writes a plain JSON array of
	// byte-number arrays instead (e.g. "[[48,130,...]]") -- confirmed by a
	// round-trip test against that exact shape. Decoding into []byte here
	// would silently accept this package's own base64 output in tests
	// while failing to parse (or worse, silently misparsing) a real
	// paired.json written by the actual Rust binary.
	var rows [][]int
	if json.Unmarshal(data, &rows) != nil {
		return nil, nil
	}
	ders := make([][]byte, 0, len(rows))
	for _, row := range rows {
		der := make([]byte, len(row))
		for i, v := range row {
			der[i] = byte(v)
		}
		ders = append(ders, der)
	}
	labels := map[string]string{}
	if ldata, lerr := os.ReadFile(punktfunkLabelsPath(stateDir)); lerr == nil {
		_ = json.Unmarshal(ldata, &labels)
	}
	for _, der := range ders {
		fp := fingerprintOf(der)
		clients = append(clients, trustedClient{
			Fingerprint: fp,
			Name:        labels[fp],
			CertDER:     der,
		})
	}
	return clients, nil
}

func writePunktfunkTrusted(stateDir string, clients []trustedClient) error {
	sorted := append([]trustedClient(nil), clients...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Fingerprint < sorted[j].Fingerprint })

	// []int per cert, not [][]byte: see readPunktfunkTrusted's doc comment
	// for why -- Go would otherwise silently write base64 strings instead
	// of the plain byte-number arrays the real punktfunk-host binary reads.
	rows := make([][]int, 0, len(sorted))
	labels := map[string]string{}
	for _, c := range sorted {
		row := make([]int, len(c.CertDER))
		for i, b := range c.CertDER {
			row[i] = int(b)
		}
		rows = append(rows, row)
		if c.Name != "" {
			labels[c.Fingerprint] = c.Name
		}
	}

	pairedBytes, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	if err := atomicWriteLocked(punktfunkPairedPath(stateDir), pairedBytes, 0o600); err != nil {
		return err
	}

	labelsBytes, err := json.Marshal(labels)
	if err != nil {
		return err
	}
	return atomicWriteLocked(punktfunkLabelsPath(stateDir), labelsBytes, 0o644)
}

func projectToPunktfunk(stateDir string, canonical []trustedClient, removed []string) (imported []trustedClient, err error) {
	existing, _ := readPunktfunkTrusted(stateDir)

	have := make(map[string]bool, len(canonical))
	for _, c := range canonical {
		have[c.Fingerprint] = true
	}
	isRemoved := make(map[string]bool, len(removed))
	for _, fp := range removed {
		isRemoved[fp] = true
	}

	merged := append([]trustedClient(nil), canonical...)
	for _, c := range existing {
		if isRemoved[c.Fingerprint] {
			continue
		}
		if !have[c.Fingerprint] {
			imported = append(imported, c)
			have[c.Fingerprint] = true
		}
		merged = mergeClient(merged, c)
	}

	if err := writePunktfunkTrusted(stateDir, merged); err != nil {
		return nil, err
	}
	return imported, nil
}

func provisionPunktfunkIdentity(stateDir string) error {
	certPEM, keyPEM, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		return err
	}
	certPath, keyPath, uuidPath := punktfunkIdentityPaths(stateDir)
	if err := atomicWriteLocked(certPath, certPEM, 0o644); err != nil {
		return err
	}
	if err := atomicWriteLocked(keyPath, keyPEM, 0o600); err != nil {
		return err
	}
	// Verbatim, dashes and all: punktfunk-host reads this file as an opaque
	// string and reports it as serverinfo's uniqueid, and Moonlight keys a
	// host by that exact string. Its own generator writes dash-less hex, so
	// handing it that form made Moonlight see punktfunk as a different PC
	// from Sunshine and rust-shine, which report the dashed uppercase form.
	return atomicWriteLocked(uuidPath, []byte(serverUUID), 0o644)
}
