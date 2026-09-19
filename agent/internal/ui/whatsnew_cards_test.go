package ui

import (
	"testing"

	"usbridge_agent/internal/ui/i18n"
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
	if got := formatWhatsNewVersion("2.4.45"); got != "v2.4.45" {
		t.Fatalf("got %q", got)
	}
	if got := formatWhatsNewVersion("v2.4.45"); got != "v2.4.45" {
		t.Fatalf("got %q", got)
	}
}

func TestWhatsNewKindColors(t *testing.T) {
	if whatsNewKindLabel(whatsNewKindOpensource) != "Open Source" {
		t.Fatal("opensource label")
	}
	if whatsNewKindLabel(whatsNewKindPro) != "Pro" {
		t.Fatal("pro label")
	}
	if whatsNewKindLabel("") != "Other" {
		t.Fatal("empty kind should be Other")
	}
	if whatsNewKindLabel(whatsNewKindEnterprise) != "Enterprise" {
		t.Fatal("enterprise label")
	}
}

func TestWhatsNewCatalogHasCards(t *testing.T) {
	cards := whatsNewCatalog()
	if len(cards) != 1 {
		t.Fatalf("shipping one appeal for now, got %d", len(cards))
	}
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
				t.Fatalf("items not ordered Pro → Free → Open Source → Other")
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
			if whatsNewKindLabel(item.Kind) == "" {
				t.Fatalf("card %s has empty kind label", card.Version)
			}
		}
	}
}
