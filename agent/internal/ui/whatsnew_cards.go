package ui

import (
	"sort"

	"usbridge_agent/internal/ui/i18n"
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

// Protocol-colored plaques. Technical names stay English on the badge.
type whatsNewKind string

const (
	whatsNewKindOpensource whatsNewKind = "opensource"
	whatsNewKindFree       whatsNewKind = "free"
	whatsNewKindPro        whatsNewKind = "pro"
	whatsNewKindEnterprise whatsNewKind = "enterprise"
	whatsNewKindOther      whatsNewKind = "other"
)

type whatsNewPoint struct {
	Title whatsNewCopy
	Body  whatsNewCopy
}

// whatsNewItem is one plaque. Several Points share the same Kind so they
// stay in one card instead of stacking duplicate protocol plaques.
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
	case whatsNewKindPro:
		return 0
	case whatsNewKindEnterprise:
		return 1
	case whatsNewKindFree:
		return 2
	case whatsNewKindOpensource:
		return 3
	default:
		return 4
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
			Version: "2.4.45",
			Items: []whatsNewItem{
				{
					Kind: whatsNewKindPro,
					Points: []whatsNewPoint{{
						Title: whatsNewCopy{
							EN: "USB emulation support",
							ES: "Soporte de emulacion USB",
							UK: "Підтримка USB-емуляції",
						},
						Body: whatsNewCopy{
							EN: "Pass local USB devices through to the host.",
							ES: "Pasa dispositivos USB locales al host.",
							UK: "Прокидання локальних USB-пристроїв на хост.",
						},
					}},
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
								EN: "Remote window lock",
								ES: "Bloqueo de la ventana remota",
								UK: "Блокування віддаленого вікна",
							},
							Body: whatsNewCopy{
								EN: "Stop a remote session from clicking the agent window, changing protocol, or quitting. Local use still works.",
								ES: "Impide que una sesion remota pulse la ventana del agent, cambie el protocolo o lo cierre. El uso local sigue funcionando.",
								UK: "Віддалена сесія більше не може натискати вікно агента, змінювати протокол чи закривати його. Локально все працює як раніше.",
							},
						},
						{
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
