//go:build tools

package service

// Этот файл нужен только для того, чтобы go mod tidy не удалял зависимости Tailscale CLI
import (
	_ "tailscale.com/cmd/tailscale/cli"
	_ "tailscale.com/cmd/tailscaled"
)
