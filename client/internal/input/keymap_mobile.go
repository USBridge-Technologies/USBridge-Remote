//go:build android || ios

package input

import (
	"github.com/sirupsen/logrus"
)

// GetRuneKeyCodeWithModifiers returns the HID code and modifiers for a rune (Mobile version)
func GetRuneKeyCodeWithModifiers(r rune) (int, int) {
	// Smart detection: if a Cyrillic letter comes in, the layout is DEFINITELY Russian.
	// This helps when the Android IME reports the wrong language or hasn't updated it yet.
	if (r >= 'а' && r <= 'я') || (r >= 'А' && r <= 'Я') || r == 'ё' || r == 'Ё' {
		if !IsRussianLanguage() {
			logrus.Infof("⌨️ [MAP] Smart-detected Russian language from rune %q", string(r))
			SetCurrentLanguage("ru")
		}
	} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
		// If a Latin letter comes in while we're set to RU — maybe we should switch to EN?
		// But be careful: some characters can belong to both. Letters are a reliable signal.
		if IsRussianLanguage() {
			logrus.Infof("⌨️ [MAP] Smart-detected English language from rune %q", string(r))
			SetCurrentLanguage("en-US")
		}
	}

	isRu := IsRussianLanguage()
	keyCode := 0
	modifiers := 0

	// If Russian is the active language, use the mapping for the host's Russian layout.
	if isRu {
		mobileSymbolMap := map[rune]RuneKeyInfo{
			'.': {56, 0}, // In the RU layout, period is the / key (Key 56)
			',': {56, 2}, // In the RU layout, comma is Shift + / (Key 56)
			':': {35, 2}, // In the RU layout, colon is Shift + 6 (Key 35)
			';': {33, 2}, // In the RU layout, semicolon is Shift + 4 (Key 33)
			'"': {31, 2}, // In the RU layout, quote is Shift + 2 (Key 31)
			'?': {36, 2}, // In the RU layout, question mark is Shift + 7 (Key 36)
			'№': {32, 2}, // The # key in US -> № in RU (Shift + 3)
		}

		if info, exists := mobileSymbolMap[r]; exists {
			keyCode = info.KeyCode
			modifiers = info.Modifiers
		}
	}

	if keyCode == 0 {
		// If the language isn't Russian (or the rune isn't in the RU map), use the general US-centric map.
		if info, exists := CommonRuneMap[r]; exists {
			keyCode = info.KeyCode
			modifiers = info.Modifiers
		}
	}

	// If it's still not found (e.g. a Cyrillic letter while the language isn't RU), try converting to Latin.
	if keyCode == 0 {
		if latinRune, ok := mapRussianLayoutRuneToLatin(r); ok {
			if info, exists := CommonRuneMap[latinRune]; exists {
				keyCode = info.KeyCode
				modifiers = info.Modifiers
			}
		}
	}

	if keyCode != 0 {
		logrus.Debugf("⌨️ [MAP] rune=%q (%U) -> HID=%d mod=%d (isRu=%v)", string(r), r, keyCode, modifiers, isRu)
	} else {
		logrus.Warnf("⌨️ [MAP] FAILED to map rune=%q (%U) (isRu=%v)", string(r), r, isRu)
	}

	return keyCode, modifiers
}
