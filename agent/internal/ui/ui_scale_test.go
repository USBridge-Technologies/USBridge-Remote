package ui

import "testing"

func TestUIScaleLabels(t *testing.T) {
	if uiScaleLabel(1) != "100%" || uiScaleLabel(1.25) != "125%" || uiScaleLabel(1.5) != "150%" || uiScaleLabel(1.75) != "175%" {
		t.Fatalf("labels: %s %s %s %s", uiScaleLabel(1), uiScaleLabel(1.25), uiScaleLabel(1.5), uiScaleLabel(1.75))
	}
	v, ok := uiScaleFromLabel("150%")
	if !ok || v != 1.5 {
		t.Fatalf("150%%: %v %v", v, ok)
	}
	if _, ok := uiScaleFromLabel("200%"); ok {
		t.Fatal("200% should not be a step")
	}
}
