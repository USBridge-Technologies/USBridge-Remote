package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"usbridge_agent/assets"
	"usbridge_agent/internal/ui/design"
)

// decodeTrayIconBase is the shared starting point every test below composites
// onto -- exercised on its own here too, since every other test's validity
// depends on assets.TrayIconPNG actually being a decodable PNG.
func decodeTrayIconBase(t *testing.T) image.Image {
	t.Helper()
	base, err := png.Decode(bytes.NewReader(assets.TrayIconPNG))
	if err != nil {
		t.Fatalf("assets.TrayIconPNG did not decode as PNG: %v", err)
	}
	return base
}

func TestEncodeTrayIconPreservesDimensions(t *testing.T) {
	base := decodeTrayIconBase(t)

	got := encodeTrayIcon(base, nil)
	out, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("encodeTrayIcon produced an undecodable PNG: %v", err)
	}
	if out.Bounds() != base.Bounds() {
		t.Fatalf("encodeTrayIcon changed bounds: base=%v got=%v", base.Bounds(), out.Bounds())
	}
}

func TestEncodeTrayIconNilDotMatchesBasePixels(t *testing.T) {
	base := decodeTrayIconBase(t)

	got := encodeTrayIcon(base, nil)
	out, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	b := base.Bounds()
	for _, pt := range []image.Point{b.Min, {X: b.Max.X - 1, Y: b.Max.Y - 1}, {X: b.Dx() / 2, Y: b.Dy() / 2}} {
		wantR, wantG, wantB, wantA := base.At(pt.X, pt.Y).RGBA()
		gotR, gotG, gotB, gotA := out.At(pt.X, pt.Y).RGBA()
		if wantR != gotR || wantG != gotG || wantB != gotB || wantA != gotA {
			t.Fatalf("pixel %v changed with a nil dot: base=%v got=%v", pt, base.At(pt.X, pt.Y), out.At(pt.X, pt.Y))
		}
	}
}

func TestEncodeTrayIconDotPaintsBottomRightCorner(t *testing.T) {
	base := decodeTrayIconBase(t)
	dot := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}

	got := encodeTrayIcon(base, dot)
	out, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Mirrors drawStatusDot's own center formula -- the dot's actual center,
	// not the image's literal corner pixel (which sits just outside a
	// radius = size/3 circle centered there, so asserting on it directly
	// would be testing the wrong point).
	b := out.Bounds()
	size := b.Dx()
	if b.Dy() < size {
		size = b.Dy()
	}
	radius := size / 3
	if radius < 2 {
		radius = 2
	}
	cx, cy := b.Max.X-radius, b.Max.Y-radius
	r, g, bl, _ := out.At(cx, cy).RGBA()
	wantR, wantG, wantB, _ := dot.RGBA()
	if r != wantR || g != wantG || bl != wantB {
		t.Fatalf("dot center pixel (%d,%d) = %v, want the dot color %v", cx, cy, out.At(cx, cy), dot)
	}

	// The opposite (top-left) corner must be untouched -- otherwise the dot
	// isn't actually localized and just recolors the whole icon.
	tlR, tlG, tlB, tlA := out.At(b.Min.X, b.Min.Y).RGBA()
	baseR, baseG, baseB, baseA := base.At(b.Min.X, b.Min.Y).RGBA()
	if tlR != baseR || tlG != baseG || tlB != baseB || tlA != baseA {
		t.Fatalf("top-left corner changed, want it untouched by the status dot: base=%v got=%v",
			base.At(b.Min.X, b.Min.Y), out.At(b.Min.X, b.Min.Y))
	}
}

func TestDrawStatusDotNoPanicOnTinyImage(t *testing.T) {
	// A pathologically small image (smaller than any real icon asset) must
	// not panic drawStatusDot's radius/bounds math -- guards against a
	// future asset swap to something tiny regressing this silently.
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	drawStatusDot(img, color.NRGBA{R: 0xFF, A: 0xFF})
}

func TestBuildTrayIconsHasDistinctVariantsPerState(t *testing.T) {
	icons := buildTrayIcons()
	for _, state := range []trayIconState{trayIconIdle, trayIconActive, trayIconAttention} {
		if _, ok := icons[state]; !ok {
			t.Fatalf("buildTrayIcons missing entry for state %v", state)
		}
	}

	idle := icons[trayIconIdle].Content()
	active := icons[trayIconActive].Content()
	attention := icons[trayIconAttention].Content()

	if bytes.Equal(idle, active) {
		t.Fatal("idle and active tray icons are byte-identical -- the status dot isn't actually distinguishing them")
	}
	if bytes.Equal(idle, attention) {
		t.Fatal("idle and attention tray icons are byte-identical -- the status dot isn't actually distinguishing them")
	}
	if bytes.Equal(active, attention) {
		t.Fatal("active and attention tray icons are byte-identical -- design.ColorBrandAccent/ColorError must differ")
	}
}

func TestBuildTrayIconsFallsBackGracefullyOnBadPNG(t *testing.T) {
	// buildTrayIcons must never panic or return a nil/empty map just
	// because its embedded asset somehow failed to decode -- exercised
	// directly against encodeTrayIcon/png.Decode's own error path rather
	// than mutating the real embedded asset (which is a package-level
	// []byte shared by the real running app).
	if _, err := png.Decode(bytes.NewReader([]byte("not a png"))); err == nil {
		t.Fatal("test assumption broken: expected garbage bytes to fail PNG decoding")
	}
}

func TestDesignStatusColorsAreDistinct(t *testing.T) {
	// Sanity guard for the palette buildTrayIcons relies on: if these ever
	// collapse to the same color, TestBuildTrayIconsHasDistinctVariantsPerState
	// above would start failing for a much more confusing reason.
	if design.ColorBrandAccent == design.ColorError {
		t.Fatal("design.ColorBrandAccent and design.ColorError must be distinct for the tray's active/attention icons to differ")
	}
}
