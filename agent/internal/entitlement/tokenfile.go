package entitlement

import (
	"os"
	"path/filepath"
	"strings"
)

// TokenFilePath is where the current entitlement token is mirrored to disk
// for the RustShine process to pick up via its own --entitlement-file flag
// -- the Go agent's own cfg.EntitlementToken is the source of truth this
// file mirrors, not the other way around. Same directory StagePath already
// uses. Duplicated (not imported) by
// agent/internal/streamhost/rustshine_backend.go rather than referenced
// across packages -- mirrors download.go's binaryName() doing the same for
// the same reason (see that doc comment): this package stays self-contained,
// streamhost stays entitlement-agnostic.
func TokenFilePath(stateDir string) string {
	return filepath.Join(stateDir, "rustshine", "entitlement.token")
}

// WriteTokenFile atomically (over)writes the entitlement token file so
// whatever reads it always sees a complete token, never a half-written one
// -- reuses writeAtomic's same temp-file+rename discipline download.go's
// own staging already relies on. 0600: same trust level as
// config.Config's MasterKey field this token is cached alongside.
func WriteTokenFile(stateDir, token string) error {
	dest := TokenFilePath(stateDir)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return writeAtomic(dest, strings.NewReader(token), 0o600)
}
