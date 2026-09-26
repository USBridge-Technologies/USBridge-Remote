package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadArtifact(t *testing.T) {
	body := []byte("verified update artifact")
	digest := sha256.Sum256(body)
	wantDigest := hex.EncodeToString(digest[:])
	var gotUserAgent string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotUserAgent = req.Header.Get("User-Agent")
		resp := response(http.StatusOK, string(body))
		resp.ContentLength = int64(len(body))
		return resp, nil
	})}
	type progress struct{ downloaded, total int64 }
	var updates []progress

	path, err := downloadArtifact(context.Background(), client, "https://example.test/agent", wantDigest, func(downloaded, total int64) {
		updates = append(updates, progress{downloaded, total})
	})
	if err != nil {
		t.Fatalf("downloadArtifact: %v", err)
	}
	defer os.Remove(path)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded artifact: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("downloaded bytes = %q, want %q", got, body)
	}
	if gotUserAgent != "USBridge-agent-updater" {
		t.Fatalf("User-Agent = %q", gotUserAgent)
	}
	if len(updates) < 2 || updates[0] != (progress{0, int64(len(body))}) || updates[len(updates)-1] != (progress{int64(len(body)), int64(len(body))}) {
		t.Fatalf("progress updates = %#v", updates)
	}
}

func TestDownloadArtifactRejectsHashMismatchAndRemovesTempFile(t *testing.T) {
	tempDir := t.TempDir()
	setSystemTempDir(t, tempDir)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "tampered"), nil
	})}

	if _, err := downloadArtifact(context.Background(), client, "https://example.test/agent", strings.Repeat("0", 64), nil); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("downloadArtifact mismatch error = %v", err)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("partial download was not removed: %v", entries)
	}
}

func TestDownloadArtifactErrors(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		transport roundTripFunc
		want      string
	}{
		{
			name: "transport",
			url:  "https://example.test/agent",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("offline")
			},
			want: "offline",
		},
		{
			name: "status",
			url:  "https://example.test/agent",
			transport: func(*http.Request) (*http.Response, error) {
				return response(http.StatusForbidden, "denied"), nil
			},
			want: "unexpected status 403",
		},
		{
			name: "body read",
			url:  "https://example.test/agent",
			transport: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: &errorReader{}, Header: make(http.Header)}, nil
			},
			want: "read failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := downloadArtifact(context.Background(), &http.Client{Transport: tt.transport}, tt.url, "", nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("downloadArtifact error = %v, want %q", err, tt.want)
			}
		})
	}

	if _, err := downloadArtifact(context.Background(), http.DefaultClient, "://bad-url", "", nil); err == nil {
		t.Fatal("downloadArtifact(invalid URL) returned nil error")
	}
}

type errorReader struct{}

func (*errorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (*errorReader) Close() error             { return nil }

func TestProgressWriterThrottlesReports(t *testing.T) {
	type progress struct{ downloaded, total int64 }
	var updates []progress
	w := &progressWriter{
		total: 10,
		onProgress: func(downloaded, total int64) {
			updates = append(updates, progress{downloaded, total})
		},
	}

	if n, err := w.Write([]byte("abc")); err != nil || n != 3 {
		t.Fatalf("first Write = %d, %v", n, err)
	}
	if len(updates) != 1 || updates[0] != (progress{3, 10}) {
		t.Fatalf("updates before interval = %#v", updates)
	}

	w.lastReport = time.Now().Add(time.Hour)
	if n, err := w.Write([]byte("de")); err != nil || n != 2 {
		t.Fatalf("second Write = %d, %v", n, err)
	}
	if len(updates) != 1 {
		t.Fatalf("throttled Write produced an update: %#v", updates)
	}

	w.lastReport = time.Now().Add(-progressInterval)
	if _, err := io.WriteString(w, "f"); err != nil {
		t.Fatal(err)
	}
	if len(updates) != 2 || updates[1] != (progress{6, 10}) {
		t.Fatalf("updates after interval = %#v", updates)
	}
}

func TestDownloadedFileUsesSystemTempDirectory(t *testing.T) {
	body := []byte("artifact")
	digest := sha256.Sum256(body)
	tempDir := t.TempDir()
	setSystemTempDir(t, tempDir)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, string(body)), nil
	})}

	path, err := downloadArtifact(context.Background(), client, "https://example.test/agent", hex.EncodeToString(digest[:]), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if filepath.Dir(path) != tempDir {
		t.Fatalf("download path = %q, want directory %q", path, tempDir)
	}
}

func setSystemTempDir(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, dir)
	}
}
