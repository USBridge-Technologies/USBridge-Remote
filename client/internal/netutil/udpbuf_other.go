//go:build !linux || android

package netutil

import "fmt"

// The UDP receive-buffer cap is a Linux sysctl (see udpbuf_linux.go); elsewhere the
// client's own SO_RCVBUF request is honored and there is nothing to grant.

func UDPBufferMax() int { return 0 }

func UDPBufferOK() bool { return true }

func UDPBufferAdjustable() bool { return false }

func UDPBufferCommand() string { return "" }

func EnsureUDPBuffer() error {
	return fmt.Errorf("the UDP receive buffer cap is only adjustable on Linux")
}
