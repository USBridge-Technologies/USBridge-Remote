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
	if got := formatWhatsNewVersion("2.4.48"); got != "v2.4.48" {
		t.Fatalf("got %q", got)
	}
	if got := formatWhatsNewVersion("v2.4.48"); got != "v2.4.48" {
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
	if whatsNewKindRank(whatsNewKindHardwareAgent) <= whatsNewKindRank(whatsNewKindOpensource) {
		t.Fatal("hardware agent should sit after Open Source")
	}
}

func TestWhatsNewCatalogHasCards(t *testing.T) {
	cards := whatsNewCatalog()
	if len(cards) != 1 {
		t.Fatalf("shipping one appeal for now, got %d", len(cards))
	}
	if whatsNewKindRank(whatsNewKindBeta) >= whatsNewKindRank(whatsNewKindPro) {
		t.Fatal("beta should be first")
	}
	var sawBeta, sawProUSB, sawPro444, sawFree, sawOther bool
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
				if len(item.Points) < 2 {
					t.Fatal("pro plaque should have USB and 4:4:4")
				}
				sawProUSB, sawPro444 = true, true
			case whatsNewKindFree:
				sawFree = true
			case whatsNewKindOther:
				sawOther = true
			}
		}
	}
	if !sawBeta || !sawProUSB || !sawPro444 || !sawFree || !sawOther {
		t.Fatalf("missing plaques: beta=%v pro=%v/%v free=%v other=%v", sawBeta, sawProUSB, sawPro444, sawFree, sawOther)
	}
	if cards[0].Date != "September 2026" {
		t.Fatalf("date %q", cards[0].Date)
	}
}

func TestWhatsNewCatalogFingerprintJoinsVersions(t *testing.T) {
	fp := whatsNewCatalogFingerprint()
	if fp != "2.4.48" {
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
	a.Preferences().SetString(whatsNewSeenPrefKey, "2.4.47")
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
