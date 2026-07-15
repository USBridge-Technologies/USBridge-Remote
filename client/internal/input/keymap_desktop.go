//go:build !android && !ios

package input

// GetRuneKeyCodeWithModifiers возвращает HID код и модификаторы для символа (Desktop версия)
func GetRuneKeyCodeWithModifiers(r rune) (int, int) {
	// На десктопе используем стандартную карту (US-центричную),
	// так как ввод идет с физической клавиатуры и важна 1-к-1 привязка к клавишам.
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
