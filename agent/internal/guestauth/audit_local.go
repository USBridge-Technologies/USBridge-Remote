package guestauth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const auditMaxBytes = 10 * 1024 * 1024 // 10 MiB before simple single-file rotation

// LocalAuditRecord is one JSONL line written to <stateDir>/audit.log.
// Designed for TPN compliance and offline SIEM ingestion without cloud access.
type LocalAuditRecord struct {
	Time    string `json:"time"`
	TS      int64  `json:"ts"`
	Event   string `json:"event"`
	ShareID string `json:"share_id,omitempty"`
	Label   string `json:"label,omitempty"`
	Email   string `json:"email,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// LocalAuditWriter appends structured JSONL audit records to <stateDir>/audit.log
// with 0600 file permissions.
type LocalAuditWriter struct {
	mu   sync.Mutex
	path string
}

// NewLocalAuditWriter creates a local JSONL audit writer under stateDir.
func NewLocalAuditWriter(stateDir string) *LocalAuditWriter {
	return &LocalAuditWriter{path: filepath.Join(stateDir, "audit.log")}
}

// Path returns the log file path.
func (w *LocalAuditWriter) Path() string {
	if w == nil {
		return ""
	}
	return w.path
}

// Write appends one record, rotating audit.log -> audit.log.1 if it exceeds 10 MiB.
func (w *LocalAuditWriter) Write(rec LocalAuditRecord) {
	if w == nil || w.path == "" {
		return
	}
	now := time.Now().UTC()
	if rec.TS == 0 {
		rec.TS = now.Unix()
	}
	if rec.Time == "" {
		rec.Time = time.Unix(rec.TS, 0).UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	line = append(line, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	if info, err := os.Stat(w.path); err == nil && info.Size() >= auditMaxBytes {
		_ = os.Rename(w.path, w.path+".1")
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(line)
	_ = f.Close()
}
