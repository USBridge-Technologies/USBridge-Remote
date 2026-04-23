//go:build android || ios

package input

import (
	"github.com/sirupsen/logrus"
)

// GetRuneKeyCodeWithModifiers возвращает HID код и модификаторы для символа (Mobile версия)
func GetRuneKeyCodeWithModifiers(r rune) (int, int) {
	// Смарт-детект: если прилетела русская буква, значит раскладка ТОЧНО русская.
	// Это помогает, если Android IME сообщил неверный язык или не успел обновить его.
	if (r >= 'а' && r <= 'я') || (r >= 'А' && r <= 'Я') || r == 'ё' || r == 'Ё' {
		if !IsRussianLanguage() {
			logrus.Infof("⌨️ [MAP] Smart-detected Russian language from rune %q", string(r))
			SetCurrentLanguage("ru")
		}
	} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
		// Если прилетела латинская буква, а у нас стоит RU — возможно стоит переключить на EN?
		// Но осторожно: некоторые символы могут быть в обеих. Буквы — надежный признак.
		if IsRussianLanguage() {
			logrus.Infof("⌨️ [MAP] Smart-detected English language from rune %q", string(r))
			SetCurrentLanguage("en-US")
		}
	}

	isRu := IsRussianLanguage()
	keyCode := 0
	modifiers := 0

	// Если активен русский язык, используем маппинг для русской раскладки хоста.
	if isRu {
		mobileSymbolMap := map[rune]RuneKeyInfo{
			'.': {56, 0}, // В RU раскладке точка — это клавиша / (Key 56)
			',': {56, 2}, // В RU раскладке запятая — это Shift + / (Key 56)
			':': {35, 2}, // В RU раскладке двоеточие — это Shift + 6 (Key 35)
			';': {33, 2}, // В RU раскладке точка с запятой — это Shift + 4 (Key 33)
			'"': {31, 2}, // В RU раскладке кавычки — это Shift + 2 (Key 31)
			'?': {36, 2}, // В RU раскладке вопрос — это Shift + 7 (Key 36)
			'№': {32, 2}, // Клавиша # в US -> № в RU (Shift + 3)
		}

		if info, exists := mobileSymbolMap[r]; exists {
			keyCode = info.KeyCode
			modifiers = info.Modifiers
		}
	}

	if keyCode == 0 {
		// Если язык не русский (или символа нет в RU-карте), используем общую US-центричную карту.
		if info, exists := CommonRuneMap[r]; exists {
			keyCode = info.KeyCode
			modifiers = info.Modifiers
		}
	}

	// Если и там нет (например, русская буква при не-RU языке), пробуем конвертировать в латиницу.
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
