//go:build rustshine

package streamhost

import (
	"context"
	"time"
)

// execTimeout is a small shared helper for the rustshine device-listing
// files (Linux/Windows) that shell out to gamestream-server
// --list-capture-devices and need a bounded wait.
func execTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
