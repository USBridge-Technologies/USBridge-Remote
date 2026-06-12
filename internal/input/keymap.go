package input

import (
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

var (
	currentLanguage   = "en-US"
	currentLanguageMu sync.RWMutex
)

// SetCurrentLanguage устанавливает текущий язык ввода (из Android IME)
func SetCurrentLanguage(lang string) {
	if lang == "" {
		lang = "en-US"
	}
	currentLanguageMu.Lock()
	defer currentLanguageMu.Unlock()
	if currentLanguage != lang {
		logrus.Infof("⌨️ [KEYMAP] Language changed: %q -> %q", currentLanguage, lang)
		currentLanguage = lang
	}
}

// IsRussianLanguage возвращает true, если текущий язык — русский
func IsRussianLanguage() bool {
	currentLanguageMu.RLock()
	lang := currentLanguage
	currentLanguageMu.RUnlock()

	isRu := strings.HasPrefix(strings.ToLower(lang), "ru")
	return isRu
}

// RuneKeyInfo информация о HID коде для символа
type RuneKeyInfo struct {
	KeyCode   int
	Modifiers int
}

// GetKeyCode возвращает HID код клавиши по имени Fyne
func GetKeyCode(keyName fyne.KeyName) int {
	keyNameStr := string(keyName)
	switch keyNameStr {
	case "LeftAlt":
		return 226 // Left Alt
	case "RightAlt":
		return 230 // Right Alt
	case "LeftShift":
		return 225 // Left Shift
	case "RightShift":
		return 229 // Right Shift
	case "LeftControl":
		return 224 // Left Control
	case "RightControl":
		return 228 // Right Control
	case "LeftSuper":
		return 227 // Left GUI (Windows/Command)
	case "RightSuper":
		return 231 // Right GUI
	}

	keyMap := map[fyne.KeyName]int{
		fyne.KeyA: 4, fyne.KeyB: 5, fyne.KeyC: 6, fyne.KeyD: 7, fyne.KeyE: 8,
		fyne.KeyF: 9, fyne.KeyG: 10, fyne.KeyH: 11, fyne.KeyI: 12, fyne.KeyJ: 13,
		fyne.KeyK: 14, fyne.KeyL: 15, fyne.KeyM: 16, fyne.KeyN: 17, fyne.KeyO: 18,
		fyne.KeyP: 19, fyne.KeyQ: 20, fyne.KeyR: 21, fyne.KeyS: 22, fyne.KeyT: 23,
		fyne.KeyU: 24, fyne.KeyV: 25, fyne.KeyW: 26, fyne.KeyX: 27, fyne.KeyY: 28, fyne.KeyZ: 29,

		fyne.Key1: 30, fyne.Key2: 31, fyne.Key3: 32, fyne.Key4: 33, fyne.Key5: 34,
		fyne.Key6: 35, fyne.Key7: 36, fyne.Key8: 37, fyne.Key9: 38, fyne.Key0: 39,

		fyne.KeyReturn: 40, fyne.KeyEscape: 41, fyne.KeyBackspace: 42, fyne.KeyTab: 43, fyne.KeySpace: 44,
		fyne.KeyMinus: 45, fyne.KeyEqual: 46, fyne.KeyLeftBracket: 47, fyne.KeyRightBracket: 48,
		fyne.KeyBackslash: 49, fyne.KeySemicolon: 51, fyne.KeyApostrophe: 52, fyne.KeyBackTick: 53,
		fyne.KeyComma: 54, fyne.KeyPeriod: 55, fyne.KeySlash: 56,

		fyne.KeyF1: 58, fyne.KeyF2: 59, fyne.KeyF3: 60, fyne.KeyF4: 61,
		fyne.KeyF5: 62, fyne.KeyF6: 63, fyne.KeyF7: 64, fyne.KeyF8: 65,
		fyne.KeyF9: 66, fyne.KeyF10: 67, fyne.KeyF11: 68, fyne.KeyF12: 69,

		fyne.KeyInsert: 73, fyne.KeyHome: 74, fyne.KeyPageUp: 75,
		fyne.KeyDelete: 76, fyne.KeyEnd: 77, fyne.KeyPageDown: 78,
		fyne.KeyRight: 79, fyne.KeyLeft: 80, fyne.KeyDown: 81, fyne.KeyUp: 82,
	}

	if code, exists := keyMap[keyName]; exists {
		return code
	}
	return 0
}

// GetKeyCodeFromPhysical возвращает HID код по физическому коду (пока не используется)
func GetKeyCodeFromPhysical(physical fyne.HardwareKey) int {
	return 0
}

// GetVKCodeFromScanCode maps a Windows PS/2 scan code (as reported by GLFW) to a
// Windows Virtual Key code. Used as a fallback when the Fyne key name is KeyUnknown
// (e.g. letter keys on non-Latin keyboard layouts like Russian or Ukrainian).
// Scan codes are layout-independent physical positions, so this correctly maps
// ЙЦУКЕН → QWERTY, WASD positions, etc. regardless of the active IME layout.
func GetVKCodeFromScanCode(scanCode int) int16 {
	// Standard PS/2 Set-1 scan codes for US QWERTY.
	// Extended keys (arrow keys, ins/del/home/end, pg-up/dn, right-ctrl/alt, numpad-/)
	// have bit 8 set by GLFW: extended scan code = base + 0x100.
	switch scanCode {
	// Row above letters
	case 0x02: return 0x31 // 1
	case 0x03: return 0x32 // 2
	case 0x04: return 0x33 // 3
	case 0x05: return 0x34 // 4
	case 0x06: return 0x35 // 5
	case 0x07: return 0x36 // 6
	case 0x08: return 0x37 // 7
	case 0x09: return 0x38 // 8
	case 0x0A: return 0x39 // 9
	case 0x0B: return 0x30 // 0
	case 0x0C: return 0xBD // - (VK_OEM_MINUS)
	case 0x0D: return 0xBB // = (VK_OEM_PLUS)
	// Top letter row  Q W E R T Y U I O P [ ]
	case 0x10: return 0x51 // Q
	case 0x11: return 0x57 // W
	case 0x12: return 0x45 // E
	case 0x13: return 0x52 // R
	case 0x14: return 0x54 // T
	case 0x15: return 0x59 // Y
	case 0x16: return 0x55 // U
	case 0x17: return 0x49 // I
	case 0x18: return 0x4F // O
	case 0x19: return 0x50 // P
	case 0x1A: return 0xDB // [ (VK_OEM_4)
	case 0x1B: return 0xDD // ] (VK_OEM_6)
	// Middle letter row  A S D F G H J K L ; '
	case 0x1E: return 0x41 // A
	case 0x1F: return 0x53 // S
	case 0x20: return 0x44 // D
	case 0x21: return 0x46 // F
	case 0x22: return 0x47 // G
	case 0x23: return 0x48 // H
	case 0x24: return 0x4A // J
	case 0x25: return 0x4B // K
	case 0x26: return 0x4C // L
	case 0x27: return 0xBA // ; (VK_OEM_1)
	case 0x28: return 0xDE // ' (VK_OEM_7)
	case 0x29: return 0xC0 // ` (VK_OEM_3)
	case 0x2B: return 0xDC // \ (VK_OEM_5)
	// Bottom letter row  Z X C V B N M , . /
	case 0x2C: return 0x5A // Z
	case 0x2D: return 0x58 // X
	case 0x2E: return 0x43 // C
	case 0x2F: return 0x56 // V
	case 0x30: return 0x42 // B
	case 0x31: return 0x4E // N
	case 0x32: return 0x4D // M
	case 0x33: return 0xBC // , (VK_OEM_COMMA)
	case 0x34: return 0xBE // . (VK_OEM_PERIOD)
	case 0x35: return 0xBF // / (VK_OEM_2)
	// Extended: navigation cluster (scan + 0x100)
	case 0x147: return 0x24 // Home
	case 0x148: return 0x26 // Up
	case 0x149: return 0x21 // Page Up
	case 0x14B: return 0x25 // Left
	case 0x14D: return 0x27 // Right
	case 0x14F: return 0x23 // End
	case 0x150: return 0x28 // Down
	case 0x151: return 0x22 // Page Down
	case 0x152: return 0x2D // Insert
	case 0x153: return 0x2E // Delete
	// Extended: modifier keys
	case 0x11D: return 0xA3 // Right Ctrl
	case 0x138: return 0xA5 // Right Alt
	// Numpad /
	case 0x135: return 0x6F // VK_DIVIDE
	}
	return 0
}

// GetVKCode returns the Windows Virtual Key code for a Fyne key name.
// Used for routing keyboard events through Moonlight (LiSendKeyboardEvent).
func GetVKCode(keyName fyne.KeyName) int16 {
	keyNameStr := string(keyName)
	switch keyNameStr {
	case "LeftControl":
		return 0xA2
	case "RightControl":
		return 0xA3
	case "LeftShift":
		return 0xA0
	case "RightShift":
		return 0xA1
	case "LeftAlt":
		return 0xA4
	case "RightAlt":
		return 0xA5
	case "LeftSuper":
		return 0x5B
	case "RightSuper":
		return 0x5C
	}

	vkMap := map[fyne.KeyName]int16{
		fyne.KeyA: 0x41, fyne.KeyB: 0x42, fyne.KeyC: 0x43, fyne.KeyD: 0x44, fyne.KeyE: 0x45,
		fyne.KeyF: 0x46, fyne.KeyG: 0x47, fyne.KeyH: 0x48, fyne.KeyI: 0x49, fyne.KeyJ: 0x4A,
		fyne.KeyK: 0x4B, fyne.KeyL: 0x4C, fyne.KeyM: 0x4D, fyne.KeyN: 0x4E, fyne.KeyO: 0x4F,
		fyne.KeyP: 0x50, fyne.KeyQ: 0x51, fyne.KeyR: 0x52, fyne.KeyS: 0x53, fyne.KeyT: 0x54,
		fyne.KeyU: 0x55, fyne.KeyV: 0x56, fyne.KeyW: 0x57, fyne.KeyX: 0x58, fyne.KeyY: 0x59, fyne.KeyZ: 0x5A,

		fyne.Key0: 0x30, fyne.Key1: 0x31, fyne.Key2: 0x32, fyne.Key3: 0x33, fyne.Key4: 0x34,
		fyne.Key5: 0x35, fyne.Key6: 0x36, fyne.Key7: 0x37, fyne.Key8: 0x38, fyne.Key9: 0x39,

		fyne.KeyReturn:    0x0D,
		fyne.KeyEscape:    0x1B,
		fyne.KeyBackspace: 0x08,
		fyne.KeyTab:       0x09,
		fyne.KeySpace:     0x20,
		fyne.KeyDelete:    0x2E,
		fyne.KeyInsert:    0x2D,
		fyne.KeyHome:      0x24,
		fyne.KeyEnd:       0x23,
		fyne.KeyPageUp:    0x21,
		fyne.KeyPageDown:  0x22,
		fyne.KeyLeft:      0x25,
		fyne.KeyUp:        0x26,
		fyne.KeyRight:     0x27,
		fyne.KeyDown:      0x28,

		fyne.KeyF1: 0x70, fyne.KeyF2: 0x71, fyne.KeyF3: 0x72, fyne.KeyF4: 0x73,
		fyne.KeyF5: 0x74, fyne.KeyF6: 0x75, fyne.KeyF7: 0x76, fyne.KeyF8: 0x77,
		fyne.KeyF9: 0x78, fyne.KeyF10: 0x79, fyne.KeyF11: 0x7A, fyne.KeyF12: 0x7B,

		fyne.KeyMinus:        0xBD,
		fyne.KeyEqual:        0xBB,
		fyne.KeyLeftBracket:  0xDB,
		fyne.KeyRightBracket: 0xDD,
		fyne.KeyBackslash:    0xDC,
		fyne.KeySemicolon:    0xBA,
		fyne.KeyApostrophe:   0xDE,
		fyne.KeyBackTick:     0xC0,
		fyne.KeyComma:        0xBC,
		fyne.KeyPeriod:       0xBE,
		fyne.KeySlash:        0xBF,
	}

	if code, ok := vkMap[keyName]; ok {
		return code
	}
	return 0
}

// IsPrintableKey возвращает true для клавиш, которые дают символ (TypedRune).
func IsPrintableKey(keyName fyne.KeyName) bool {
	switch keyName {
	case fyne.KeyA, fyne.KeyB, fyne.KeyC, fyne.KeyD, fyne.KeyE, fyne.KeyF,
		fyne.KeyG, fyne.KeyH, fyne.KeyI, fyne.KeyJ, fyne.KeyK, fyne.KeyL,
		fyne.KeyM, fyne.KeyN, fyne.KeyO, fyne.KeyP, fyne.KeyQ, fyne.KeyR,
		fyne.KeyS, fyne.KeyT, fyne.KeyU, fyne.KeyV, fyne.KeyW, fyne.KeyX,
		fyne.KeyY, fyne.KeyZ:
		return true
	case fyne.Key0, fyne.Key1, fyne.Key2, fyne.Key3, fyne.Key4,
		fyne.Key5, fyne.Key6, fyne.Key7, fyne.Key8, fyne.Key9:
		return true
	case fyne.KeySpace:
		return true
	case fyne.KeyMinus, fyne.KeyEqual, fyne.KeyLeftBracket, fyne.KeyRightBracket,
		fyne.KeyBackslash, fyne.KeySemicolon, fyne.KeyApostrophe, fyne.KeyBackTick,
		fyne.KeyComma, fyne.KeyPeriod, fyne.KeySlash:
		return true
	default:
		return false
	}
}

func mapRussianLayoutRuneToLatin(r rune) (rune, bool) {
	layoutMap := map[rune]rune{
		'ё': '`', 'Ё': '~',
		'й': 'q', 'Й': 'Q',
		'ц': 'w', 'Ц': 'W',
		'у': 'e', 'У': 'E',
		'к': 'r', 'К': 'R',
		'е': 't', 'Е': 'T',
		'н': 'y', 'Н': 'Y',
		'г': 'u', 'Г': 'U',
		'ш': 'i', 'Ш': 'I',
		'щ': 'o', 'Щ': 'O',
		'з': 'p', 'З': 'P',
		'х': '[', 'Х': '{',
		'ъ': ']', 'Ъ': '}',
		'ф': 'a', 'Ф': 'A',
		'ы': 's', 'Ы': 'S',
		'в': 'd', 'В': 'D',
		'а': 'f', 'А': 'F',
		'п': 'g', 'П': 'G',
		'р': 'h', 'Р': 'H',
		'о': 'j', 'О': 'J',
		'л': 'k', 'Л': 'K',
		'д': 'l', 'Д': 'L',
		'ж': ';', 'Ж': ':',
		'э': '\'', 'Э': '"',
		'я': 'z', 'Я': 'Z',
		'ч': 'x', 'Ч': 'X',
		'с': 'c', 'С': 'C',
		'м': 'v', 'М': 'V',
		'и': 'b', 'И': 'B',
		'т': 'n', 'Т': 'N',
		'ь': 'm', 'Ь': 'M',
		'б': ',', 'Б': '<',
		'ю': '.', 'Ю': '>',
		'№': '#',
	}
	latin, ok := layoutMap[r]
	return latin, ok
}

// CommonRuneMap общая карта символов для US раскладки
var CommonRuneMap = map[rune]RuneKeyInfo{
	'a': {4, 0}, 'b': {5, 0}, 'c': {6, 0}, 'd': {7, 0}, 'e': {8, 0},
	'f': {9, 0}, 'g': {10, 0}, 'h': {11, 0}, 'i': {12, 0}, 'j': {13, 0},
	'k': {14, 0}, 'l': {15, 0}, 'm': {16, 0}, 'n': {17, 0}, 'o': {18, 0},
	'p': {19, 0}, 'q': {20, 0}, 'r': {21, 0}, 's': {22, 0}, 't': {23, 0},
	'u': {24, 0}, 'v': {25, 0}, 'w': {26, 0}, 'x': {27, 0}, 'y': {28, 0}, 'z': {29, 0},

	'A': {4, 2}, 'B': {5, 2}, 'C': {6, 2}, 'D': {7, 2}, 'E': {8, 2},
	'F': {9, 2}, 'G': {10, 2}, 'H': {11, 2}, 'I': {12, 2}, 'J': {13, 2},
	'K': {14, 2}, 'L': {15, 2}, 'M': {16, 2}, 'N': {17, 2}, 'O': {18, 2},
	'P': {19, 2}, 'Q': {20, 2}, 'R': {21, 2}, 'S': {22, 2}, 'T': {23, 2},
	'U': {24, 2}, 'V': {25, 2}, 'W': {26, 2}, 'X': {27, 2}, 'Y': {28, 2}, 'Z': {29, 2},

	'1': {30, 0}, '2': {31, 0}, '3': {32, 0}, '4': {33, 0}, '5': {34, 0},
	'6': {35, 0}, '7': {36, 0}, '8': {37, 0}, '9': {38, 0}, '0': {39, 0},

	' ': {44, 0}, '-': {45, 0}, '=': {46, 0}, '[': {47, 0}, ']': {48, 0},
	'\\': {49, 0}, ';': {51, 0}, '\'': {52, 0}, '`': {53, 0}, ',': {54, 0},
	'.': {55, 0}, '/': {56, 0},
	'\n': {40, 0}, '\r': {40, 0}, // Enter
	'\t': {43, 0}, // Tab

	'×': {37, 2}, '÷': {56, 0}, // умножение, деление (как * и /)

	'!': {30, 2}, '@': {31, 2}, '#': {32, 2}, '$': {33, 2}, '%': {34, 2},
	'^': {35, 2}, '&': {36, 2}, '*': {37, 2}, '(': {38, 2}, ')': {39, 2},
	'_': {45, 2}, '+': {46, 2}, '{': {47, 2}, '}': {48, 2}, '|': {49, 2},
	':': {51, 2}, '"': {52, 2}, '~': {53, 2}, '<': {54, 2}, '>': {55, 2}, '?': {56, 2},
}
