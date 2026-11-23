package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdminPINHashAndVerify(t *testing.T) {
	if _, err := HashAdminPIN("123"); err == nil {
		t.Fatal("expected error for weak PIN < 4 chars")
	}
	hash, err := HashAdminPIN("482910")
	if err != nil {
		t.Fatalf("HashAdminPIN: %v", err)
	}
	if !VerifyAdminPIN(hash, "482910") {
		t.Fatal("expected valid PIN to verify")
	}
	if VerifyAdminPIN(hash, "000000") {
		t.Fatal("expected wrong PIN to fail")
	}
}

func TestConfigSavePermissions0600(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(p, Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected config.yaml mode 0600, got %o", info.Mode().Perm())
	}
}
