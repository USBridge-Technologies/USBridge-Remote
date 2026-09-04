// Package hwid derives a stable identifier for the physical machine this
// agent is running on, used to bind desktop RustShine licenses/trials to
// hardware (see agent/internal/entitlement, and
// usbridge-entitlement-backend's src/desktopLicense.ts on the server side).
//
// Threat model, stated plainly: this is a software-only hardware id, not a
// TPM-backed attestation -- USBridge desktop installs cannot assume a TPM
// (or Secure Enclave, or any other hardware root of trust) is present or
// enabled, so this deliberately doesn't require one. That means it is NOT
// unforgeable: an attacker with local admin/root on their own machine can,
// in principle, read the same OS-level identifier this package reads and
// write it into their own copy (or a VM they fully control), producing a
// device with the same Get() result and therefore able to replay a token
// legitimately issued to someone else's hardware. This is the same
// trade-off essentially every desktop software license binding makes
// without a hardware security module to anchor to -- raising the bar
// against casual copying (a token file alone, copied to another machine,
// no longer works) without claiming to defeat a determined, technically
// sophisticated attacker who deliberately sets out to clone one specific
// machine's identity. Closing that last gap requires a hardware root of
// trust this deployment doesn't assume exists.
//
// What actually makes this hard to abuse in practice is less about any one
// signal being unspoofable and more about there being no incentive to try:
// the trial is one-time per hw_id *as tracked by the backend* (see
// desktopLicense.ts's issueOrRefreshTrial), so spoofing one's own id to
// look "new" only works by regenerating the OS-level identifiers this
// reads from (a fresh Windows install's MachineGuid, wiping /etc/machine-id,
// etc.) -- each of which is itself either a deliberate, deterrent-sized
// amount of effort (reinstalling an OS for a 7-day trial) or requires the
// same admin/root access that could just as easily be used to patch the
// binary's license check directly. This package's job is to make the
// *default*, no-special-effort path (copy a token file to another machine)
// not work, not to defend against a determined local-privilege attacker --
// nothing running as an unprivileged desktop app ever can, TPM or not.
package hwid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// idVersion is mixed into the hash so a future change to which raw
// OS-level identifiers Get() reads (or how they're combined) produces
// visibly different hw ids rather than silently colliding with the old
// scheme's output for some machine.
const idVersion = "usbhw1"

var (
	once      sync.Once
	cachedID  string
	cachedErr error
)

// Get returns this machine's hardware id: a stable (across app restarts and
// reinstalls, NOT across an OS reinstall or a cloned/re-imaged disk -- see
// rawMachineID's platform-specific doc comments for exactly what each OS's
// underlying identifier does and doesn't survive), opaque, 64-character hex
// string. Cached after the first successful call -- the underlying reads
// (registry, ioreg, a file) are cheap but there is no reason to repeat them
// on every entitlement check.
//
// Hashed (not returned raw): the raw OS identifiers this derives from
// (Windows' MachineGuid, macOS's IOPlatformUUID, Linux's /etc/machine-id)
// are themselves sometimes used by OTHER software as a machine identifier,
// and are visible to anything with local read access regardless -- hashing
// through idVersion and a fixed per-purpose label avoids handing out the
// raw value verbatim under a name ("USBridge hardware id") that makes it
// easy to correlate with whatever other purpose reads the same raw GUID.
func Get() (string, error) {
	once.Do(func() {
		raw, source, err := rawMachineID()
		if err != nil {
			cachedErr = fmt.Errorf("hwid: %w", err)
			return
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			cachedErr = fmt.Errorf("hwid: %s returned an empty identifier", source)
			return
		}
		sum := sha256.Sum256([]byte(idVersion + ":" + source + ":" + raw))
		cachedID = hex.EncodeToString(sum[:])
	})
	return cachedID, cachedErr
}

// readFileTrimmed is shared by the Linux backend (tries a couple of
// candidate paths) -- kept here rather than per-file since only one
// platform file needs it.
func readFileTrimmed(path string) (string, error) {
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
