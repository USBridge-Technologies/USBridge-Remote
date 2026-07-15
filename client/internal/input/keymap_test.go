package input

import (
	"testing"

	"fyne.io/fyne/v2"
)

func TestGetKeyCodePrintablePunctuation(t *testing.T) {
	tests := map[fyne.KeyName]int{
		fyne.KeyMinus:        45,
		fyne.KeyEqual:        46,
		fyne.KeyLeftBracket:  47,
		fyne.KeyRightBracket: 48,
		fyne.KeyBackslash:    49,
		fyne.KeySemicolon:    51,
		fyne.KeyApostrophe:   52,
		fyne.KeyBackTick:     53,
		fyne.KeyComma:        54,
		fyne.KeyPeriod:       55,
		fyne.KeySlash:        56,
	}

	for keyName, want := range tests {
		if got := GetKeyCode(keyName); got != want {
			t.Fatalf("GetKeyCode(%q) = %d, want %d", keyName, got, want)
		}
	}
}
