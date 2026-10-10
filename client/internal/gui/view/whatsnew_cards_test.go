package view

import (
	"image/color"
	"testing"

	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

func TestWhatsNewCopyPicksLanguage(t *testing.T) {
	c := whatsNewCopy{EN: "en", ES: "es", UK: "uk"}
	i18n.Init("en")
	if got := c.String(); got != "en" {
		t.Fatalf("en: %q", got)
	}
	i18n.Init("es")
	if got := c.String(); got != "es" {
		t.Fatalf("es: %q", got)
	}
	i18n.Init("uk")
	if got := c.String(); got != "uk" {
		t.Fatalf("uk: %q", got)
	}
	i18n.Init("en")
}

func TestWhatsNewOverflowPad(t *testing.T) {
	inner := canvas.NewRectangle(color.Transparent)
	inner.SetMinSize(fyne.NewSize(200, 100))
	body := newWhatsNewOverflowBody(inner, 200, 400)
	if got := body.MinSize(); got.Height != 100 || got.Width < 200 {
		t.Fatalf("fits without scroll: %+v", got)
	}
	inner.SetMinSize(fyne.NewSize(200, 500))
	if got := body.MinSize(); got.Height != 400 {
		t.Fatalf("caps at max: %+v", got)
	}
}

func TestFormatWhatsNewVersion(t *testing.T) {
	if got := formatWhatsNewVersion("3.0.45"); got != "v3.0.45" {
		t.Fatalf("got %q", got)
	}
	if got := formatWhatsNewVersion("v3.0.45"); got != "v3.0.45" {
		t.Fatalf("got %q", got)
	}
}

func TestWhatsNewKindBeta(t *testing.T) {
	if whatsNewKindLabel(whatsNewKindBeta) != "Beta" {
		t.Fatal("beta label")
	}
	if whatsNewKindRank(whatsNewKindBeta) >= whatsNewKindRank(whatsNewKindPro) {
		t.Fatal("beta should be first")
	}
}

func TestWhatsNewKindChrome(t *testing.T) {
	beta := whatsNewKindChromeFor(whatsNewKindBeta)
	if beta.Accent.R != 0xde || beta.Fill.R != 0x1f {
		t.Fatalf("beta chrome: %+v", beta)
	}
	free := whatsNewKindChromeFor(whatsNewKindFree)
	if free.Accent.G != 0xd4 {
		t.Fatalf("free chrome: %+v", free)
	}
	pro := whatsNewKindChromeFor(whatsNewKindPro)
	if pro.Accent.R != 0xb3 {
		t.Fatalf("pro chrome: %+v", pro)
	}
	gray := whatsNewKindChromeFor(whatsNewKindOther)
	if gray.Stroke.R != 0x4f {
		t.Fatalf("gray chrome: %+v", gray)
	}
}

func TestWhatsNewKindIncluded(t *testing.T) {
	if whatsNewKindLabel(whatsNewKindOther) != "Included" {
		t.Fatal("other label")
	}
}

func TestWhatsNewKindHardwareAgent(t *testing.T) {
	if whatsNewKindLabel(whatsNewKindHardwareAgent) != "Hardware Agent" {
		t.Fatal("hardware agent label")
	}
	if whatsNewKindRank(whatsNewKindOpensource) <= whatsNewKindRank(whatsNewKindHardwareAgent) {
		t.Fatal("open source should sit after Hardware Agent")
	}
}

func TestWhatsNewCatalogHasCards(t *testing.T) {
	cards := whatsNewCatalog()
	if len(cards) != 2 {
		t.Fatalf("shipping the current appeal plus the previous one, got %d", len(cards))
	}
	if cards[0].Version != "3.0.111" || cards[0].Date != "October 2026" {
		t.Fatalf("newest card: %s %q", cards[0].Version, cards[0].Date)
	}
	if cards[1].Version != "3.0.45" || cards[1].Date != "September 2026" {
		t.Fatalf("previous card: %s %q", cards[1].Version, cards[1].Date)
	}
	if whatsNewKindRank(whatsNewKindBeta) >= whatsNewKindRank(whatsNewKindPro) {
		t.Fatal("beta should be first")
	}
	if whatsNewKindRank(whatsNewKindFree) >= whatsNewKindRank(whatsNewKindHardwareAgent) {
		t.Fatal("free USB row should sit above the NanoKVM firmware row")
	}
	var sawBeta, sawPro, sawFree, sawHardware, sawOther bool
	for i, card := range cards {
		if card.Version == "" {
			t.Fatalf("card %d missing version", i)
		}
		if len(card.Items) == 0 {
			t.Fatalf("card %s has no items", card.Version)
		}
		sorted := sortWhatsNewItems(card.Items)
		for j := 1; j < len(sorted); j++ {
			if whatsNewKindRank(sorted[j-1].Kind) > whatsNewKindRank(sorted[j].Kind) {
				t.Fatalf("items not ordered")
			}
		}
		for _, item := range card.Items {
			if len(item.Points) == 0 {
				t.Fatalf("card %s %s has no points", card.Version, item.Kind)
			}
			for _, pt := range item.Points {
				if pt.Title.EN == "" || pt.Body.EN == "" {
					t.Fatalf("card %s is missing EN copy", card.Version)
				}
			}
			switch item.Kind {
			case whatsNewKindBeta:
				sawBeta = true
			case whatsNewKindPro:
				sawPro = true
			case whatsNewKindFree:
				sawFree = true
			case whatsNewKindHardwareAgent:
				sawHardware = true
			case whatsNewKindOther:
				sawOther = true
			}
		}
	}
	if !sawBeta || !sawPro || !sawFree || !sawHardware || !sawOther {
		t.Fatalf("missing plaques: beta=%v pro=%v free=%v hardware=%v other=%v", sawBeta, sawPro, sawFree, sawHardware, sawOther)
	}
	newest := sortWhatsNewItems(cards[0].Items)
	if newest[0].Kind != whatsNewKindFree || newest[0].Points[0].Glyph != whatsNewGlyphUSB {
		t.Fatal("first row of 3.0.111 should be free USB passthrough")
	}
	nano := newest[1].Points[0]
	if newest[1].Kind != whatsNewKindHardwareAgent || nano.Glyph != whatsNewGlyphBoard {
		t.Fatal("second row of 3.0.111 should be NanoKVM firmware")
	}
	if nano.LinkURL != "https://www.usbridge.io/hardware-agent" || nano.LinkLabel.EN != "Download" || nano.LinkTone != "teal" {
		t.Fatal("NanoKVM row should offer a Download button")
	}
	esp := newest[2].Points[0]
	if len(newest) < 3 || newest[2].Kind != whatsNewKindOpensource || esp.Glyph != whatsNewGlyphBoard {
		t.Fatal("third row of 3.0.111 should be the ESP module for Mac tablets")
	}
	if esp.LinkURL != "https://www.usbridge.io/macos-wacom-support" || esp.LinkLabel.EN != "Mac Tablet Support" {
		t.Fatal("ESP row should open Mac Tablet Support")
	}
	if esp.Title.EN == "" || esp.Body.EN == "" {
		t.Fatal("ESP row text should be a title plus one body line")
	}
}

func TestWhatsNewCatalogFingerprintJoinsVersions(t *testing.T) {
	fp := whatsNewCatalogFingerprint()
	if fp != "3.0.111\n3.0.45" {
		t.Fatalf("got %q", fp)
	}
}

func TestWhatsNewUnseenTracksCatalogFingerprint(t *testing.T) {
	a := test.NewApp()
	t.Cleanup(a.Quit)

	if !whatsNewHasUnseen() {
		t.Fatal("fresh prefs should show the pip")
	}
	markWhatsNewCatalogSeen()
	if whatsNewHasUnseen() {
		t.Fatal("opening What's new should clear the pip")
	}
	a.Preferences().SetString(whatsNewSeenPrefKey, "3.0.44")
	if !whatsNewHasUnseen() {
		t.Fatal("a new catalog card should light the pip again")
	}
}

func TestWhatsNewDialogMetricsMobileFitsPhone(t *testing.T) {
	prev := ForceMobileDesign
	ForceMobileDesign = true
	t.Cleanup(func() { ForceMobileDesign = prev })

	dialogW, bodyW, scrollMax := whatsNewDialogMetricsFor(fyne.NewSize(360, 640))
	if dialogW != 336 {
		t.Fatalf("dialogW=%v want 336 on a 360 canvas", dialogW)
	}
	if bodyW != 312 {
		t.Fatalf("bodyW=%v want 312", bodyW)
	}
	if scrollMax < 180 {
		t.Fatalf("scrollMax=%v", scrollMax)
	}
}
