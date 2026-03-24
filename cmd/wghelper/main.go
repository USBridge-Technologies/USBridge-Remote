//go:build darwin

package main

import (
	"fmt"
	"os"

	"usbridge-client/internal/service"
)

func main() {
	if err := service.RunDarwinWireGuardHelper(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

