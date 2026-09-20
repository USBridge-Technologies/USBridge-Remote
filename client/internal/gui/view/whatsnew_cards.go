package view

import (
	"sort"
	"strings"

	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
)

// whatsNewCopy is EN/ES/UK text that lives next to the card, not in the
// chrome i18n table — edit it here when staging a release so the window
// can be previewed as it will ship.
type whatsNewCopy struct {
	EN, ES, UK string
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
	whatsNewGlyphColor   whatsNewGlyph = "color"
	whatsNewGlyphDisplay whatsNewGlyph = "display"
	whatsNewGlyphCloud   whatsNewGlyph = "cloud"
	whatsNewGlyphMetrics whatsNewGlyph = "metrics"
)

type whatsNewPoint struct {
	Glyph whatsNewGlyph
	Title whatsNewCopy
	Body  whatsNewCopy
}

// whatsNewItem is one plaque. Several Points share the same Kind so they
// stay in one card instead of stacking duplicate plaques.
type whatsNewItem struct {
	Kind   whatsNewKind
	Points []whatsNewPoint
}

// whatsNewCard is one post-update appeal. Newest first in the catalog.
type whatsNewCard struct {
	Version string
	Date    string
	Items   []whatsNewItem
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
	case whatsNewKindOpensource:
		return 4
	case whatsNewKindHardwareAgent:
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

// whatsNewCatalog is the release-notes deck. Add a new card at the top
// before shipping; leave older ones so the window can page through them.
func whatsNewCatalog() []whatsNewCard {
	return []whatsNewCard{
		{
			Version: "2.4.48",
			Date:    "September 2026",
			Items: []whatsNewItem{
				{
					Kind: whatsNewKindBeta,
					Points: []whatsNewPoint{{
						Glyph: whatsNewGlyphAI,
						Title: whatsNewCopy{
							EN: "AI Vision Overlay",
							ES: "Overlay AI Vision",
							UK: "Накладка AI Vision",
						},
						Body: whatsNewCopy{
							EN: "Live object detection overlay on the video feed.",
							ES: "Overlay de deteccion de objetos en vivo sobre el video.",
							UK: "Накладка розпізнавання об’єктів поверх відео.",
						},
					}},
				},
				{
					Kind: whatsNewKindPro,
					Points: []whatsNewPoint{
						{
							Glyph: whatsNewGlyphUSB,
							Title: whatsNewCopy{
								EN: "USB Emulation & Passthrough",
								ES: "Emulacion y passthrough USB",
								UK: "USB-емуляція та проброс",
							},
							Body: whatsNewCopy{
								EN: "Pass local USB devices through to the host.",
								ES: "Pasa dispositivos USB locales al host.",
								UK: "Прокидання локальних USB-пристроїв на хост.",
							},
						},
						{
							Glyph: whatsNewGlyphColor,
							Title: whatsNewCopy{
								EN: "4:4:4 True Color Fidelity",
								ES: "Fidelidad de color 4:4:4",
								UK: "Точна передача кольору 4:4:4",
							},
							Body: whatsNewCopy{
								EN: "Full chroma for sharper text and color-critical work.",
								ES: "Croma completo para texto mas nitido y trabajo de color.",
								UK: "Повна кольоровість для чіткішого тексту і точної передачі кольору.",
							},
						},
					},
				},
				{
					Kind: whatsNewKindFree,
					Points: []whatsNewPoint{{
						Glyph: whatsNewGlyphDisplay,
						Title: whatsNewCopy{
							EN: "Virtual Displays",
							ES: "Pantallas virtuales",
							UK: "Віртуальні дисплеї",
						},
						Body: whatsNewCopy{
							EN: "Extra screens without extra hardware.",
							ES: "Pantallas extra sin hardware extra.",
							UK: "Додаткові екрани без зайвого заліза.",
						},
					}},
				},
				{
					Kind: whatsNewKindOther,
					Points: []whatsNewPoint{
						{
							Glyph: whatsNewGlyphCloud,
							Title: whatsNewCopy{
								EN: "Cloud Connection Sync",
								ES: "Sincronizacion de conexiones",
								UK: "Хмарна синхронізація з’єднань",
							},
							Body: whatsNewCopy{
								EN: "Saved connections stay in sync across devices when you log in with Google.",
								ES: "Las conexiones guardadas se sincronizan entre dispositivos al entrar con Google.",
								UK: "Збережені з’єднання синхронізуються між пристроями після входу з Google.",
							},
						},
						{
							Glyph: whatsNewGlyphMetrics,
							Title: whatsNewCopy{
								EN: "Video metrics",
								ES: "Metricas de video",
								UK: "Відеометрики",
							},
							Body: whatsNewCopy{
								EN: "Live bitrate, FPS, and other stream parameters while you watch.",
								ES: "Bitrate, FPS y otros parametros del stream en vivo mientras miras.",
								UK: "Наживо bitrate, FPS та інші параметри передачі відео під час перегляду.",
							},
						},
					},
				},
			},
		},
	}
}
