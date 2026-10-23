package input

import (
	"fmt"
	"strings"
)

// klidFromHKL: an HKL's low word is the language, its high word the layout:
// the language again for a primary layout (040E040E -> 0000040E), a device
// ID 0xFxxx for a variant, found by its "Layout Id" in the registry.
func klidFromHKL(hkl uint32, variant func(layoutID uint16) string) string {
	hi := uint16(hkl >> 16)
	if hi&0xF000 == 0xF000 {
		return variant(hi & 0x0FFF)
	}
	if hi == 0 {
		hi = uint16(hkl)
	}
	return fmt.Sprintf("%08X", uint32(hi))
}

// layoutForLanguage maps an input language tag ("hu-HU", "fr_CA", "pt-BR")
// to the usual keyboard layout for it, "" when there is no table for it.
func layoutForLanguage(tag string) string {
	tag = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"))
	lang, region, _ := strings.Cut(tag, "-")
	switch lang + "-" + region {
	case "en-gb", "en-ie":
		return "00000809"
	case "de-ch":
		return "00000807"
	case "fr-be":
		return "0000080C"
	case "fr-ch":
		return "0000100C"
	case "fr-ca":
		return "00001009"
	case "pt-br":
		return "00000416"
	case "es-es", "es-":
		return "0000040A"
	}
	if lang == "es" {
		return "0000080A" // every other Spanish region uses Latin American
	}
	return languageLayouts[lang]
}

var languageLayouts = map[string]string{
	"en": "00000409", "hu": "0000040E", "de": "00000407", "fr": "0000040C",
	"it": "00000410", "pt": "00000816", "nl": "00000413", "da": "00000406",
	"nb": "00000414", "no": "00000414", "nn": "00000414", "sv": "0000041D",
	"fi": "0000040B", "is": "0000040F", "pl": "00000415", "cs": "00000405",
	"sk": "0000041B", "sl": "00000424", "hr": "0000041A", "ro": "00010418",
	"tr": "0000041F", "et": "00000425", "lv": "00000426", "lt": "00000427",
	"el": "00000408", "ru": "00000419", "uk": "00000422", "be": "00000423",
	"bg": "00000402", "kk": "0000043F",
}
