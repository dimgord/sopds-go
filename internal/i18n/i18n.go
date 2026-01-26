// Package i18n provides internationalization support for SOPDS.
//
// To add a new language:
// 1. Copy locales/en.yaml to locales/XX.yaml (where XX is the language code)
// 2. Translate all values in the new file
// 3. Add the language code to supportedLanguages slice below
// 4. Rebuild the application
package i18n

import (
	"embed"
	"fmt"
	"log"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed locales/*.yaml
var localesFS embed.FS

// Supported languages - add new language codes here
var supportedLanguages = []string{"en", "uk"}

// DefaultLang is the fallback language
const DefaultLang = "en"

// translations holds all loaded translations: lang -> flat key -> value
var translations = make(map[string]map[string]string)

// Language represents a language option for the UI
type Language struct {
	Code string
	Name string
}

// languageNames maps language codes to display names
var languageNames = map[string]string{
	"en": "English",
	"uk": "Українська",
}

func init() {
	for _, lang := range supportedLanguages {
		if err := loadLanguage(lang); err != nil {
			log.Printf("Warning: failed to load language %s: %v", lang, err)
		}
	}
}

// loadLanguage loads a single language file
func loadLanguage(lang string) error {
	data, err := localesFS.ReadFile(fmt.Sprintf("locales/%s.yaml", lang))
	if err != nil {
		return fmt.Errorf("read locale file: %w", err)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}

	// Flatten nested structure to dot-notation keys
	flat := make(map[string]string)
	flatten("", raw, flat)
	translations[lang] = flat

	return nil
}

// flatten converts nested map to flat key-value pairs with dot notation
func flatten(prefix string, data map[string]interface{}, result map[string]string) {
	for k, v := range data {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}

		switch val := v.(type) {
		case string:
			result[key] = val
		case map[string]interface{}:
			flatten(key, val, result)
		}
	}
}

// T returns the translation for a key in the specified language.
// Falls back to English if not found, then returns the key itself.
func T(lang, key string) string {
	if t, ok := translations[lang]; ok {
		if val, ok := t[key]; ok {
			return val
		}
	}
	// Fallback to English
	if lang != DefaultLang {
		if t, ok := translations[DefaultLang]; ok {
			if val, ok := t[key]; ok {
				return val
			}
		}
	}
	// Return key as last resort
	return key
}

// GetTranslations returns all translations for a language as a flat map.
// This is useful for passing to templates.
func GetTranslations(lang string) map[string]string {
	result := make(map[string]string)

	// Start with English as base
	if en, ok := translations[DefaultLang]; ok {
		for k, v := range en {
			result[k] = v
		}
	}

	// Override with requested language
	if lang != DefaultLang {
		if t, ok := translations[lang]; ok {
			for k, v := range t {
				result[k] = v
			}
		}
	}

	return result
}

// GetSupportedLanguages returns the list of supported language options
func GetSupportedLanguages() []Language {
	langs := make([]Language, 0, len(supportedLanguages))
	for _, code := range supportedLanguages {
		name := languageNames[code]
		if name == "" {
			name = strings.ToUpper(code)
		}
		langs = append(langs, Language{Code: code, Name: name})
	}
	return langs
}

// IsValidLang checks if a language code is supported
func IsValidLang(lang string) bool {
	for _, l := range supportedLanguages {
		if l == lang {
			return true
		}
	}
	return false
}

// Translator is a helper struct for template usage
type Translator struct {
	lang string
	t    map[string]string
}

// NewTranslator creates a new translator for the given language
func NewTranslator(lang string) *Translator {
	if !IsValidLang(lang) {
		lang = DefaultLang
	}
	return &Translator{
		lang: lang,
		t:    GetTranslations(lang),
	}
}

// Get returns a translation, implementing a method that templates can call
func (tr *Translator) Get(key string) string {
	if val, ok := tr.t[key]; ok {
		return val
	}
	return key
}

// All returns all translations as a map for template access via .T.key syntax
func (tr *Translator) All() map[string]string {
	return tr.t
}
