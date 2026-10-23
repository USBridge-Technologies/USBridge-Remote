//go:build !windows

package input

// ClientKeyboardLayoutID guesses the layout from the input language the
// soft keyboard reports (Android IME, see SetCurrentLanguage); "" when
// nothing reported one, and auto then means US, as KVM typing always did.
func ClientKeyboardLayoutID() string {
	currentLanguageMu.RLock()
	lang := currentLanguage
	reported := currentLanguageReported
	currentLanguageMu.RUnlock()
	if !reported {
		return ""
	}
	return layoutForLanguage(lang)
}
