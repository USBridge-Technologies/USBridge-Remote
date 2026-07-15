package controller

import (
	"testing"

	"fyne.io/fyne/v2"
)

func TestModifierMaskForKeyName(t *testing.T) {
	tests := []struct {
		name fyne.KeyName
		want int32
	}{
		{name: fyne.KeyName("LeftControl"), want: 1},
		{name: fyne.KeyName("RightControl"), want: 1},
		{name: fyne.KeyName("LeftShift"), want: 2},
		{name: fyne.KeyName("RightShift"), want: 2},
		{name: fyne.KeyName("LeftAlt"), want: 4},
		{name: fyne.KeyName("RightAlt"), want: 4},
		{name: fyne.KeyName("LeftSuper"), want: 8},
		{name: fyne.KeyName("RightSuper"), want: 8},
		{name: fyne.KeyA, want: 0},
	}

	for _, tt := range tests {
		if got := modifierMaskForKeyName(tt.name); got != tt.want {
			t.Fatalf("modifierMaskForKeyName(%q) = %d, want %d", tt.name, got, tt.want)
		}
	}
}
