package view

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
)

// whatsNewCopy is EN/ES/UK text that lives next to the card, not in the
// chrome i18n table — edit it here when staging a release so the window
// can be previewed as it will ship.
type whatsNewCopy struct {
	EN string `json:"en"`
	ES string `json:"es,omitempty"`
	UK string `json:"uk,omitempty"`
}

func (c whatsNewCopy) String() string {
	switch i18n.Code() {
	case "es":
		if s := c.ES; s != "" {
			return s
		}
	case "uk":
		if s := c.UK; s != "" {
			return s
		}
	}
	return c.EN
}

// Protocol- and product-colored plaques. Technical names stay English on the badge.
type whatsNewKind string

const (
	whatsNewKindOpensource    whatsNewKind = "opensource"
	whatsNewKindFree          whatsNewKind = "free"
	whatsNewKindPro           whatsNewKind = "pro"
	whatsNewKindEnterprise    whatsNewKind = "enterprise"
	whatsNewKindHardwareAgent whatsNewKind = "hardware"
	whatsNewKindBeta          whatsNewKind = "beta"
	whatsNewKindOther         whatsNewKind = "other"
)

// whatsNewGlyph picks the left icon tile for one feature row.
type whatsNewGlyph string

const (
	whatsNewGlyphAI      whatsNewGlyph = "ai"
	whatsNewGlyphUSB     whatsNewGlyph = "usb"
	whatsNewGlyphBoard   whatsNewGlyph = "board"
	whatsNewGlyphColor   whatsNewGlyph = "color"
	whatsNewGlyphDisplay whatsNewGlyph = "display"
	whatsNewGlyphCloud   whatsNewGlyph = "cloud"
	whatsNewGlyphMetrics whatsNewGlyph = "metrics"
)

type whatsNewPoint struct {
	Glyph whatsNewGlyph `json:"glyph"`
	Title whatsNewCopy  `json:"title"`
	Body  whatsNewCopy  `json:"body"`
	// LinkLabel and LinkURL replace the kind badge with a pill button.
	// LinkTone "purple" is the Mac tablet pill; "teal" is the Download pill.
	LinkLabel whatsNewCopy `json:"link_label,omitempty"`
	LinkURL   string       `json:"link_url,omitempty"`
	LinkTone  string       `json:"link_tone,omitempty"`
}

// whatsNewItem is one plaque. Several Points share the same Kind so they
// stay in one card instead of stacking duplicate plaques.
type whatsNewItem struct {
	Kind   whatsNewKind    `json:"kind"`
	Points []whatsNewPoint `json:"points"`
}

// whatsNewCard is one post-update appeal. Newest first in the catalog.
type whatsNewCard struct {
	Version string         `json:"version"`
	Date    string         `json:"date"`
	Items   []whatsNewItem `json:"items"`
}

func whatsNewKindRank(kind whatsNewKind) int {
	switch kind {
	case whatsNewKindBeta:
		return 0
	case whatsNewKindPro:
		return 1
	case whatsNewKindEnterprise:
		return 2
	case whatsNewKindFree:
		return 3
	case whatsNewKindHardwareAgent:
		return 4
	case whatsNewKindOpensource:
		return 5
	default:
		return 6
	}
}

func sortWhatsNewItems(items []whatsNewItem) []whatsNewItem {
	out := append([]whatsNewItem(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		return whatsNewKindRank(out[i].Kind) < whatsNewKindRank(out[j].Kind)
	})
	return out
}

// whatsNewSeenPrefKey stores the catalog fingerprint the user last opened.
// A new card in whatsNewCatalog changes the fingerprint, so the desktop
// footer pip lights again until they open What's new.
const whatsNewSeenPrefKey = "whats_new_seen_catalog"

// whatsNewCatalogFingerprint is the unseen-pip identity: every card
// version, newest first. Edit copy freely; add a card (new version) to
// make the footer pip return.
func whatsNewCatalogFingerprint() string {
	cards := whatsNewCatalog()
	if len(cards) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cards))
	for _, c := range cards {
		if v := strings.TrimSpace(c.Version); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, "\n")
}

func whatsNewHasUnseen() bool {
	fp := whatsNewCatalogFingerprint()
	if fp == "" {
		return false
	}
	app := fyne.CurrentApp()
	if app == nil {
		return true
	}
	return app.Preferences().StringWithFallback(whatsNewSeenPrefKey, "") != fp
}

func markWhatsNewCatalogSeen() {
	fp := whatsNewCatalogFingerprint()
	if fp == "" {
		return
	}
	if app := fyne.CurrentApp(); app != nil {
		app.Preferences().SetString(whatsNewSeenPrefKey, fp)
	}
	refreshWhatsNewFooterUnseen()
}

// whatsNewEmbedded is the catalog shipped in the binary: the fallback when
// there is no network and no cached copy. CI publishes the same file as
// the release asset whatsnew-client.json, so notes can be fixed after a
// release without rebuilding.
//
//go:embed whatsnew.json
var whatsNewEmbedded []byte

var (
	whatsNewRemoteMu    sync.Mutex
	whatsNewRemoteCards []whatsNewCard
)

type whatsNewFile struct {
	App   string         `json:"app"`
	Cards []whatsNewCard `json:"cards"`
}

const (
	whatsNewMaxCards  = 40
	whatsNewMaxItems  = 8
	whatsNewMaxPoints = 8
	whatsNewMaxText   = 500
)

// parseWhatsNew decodes and sanitizes a catalog. Cards without a version
// are dropped and sizes are capped, so a malformed file degrades to fewer
// cards instead of an unreadable dialog.
func parseWhatsNew(data []byte) ([]whatsNewCard, error) {
	var f whatsNewFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.App != "client" {
		return nil, fmt.Errorf("whatsnew is for app %q, expected %q", f.App, "client")
	}
	clip := func(c whatsNewCopy) whatsNewCopy {
		cut := func(s string) string {
			if r := []rune(s); len(r) > whatsNewMaxText {
				return string(r[:whatsNewMaxText])
			}
			return s
		}
		return whatsNewCopy{EN: cut(c.EN), ES: cut(c.ES), UK: cut(c.UK)}
	}
	var out []whatsNewCard
	for _, c := range f.Cards {
		c.Version = strings.TrimSpace(c.Version)
		if c.Version == "" || len(out) >= whatsNewMaxCards {
			continue
		}
		if len(c.Items) > whatsNewMaxItems {
			c.Items = c.Items[:whatsNewMaxItems]
		}
		for i := range c.Items {
			if len(c.Items[i].Points) > whatsNewMaxPoints {
				c.Items[i].Points = c.Items[i].Points[:whatsNewMaxPoints]
			}
			for j := range c.Items[i].Points {
				p := &c.Items[i].Points[j]
				p.Title, p.Body = clip(p.Title), clip(p.Body)
				if p.LinkURL != "" && !strings.HasPrefix(p.LinkURL, "https://") {
					p.LinkURL = ""
				}
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// SetRemoteWhatsNew installs a signature-verified catalog fetched from the
// release (or the cached copy of it). Call refresh of the footer pip on the
// UI goroutine afterwards.
func SetRemoteWhatsNew(data []byte) error {
	cards, err := parseWhatsNew(data)
	if err != nil {
		return err
	}
	whatsNewRemoteMu.Lock()
	whatsNewRemoteCards = cards
	whatsNewRemoteMu.Unlock()
	return nil
}

// whatsNewVersionLess reports whether dotted version a is older than b.
// RefreshWhatsNewFooter re-evaluates the footer pip after SetRemoteWhatsNew.
// UI goroutine only.
func RefreshWhatsNewFooter() { refreshWhatsNewFooterUnseen() }

func whatsNewVersionLess(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// whatsNewCatalog is the release-notes deck, newest first: the embedded
// cards, with any downloaded card of the same version replacing its
// embedded twin and newer downloaded cards added on top.
func whatsNewCatalog() []whatsNewCard {
	cards, err := parseWhatsNew(whatsNewEmbedded)
	if err != nil {
		cards = nil
	}
	whatsNewRemoteMu.Lock()
	remote := whatsNewRemoteCards
	whatsNewRemoteMu.Unlock()
	for _, rc := range remote {
		replaced := false
		for i := range cards {
			if cards[i].Version == rc.Version {
				cards[i] = rc
				replaced = true
				break
			}
		}
		if !replaced {
			cards = append(cards, rc)
		}
	}
	sort.SliceStable(cards, func(i, j int) bool {
		return whatsNewVersionLess(cards[j].Version, cards[i].Version)
	})
	return cards
}
