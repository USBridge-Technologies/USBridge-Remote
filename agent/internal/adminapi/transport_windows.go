//go:build windows

package adminapi

import (
	"context"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// PipeName is the admin endpoint on Windows: one fixed named pipe per
// machine, whoever owns the engine (the LocalSystem service, or a GUI that
// started its own engine because no service is installed).
//
// It used to be an AF_UNIX socket under the engine's state dir. For the
// service that is C:\Windows\System32\config\systemprofile\..., which a
// standard user cannot even traverse, so a window launched from the Start
// menu failed "connect: An attempt was made to access a socket in a way
// forbidden by its access permissions" and exited without a word
// (2026-10-16); only the tray helper the service started under SYSTEM's own
// token could attach. A pipe has no directory to traverse and carries its
// own DACL, so the user's own window (and tray) can attach as the user.
const PipeName = `\\.\pipe\usbridge-agent-admin`

// pipeSDDL: SYSTEM and Administrators full control, interactive users
// (local console and RDP logons -- not network/service logons) read/write,
// which is what connecting needs. Protected (P): nothing inherited.
const pipeSDDL = "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)"

func socketPath(string) string { return PipeName }

// listen creates the pipe's first instance; a second engine on the machine
// gets ERROR_ACCESS_DENIED here instead of silently sharing the name.
func listen(path string) (net.Listener, error) {
	return winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: pipeSDDL,
		InputBufferSize:    64 << 10,
		OutputBufferSize:   64 << 10,
	})
}

func dial(ctx context.Context, path string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return winio.DialPipeContext(ctx, path)
}

func cleanup(string) {}
