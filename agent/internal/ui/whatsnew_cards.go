package ui

import (
	"sort"

	"usbridge_agent/internal/ui/i18n"
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

// Protocol-colored plaques. Technical names stay English on the badge.
type whatsNewKind string

const (
	whatsNewKindOpensource whatsNewKind = "opensource"
	whatsNewKindFree       whatsNewKind = "free"
	whatsNewKindPro        whatsNewKind = "pro"
	whatsNewKindEnterprise whatsNewKind = "enterprise"
	whatsNewKindBeta       whatsNewKind = "beta"
	whatsNewKindOther      whatsNewKind = "other"
)

type whatsNewPoint struct {
	Glyph whatsNewGlyph `json:"glyph"`
	Title whatsNewCopy  `json:"title"`
	Body  whatsNewCopy  `json:"body"`
}

type whatsNewGlyph string

const (
	whatsNewGlyphUSB      whatsNewGlyph = "usb"
	whatsNewGlyphProtocol whatsNewGlyph = "protocol"
	whatsNewGlyphDisplay  whatsNewGlyph = "display"
	whatsNewGlyphCloud   whatsNewGlyph = "cloud"
	whatsNewGlyphMetrics whatsNewGlyph = "metrics"
)

// whatsNewItem is one plaque. Several Points share the same Kind so they
// stay in one card instead of stacking duplicate protocol plaques.
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
	case whatsNewKindOpensource:
		return 4
	default:
		return 5
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
			Version: "3.1.8",
			Date:    "October 2026",
			Items: []whatsNewItem{
				{
					Kind: whatsNewKindFree,
					Points: []whatsNewPoint{{
						Glyph: whatsNewGlyphUSB,
						Title: whatsNewCopy{
							EN: "The client's microphone and MIDI",
							ES: "Microfono y MIDI del cliente",
							UK: "Мікрофон і MIDI клієнта",
						},
						Body: whatsNewCopy{
							EN: "This PC gets the client's microphone and MIDI keyboard as its own USB devices.",
							ES: "Este PC recibe el microfono y el teclado MIDI del cliente como dispositivos USB propios.",
							UK: "Цей ПК отримує мікрофон і MIDI-клавіатуру клієнта як власні USB-пристрої.",
						},
					}},
				},
				{
					Kind: whatsNewKindOpensource,
					Points: []whatsNewPoint{{
						Glyph: whatsNewGlyphProtocol,
						Title: whatsNewCopy{
							EN: "Vibepollo",
							ES: "Vibepollo",
							UK: "Vibepollo",
						},
						Body: whatsNewCopy{
							EN: "A new streamer to pick, installed and updated from here, with one shared virtual monitor on Windows.",
							ES: "Un streamer nuevo, instalado y actualizado desde aqui, con un monitor virtual compartido en Windows.",
							UK: "Новий стрімер на вибір, встановлюється й оновлюється звідси, зі спільним віртуальним монітором на Windows.",
						},
					}},
				},
				{
					Kind: whatsNewKindOther,
					Points: []whatsNewPoint{
						{
							Glyph: whatsNewGlyphCloud,
							Title: whatsNewCopy{
								EN: "Easier install",
								ES: "Instalacion mas facil",
								UK: "Простіше встановлення",
							},
							Body: whatsNewCopy{
								EN: "One-click Windows setup with drivers and the service; on Linux, a .deb updated through APT.",
								ES: "Instalador de Windows en un clic con drivers y servicio; en Linux, un .deb actualizado por APT.",
								UK: "Встановлення на Windows в один клік з драйверами й службою; на Linux — .deb з оновленнями через APT.",
							},
						},
						{
							Glyph: whatsNewGlyphMetrics,
							Title: whatsNewCopy{
								EN: "Wi-Fi tuned for streaming",
								ES: "Wi-Fi ajustado para streaming",
								UK: "Wi-Fi, налаштований для стріму",
							},
							Body: whatsNewCopy{
								EN: "A one-time tune in Permissions, and no background scans while a session runs.",
								ES: "Un ajuste unico en Permisos y sin escaneos en segundo plano durante la sesion.",
								UK: "Одноразове налаштування в Дозволах і жодних фонових сканувань під час сесії.",
							},
						},
					},
				},
			},
		},
		{
			Version: "3.0.111",
			Date:    "October 2026",
			Items: []whatsNewItem{
				{
					Kind: whatsNewKindFree,
					Points: []whatsNewPoint{{
						Glyph: whatsNewGlyphUSB,
						Title: whatsNewCopy{
							EN: "Free USB passthrough",
							ES: "Passthrough USB gratis",
							UK: "Безкоштовний проброс USB",
						},
						Body: whatsNewCopy{
							EN: "USB passthrough is now included in the free version, except graphics tablets.",
							ES: "El passthrough USB ya esta en la version gratuita, salvo tabletas graficas.",
							UK: "Проброс USB тепер доступний у безкоштовній версії, крім графічних планшетів.",
						},
					}},
				},
				{
					Kind: whatsNewKindOpensource,
					Points: []whatsNewPoint{{
						Glyph: whatsNewGlyphProtocol,
						Title: whatsNewCopy{
							EN: "Punktfunk",
							ES: "Punktfunk",
							UK: "Punktfunk",
						},
						Body: whatsNewCopy{
							EN: "A new open-source streaming protocol, next to Sunshine.",
							ES: "Un protocolo de streaming open source nuevo, junto a Sunshine.",
							UK: "Новий протокол стрімінгу з відкритим кодом, поруч із Sunshine.",
						},
					}},
				},
			},
		},
		{
			Version: "3.0.45",
			Date:    "September 2026",
			Items: []whatsNewItem{
				{
					Kind: whatsNewKindFree,
					Points: []whatsNewPoint{{
						Glyph: whatsNewGlyphDisplay,
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
							Glyph: whatsNewGlyphCloud,
							Title: whatsNewCopy{
								EN: "USBridge protocol auto-update",
								ES: "Autoactualizacion del protocolo USBridge",
								UK: "Автооновлення протоколу USBridge",
							},
							Body: whatsNewCopy{
								EN: "The streamer updates quietly in the background. Turn it off in General Settings to get a Yes/No prompt instead.",
								ES: "El streamer se actualiza en segundo plano. Desactivalo en General Settings para ver un aviso de Si/No.",
								UK: "Стрімер оновлюється тихо у фоні. У General Settings можна вимкнути авто і отримувати запит Так/Ні.",
							},
						},
					},
				},
			},
		},
	}
}
