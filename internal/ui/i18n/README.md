# Localization (i18n)

This directory contains the localization system for USB Bridge Client.

## Quick Start

The localization system is automatically initialized in `cmd/main.go` with English as the default language:

```go
i18n.Init("en")
```

## Supported Languages

- **English (en)** - Default
- **Russian (ru)**

## Adding a New Language

1. Add a new function in `localization.go`:

```go
func DE() *LocalizedStrings {
	return &LocalizedStrings{
		AppTitle: "USB Bridge Client",
		ServerAddress: "Serveradresse",
		// ... add all string translations
	}
}
```

2. Update the `Init()` function to support the new language:

```go
func Init(language string) {
	switch language {
	case "ru", "RU":
		Current = RU()
	case "de", "DE":
		Current = DE()
	default:
		Current = EN()
	}
}
```

## Using Localized Strings in UI Code

Import the i18n package and use `i18n.Current` to access localized strings:

```go
import "usbridge-client/internal/ui/i18n"

// In your UI code:
widget.NewLabel(i18n.Current.ServerAddress)
widget.NewButton(i18n.Current.SaveButton, handler)
```

## Changing Language at Runtime

To change the language dynamically:

```go
i18n.SetLanguage("ru")
```

**Note:** Changing language at runtime will affect all newly created UI elements, but won't automatically refresh existing UI elements. To fully switch languages, you may need to recreate the UI.

## Structure

- `localization.go` - Contains all localized strings
- `README.md` - This file

## Adding New Strings

When adding new UI text:

1. Add the field to `LocalizedStrings` struct:
```go
type LocalizedStrings struct {
	// ...
	NewFeatureText string
}
```

2. Add translations in both `EN()` and `RU()` functions:
```go
func EN() *LocalizedStrings {
	return &LocalizedStrings{
		// ...
		NewFeatureText: "New feature text in English",
	}
}

func RU() *LocalizedStrings {
	return &LocalizedStrings{
		// ...
		NewFeatureText: "Новый текст функции на русском",
	}
}
```

3. Use it in your UI code:
```go
label := widget.NewLabel(i18n.Current.NewFeatureText)
```

## Best Practices

1. **Never hardcode UI text** - Always use localized strings
2. **Use descriptive field names** - e.g., `ErrorNotConnected` instead of `Error1`
3. **Keep format strings consistent** - When using `fmt.Sprintf`, ensure placeholder order matches across languages
4. **Test all languages** - Verify that all UI text displays correctly in each supported language

## Example

### Before (hardcoded text):
```go
button := widget.NewButton("Save", handler)
label := widget.NewLabel("Ready to work")
```

### After (localized):
```go
import "usbridge-client/internal/ui/i18n"

button := widget.NewButton(i18n.Current.SaveButton, handler)
label := widget.NewLabel(i18n.Current.ReadyToWork)
```
