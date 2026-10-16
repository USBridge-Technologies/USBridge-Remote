//go:build !windows

package adminapi

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"time"
)

func socketPath(stateDir string) string {
	return filepath.Join(stateDir, "admin.sock")
}

// listen binds the admin unix socket, owner-only. A stale socket file left
// behind by a previous crash makes a fresh Listen fail with "address already
// in use" even though nothing is listening on it -- remove it first. If
// another instance actually IS live, the caller already probed for that via
// Dial before ever reaching here (see app.Start), so this is safe.
func listen(path string) (net.Listener, error) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func dial(ctx context.Context, path string) (net.Conn, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	return d.DialContext(ctx, "unix", path)
}

func cleanup(path string) { _ = os.Remove(path) }
