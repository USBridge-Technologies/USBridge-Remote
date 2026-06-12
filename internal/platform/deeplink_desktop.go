//go:build !android
// +build !android

package platform

import (
	"os"
	"strings"
)

// GetIntentDataURI on desktop checks os.Args for a usbridge:// URL passed as a command-line argument.
// This allows launching the app with a deeplink URL on Windows/macOS/Linux for testing.
func GetIntentDataURI() (string, error) {
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "usbridge://") {
			return arg, nil
		}
	}
	return "", nil
}
