package guestauth

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPresetVFXTPN(t *testing.T) {
	p := PresetVFXTPN()
	if !p.Has(CapView) || !p.Has(CapAudio) || !p.Has(CapInput) || !p.Has(CapPenTablet) || !p.Has(CapDisplay) {
		t.Fatalf("expected core VFX caps enabled, got %#x", uint32(p.Caps))
	}
	if p.Has(CapClipToHost) || p.Has(CapClipFromHost) || p.Has(CapFileTransfer) || p.Has(CapDisks) {
		t.Fatalf("TPN strict preset must block clipboard, files, and disks; got %#x", uint32(p.Caps))
	}
	if !p.USB.AllowHID || !p.USB.AllowPenTablet {
		t.Fatalf("TPN strict preset must allow HID (incl. SpaceMouse) and Wacom pen tablet")
	}
	if p.USB.AllowMassStorage || p.USB.AllowOther {
		t.Fatalf("TPN strict preset must block Mass Storage and arbitrary USB")
	}
}

func TestLocalTokenAuthAndAudit(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	now := time.Now()
	sh, tok, err := store.AddLocal("vfx-artist-01", PresetVFXTPN(), 0, false, now)
	if err != nil {
		t.Fatalf("AddLocal: %v", err)
	}
	id, secret, ok := ParseLocalToken(tok)
	if !ok || id != sh.ID || secret == "" {
		t.Fatalf("ParseLocalToken failed: ok=%v id=%q", ok, id)
	}

	auth := NewAuthenticator(store, nil, func() string { return "hw" })
	ts := strconv.FormatInt(now.Unix(), 10)
	sig := CalculateLocalHMAC("GET", "/api/status", ts, nil, secret)
	p, err := auth.VerifyLocal("GET", "/api/status", ts, sig, id, nil)
	if err != nil {
		t.Fatalf("VerifyLocal: %v", err)
	}
	if !p.Local || p.Label != "vfx-artist-01" || !p.Policy.Has(CapPenTablet) || p.Policy.Has(CapClipFromHost) {
		t.Fatalf("unexpected principal: %+v", p)
	}
	// Same-second identical GETs with HMAC must succeed (clients poll multiple
	// endpoints per second; timestamp window provides replay bounding).
	if _, err := auth.VerifyLocal("GET", "/api/status", ts, sig, id, nil); err != nil {
		t.Fatalf("expected same-second GET to succeed, got %v", err)
	}
	// Revoked share must be rejected
	if err := store.Revoke(sh.ID, now); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	sig2 := CalculateLocalHMAC("GET", "/api/video/info", ts, nil, secret)
	if _, err := auth.VerifyLocal("GET", "/api/video/info", ts, sig2, id, nil); err != ErrRevoked {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}

	// Local JSONL audit log writes with 0600 permissions
	aw := NewLocalAuditWriter(dir)
	aw.Write(LocalAuditRecord{Event: "local_created", ShareID: sh.ID, Label: sh.Label})
	info, err := os.Stat(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("stat audit.log: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 audit.log, got %o", info.Mode().Perm())
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "audit.log"))
	if !strings.Contains(string(raw), `"local_created"`) {
		t.Fatalf("audit.log missing event: %s", raw)
	}
}
