//go:build android || ios

package input

// GetRuneKeyCodeWithModifiers возвращает HID код и модификаторы для символа (Mobile версия)
func GetRuneKeyCodeWithModifiers(r rune) (int, int) {
	// На мобильных устройствах при использовании системной клавиатуры (особенно русской)
	// возникает конфликт: символы . и , на Android имеют те же Unicode-коды, что и в латинице,
	// но на физической клавиатуре с русской раскладкой они находятся на других клавишах.
	// Если маппить их как в US-раскладке ( . -> Key 55, , -> Key 54), то на удаленной машине
	// с русской раскладкой вместо них печатаются буквы "ю" и "б".
	//
	// Здесь мы переопределяем маппинг для спецсимволов, чтобы они "оставались собой"
	// при активной русской раскладке на удаленной машине.
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
		return info.KeyCode, info.Modifiers
	}

	// Если символа нет в мобильной карте, используем общую US-центричную карту.
	// Это обеспечит работу латинских букв и остальных символов.
	if info, exists := CommonRuneMap[r]; exists {
		return info.KeyCode, info.Modifiers
	}

	// Если и там нет (например, русская буква), пробуем конвертировать в латиницу
	// для поиска физической клавиши (важно для правильной работы букв б, ю и т.д.)
	if latinRune, ok := mapRussianLayoutRuneToLatin(r); ok {
		if info, exists := CommonRuneMap[latinRune]; exists {
			return info.KeyCode, info.Modifiers
		}
	}

	return 0, 0
}
