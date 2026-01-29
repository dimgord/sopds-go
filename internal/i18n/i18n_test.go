package i18n

import (
	"testing"
)

func TestFlatten(t *testing.T) {
	data := map[string]interface{}{
		"simple": "value",
		"nested": map[string]interface{}{
			"key": "nested value",
			"deep": map[string]interface{}{
				"key": "deep value",
			},
		},
	}

	result := make(map[string]string)
	flatten("", data, result)

	expected := map[string]string{
		"simple":          "value",
		"nested.key":      "nested value",
		"nested.deep.key": "deep value",
	}

	for k, v := range expected {
		if result[k] != v {
			t.Errorf("flatten result[%q] = %q, expected %q", k, result[k], v)
		}
	}
}

func TestFlattenWithPrefix(t *testing.T) {
	data := map[string]interface{}{
		"key": "value",
	}

	result := make(map[string]string)
	flatten("prefix", data, result)

	if result["prefix.key"] != "value" {
		t.Errorf("flatten with prefix: result[prefix.key] = %q, expected value", result["prefix.key"])
	}
}

func TestT(t *testing.T) {
	// Test that T returns translations for known keys
	// Note: This test depends on the actual locale files being loaded

	// Test fallback to key when not found
	result := T("en", "nonexistent.key.that.does.not.exist")
	if result != "nonexistent.key.that.does.not.exist" {
		t.Errorf("T(en, nonexistent) should return key, got %q", result)
	}
}

func TestTFallbackToEnglish(t *testing.T) {
	// When a key exists in English but not in another language,
	// it should fall back to English

	// Get a key that exists in English
	enValue := T("en", "nav.home")
	if enValue == "nav.home" {
		t.Skip("nav.home not found in English translations")
	}

	// A non-existent language should fall back to English
	result := T("zz", "nav.home")
	if result != enValue {
		t.Errorf("T(zz, nav.home) = %q, expected fallback to English %q", result, enValue)
	}
}

func TestGetTranslations(t *testing.T) {
	result := GetTranslations("en")

	if result == nil {
		t.Fatal("GetTranslations(en) returned nil")
	}

	if len(result) == 0 {
		t.Error("GetTranslations(en) returned empty map")
	}
}

func TestGetTranslationsOverride(t *testing.T) {
	// Ukrainian should override English values
	uk := GetTranslations("uk")
	en := GetTranslations("en")

	// Both should have the same keys (since uk inherits from en)
	// but uk should have Ukrainian values where defined
	if len(uk) < len(en) {
		t.Errorf("Ukrainian translations (%d) should have at least as many keys as English (%d)",
			len(uk), len(en))
	}
}

func TestGetSupportedLanguages(t *testing.T) {
	langs := GetSupportedLanguages()

	if len(langs) == 0 {
		t.Fatal("GetSupportedLanguages() returned empty list")
	}

	// Check that English is included
	foundEn := false
	for _, lang := range langs {
		if lang.Code == "en" {
			foundEn = true
			if lang.Name != "English" {
				t.Errorf("English language name = %q, expected English", lang.Name)
			}
		}
	}

	if !foundEn {
		t.Error("English not found in supported languages")
	}
}

func TestIsValidLang(t *testing.T) {
	tests := []struct {
		lang     string
		expected bool
	}{
		{"en", true},
		{"uk", true},
		{"fr", true},
		{"es", true},
		{"de", true},
		{"zz", false},
		{"", false},
		{"english", false},
	}

	for _, tc := range tests {
		result := IsValidLang(tc.lang)
		if result != tc.expected {
			t.Errorf("IsValidLang(%q) = %v, expected %v", tc.lang, result, tc.expected)
		}
	}
}

func TestNewTranslator(t *testing.T) {
	tr := NewTranslator("en")
	if tr == nil {
		t.Fatal("NewTranslator(en) returned nil")
	}

	if tr.t == nil {
		t.Error("Translator.t is nil")
	}
}

func TestNewTranslatorInvalidLang(t *testing.T) {
	// Invalid language should fall back to English
	tr := NewTranslator("invalid")
	if tr == nil {
		t.Fatal("NewTranslator(invalid) returned nil")
	}

	// Should have English translations
	if len(tr.t) == 0 {
		t.Error("Translator should have fallback English translations")
	}
}

func TestTranslatorGet(t *testing.T) {
	tr := NewTranslator("en")

	// Test fallback to key for missing translation
	result := tr.Get("nonexistent.key")
	if result != "nonexistent.key" {
		t.Errorf("Get(nonexistent.key) = %q, expected key as fallback", result)
	}
}

func TestTranslatorAll(t *testing.T) {
	tr := NewTranslator("en")
	all := tr.All()

	if all == nil {
		t.Fatal("All() returned nil")
	}

	if len(all) == 0 {
		t.Error("All() returned empty map")
	}
}

func TestLanguageStruct(t *testing.T) {
	lang := Language{Code: "en", Name: "English"}

	if lang.Code != "en" {
		t.Errorf("Language.Code = %q, expected en", lang.Code)
	}
	if lang.Name != "English" {
		t.Errorf("Language.Name = %q, expected English", lang.Name)
	}
}
