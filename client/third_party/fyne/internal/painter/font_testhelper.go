//go:build ci || test

package painter

import "github.com/go-text/typesetting/fontscan"

func loadSystemFonts(fm *fontscan.FontMap) error {
	return nil
}
