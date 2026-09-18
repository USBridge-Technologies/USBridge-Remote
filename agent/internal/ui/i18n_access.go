package ui

import "usbridge_agent/internal/ui/i18n"

func loc() *i18n.LocalizedStrings {
	if i18n.Current == nil {
		i18n.Init("en")
	}
	return i18n.Current
}
