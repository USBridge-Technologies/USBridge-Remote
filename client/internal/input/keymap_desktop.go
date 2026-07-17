//go:build !android && !ios

package input

// GetRuneKeyCodeWithModifiers returns the HID code and modifiers for a rune (Desktop version)
func GetRuneKeyCodeWithModifiers(r rune) (int, int) {
	// On desktop we use the standard (US-centric) map,
	// since input comes from a physical keyboard and a 1-to-1 mapping to keys matters.
	if info, exists := CommonRuneMap[r]; exists {
		return info.KeyCode, info.Modifiers
	}
	if latinRune, ok := mapRussianLayoutRuneToLatin(r); ok {
		if info, exists := CommonRuneMap[latinRune]; exists {
			return info.KeyCode, info.Modifiers
		}
	}
	return 0, 0
}
