//go:build tools

package service

// This file is only needed to prevent go mod tidy from removing Tailscale CLI dependencies
import (
	_ "tailscale.com/cmd/tailscale/cli"
	_ "tailscale.com/cmd/tailscaled"
)
