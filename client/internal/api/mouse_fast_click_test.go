package api

import "testing"

func TestLumaGridChangePercent(t *testing.T) {
	mk := func(v uint8) *LumaGrid {
		g := &LumaGrid{W: 16, H: 16, GW: 4, GH: 4, Pix: make([]uint8, 16)}
		for i := range g.Pix {
			g.Pix[i] = v
		}
		return g
	}
	if got := LumaGridChangePercent(mk(100), mk(100)); got != 0 {
		t.Errorf("identical grids = %v, want 0", got)
	}
	if got := LumaGridChangePercent(mk(100), mk(105)); got != 0 {
		t.Errorf("delta below threshold = %v, want 0", got)
	}
	if got := LumaGridChangePercent(mk(0), mk(255)); got != 100 {
		t.Errorf("full change = %v, want 100", got)
	}
	half := mk(0)
	for i := 0; i < 8; i++ {
		half.Pix[i] = 200
	}
	if got := LumaGridChangePercent(mk(0), half); got != 50 {
		t.Errorf("half change = %v, want 50", got)
	}
	small := &LumaGrid{W: 8, H: 8, GW: 2, GH: 2, Pix: make([]uint8, 4)}
	if got := LumaGridChangePercent(mk(0), small); got != 100 {
		t.Errorf("size mismatch = %v, want 100", got)
	}
	if got := LumaGridChangePercent(nil, mk(0)); got != 100 {
		t.Errorf("nil grid = %v, want 100", got)
	}
}

func TestSendFastClickRejectsBadArgs(t *testing.T) {
	one, bad := 1, 9
	if _, err := sendFastClick(nil, nil, false, nil, &one, &one, &one, nil); err == nil {
		t.Error("missing x must error before touching the client")
	}
	zero := 0
	if _, err := sendFastClick(nil, nil, false, &one, &one, &zero, &one, nil); err == nil {
		t.Error("zero screen width must error")
	}
	if _, err := sendFastClick(nil, nil, false, &one, &one, &one, &one, &bad); err == nil {
		t.Error("invalid button must error")
	}
}
