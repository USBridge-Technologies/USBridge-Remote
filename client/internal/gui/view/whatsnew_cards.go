package view

import (
	"sort"

	"usbridge-client/internal/gui/i18n"
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

type whatsNewPoint struct {
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

// whatsNewCatalog is the release-notes deck. Add a new card at the top
// before shipping; leave older ones so the window can page through them.
func whatsNewCatalog() []whatsNewCard {
	return []whatsNewCard{
		{
			Version: "2.4.48",
			Items: []whatsNewItem{
				{
					Kind: whatsNewKindBeta,
					Points: []whatsNewPoint{{
						Title: whatsNewCopy{
							EN: "AI Vision",
							ES: "AI Vision",
							UK: "AI Vision",
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
							Title: whatsNewCopy{
								EN: "USB emulation",
								ES: "Emulacion USB",
								UK: "USB-емуляція",
							},
							Body: whatsNewCopy{
								EN: "Pass local USB devices through to the host.",
								ES: "Pasa dispositivos USB locales al host.",
								UK: "Прокидання локальних USB-пристроїв на хост.",
							},
						},
						{
							Title: whatsNewCopy{
								EN: "4:4:4 color",
								ES: "Color 4:4:4",
								UK: "Колір 4:4:4",
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
						Title: whatsNewCopy{
							EN: "Virtual Display",
							ES: "Virtual Display",
							UK: "Virtual Display",
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
							Title: whatsNewCopy{
								EN: "Connection sync via Google",
								ES: "Sincronizacion de conexiones con Google",
								UK: "Синхронізація з’єднань через Google",
							},
							Body: whatsNewCopy{
								EN: "Saved connections stay in sync across devices when you log in with Google.",
								ES: "Las conexiones guardadas se sincronizan entre dispositivos al entrar con Google.",
								UK: "Збережені з’єднання синхронізуються між пристроями після входу з Google.",
							},
						},
						{
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
