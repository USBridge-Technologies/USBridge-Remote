package view

import (
	"testing"

	"usbridge-client/internal/gui/i18n"
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
}
