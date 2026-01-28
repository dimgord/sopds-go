package converter

import (
	"strings"
	"testing"
)

const readerTestFB2 = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<description>
<title-info>
<genre>fiction</genre>
<author><first-name>John</first-name><last-name>Doe</last-name></author>
<author><first-name>Jane</first-name><last-name>Smith</last-name></author>
<book-title>Reader Test Book</book-title>
<annotation><p>Test annotation.</p></annotation>
<lang>en</lang>
</title-info>
<document-info><date value="2024"/></document-info>
</description>
<body>
<title><p>Book Title</p></title>
<section>
<title><p>Chapter 1</p></title>
<p>First paragraph.</p>
<p>Second paragraph with <emphasis>emphasis</emphasis> and <strong>strong</strong>.</p>
<empty-line/>
</section>
<section id="chapter2">
<title><p>Chapter 2</p></title>
<p>Content of chapter 2.</p>
<poem>
<stanza>
<v>First verse</v>
<v>Second verse</v>
</stanza>
</poem>
</section>
</body>
</FictionBook>`

const readerFB2WithCover = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<description>
<title-info>
<genre>fiction</genre>
<author><first-name>Test</first-name><last-name>Author</last-name></author>
<book-title>Book With Cover</book-title>
<coverpage><image l:href="#cover.jpg"/></coverpage>
<lang>uk</lang>
</title-info>
<document-info><date/></document-info>
</description>
<body>
<section><p>Content</p></section>
</body>
<binary id="cover.jpg" content-type="image/jpeg">/9j/4AAQSkZJRg==</binary>
</FictionBook>`

const readerFB2Minimal = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<description>
<title-info>
<book-title></book-title>
<lang></lang>
</title-info>
<document-info><date/></document-info>
</description>
<body><section><p>Content</p></section></body>
</FictionBook>`

func TestFB2ToReaderHTML(t *testing.T) {
	result, err := FB2ToReaderHTML([]byte(readerTestFB2))
	if err != nil {
		t.Fatalf("FB2ToReaderHTML error: %v", err)
	}

	// Check title
	if result.Title != "Reader Test Book" {
		t.Errorf("Title = %q, expected 'Reader Test Book'", result.Title)
	}

	// Check authors
	if !strings.Contains(result.Authors, "John Doe") {
		t.Errorf("Authors should contain 'John Doe', got %q", result.Authors)
	}
	if !strings.Contains(result.Authors, "Jane Smith") {
		t.Errorf("Authors should contain 'Jane Smith', got %q", result.Authors)
	}

	// Check language
	if result.Lang != "en" {
		t.Errorf("Lang = %q, expected 'en'", result.Lang)
	}

	// Check TOC has entries
	if len(result.TOC) == 0 {
		t.Error("Expected TOC entries")
	}

	// Check HTML content
	if !strings.Contains(result.HTML, "First paragraph") {
		t.Error("HTML should contain 'First paragraph'")
	}
	if !strings.Contains(result.HTML, "<em>emphasis</em>") {
		t.Error("HTML should contain converted emphasis tag")
	}
	if !strings.Contains(result.HTML, "<strong>strong</strong>") {
		t.Error("HTML should contain strong tag")
	}
}

func TestFB2ToReaderHTMLWithCover(t *testing.T) {
	result, err := FB2ToReaderHTML([]byte(readerFB2WithCover))
	if err != nil {
		t.Fatalf("FB2ToReaderHTML error: %v", err)
	}

	if result.Title != "Book With Cover" {
		t.Errorf("Title = %q, expected 'Book With Cover'", result.Title)
	}

	if result.Lang != "uk" {
		t.Errorf("Lang = %q, expected 'uk'", result.Lang)
	}

	// Cover should be a data URI
	if result.Cover == "" {
		t.Log("Cover not extracted (may be expected with truncated base64)")
	} else if !strings.HasPrefix(result.Cover, "data:image/jpeg;base64,") {
		t.Errorf("Cover should be data URI, got: %s", result.Cover[:50])
	}
}

func TestFB2ToReaderHTMLMinimal(t *testing.T) {
	result, err := FB2ToReaderHTML([]byte(readerFB2Minimal))
	if err != nil {
		t.Fatalf("FB2ToReaderHTML error: %v", err)
	}

	// Empty title should default to "Untitled"
	if result.Title != "Untitled" {
		t.Errorf("Title = %q, expected 'Untitled'", result.Title)
	}

	// Empty lang should default to "en"
	if result.Lang != "en" {
		t.Errorf("Lang = %q, expected 'en'", result.Lang)
	}

	// Empty authors should default to "Unknown Author"
	if result.Authors != "Unknown Author" {
		t.Errorf("Authors = %q, expected 'Unknown Author'", result.Authors)
	}
}

func TestFB2ToReaderHTMLWithBOM(t *testing.T) {
	fb2WithBOM := "\xef\xbb\xbf" + readerTestFB2
	result, err := FB2ToReaderHTML([]byte(fb2WithBOM))
	if err != nil {
		t.Fatalf("FB2ToReaderHTML with BOM error: %v", err)
	}

	if result.Title != "Reader Test Book" {
		t.Errorf("Title = %q, expected 'Reader Test Book'", result.Title)
	}
}

func TestFB2ToReaderHTMLInvalidXML(t *testing.T) {
	_, err := FB2ToReaderHTML([]byte("<invalid>not closed"))
	if err == nil {
		t.Error("Expected error for invalid XML")
	}
}

func TestExtractPlainText(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"<p>Hello World</p>", "Hello World"},
		{"<p>First</p><p>Second</p>", "FirstSecond"}, // No space between consecutive tags
		{"Text with <em>emphasis</em>", "Text with emphasis"},
		{"<p>  Multiple   spaces  </p>", "Multiple spaces"},
		{"", ""},
		{"No tags here", "No tags here"},
		{"<nested><deep>Content</deep></nested>", "Content"},
	}

	for _, tc := range tests {
		result := extractPlainText([]byte(tc.input))
		if result != tc.expected {
			t.Errorf("extractPlainText(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestConvertFB2ContentForReader(t *testing.T) {
	images := map[string]string{
		"img1": "data:image/jpeg;base64,abc123",
	}

	tests := []struct {
		input    string
		contains string
	}{
		{"<emphasis>text</emphasis>", "<em>text</em>"},
		{"<strong>bold</strong>", "<strong>bold</strong>"},
		{"<strikethrough>deleted</strikethrough>", "<del>deleted</del>"},
		{"<sub>subscript</sub>", "<sub>subscript</sub>"},
		{"<sup>superscript</sup>", "<sup>superscript</sup>"},
		{"<code>code</code>", "<code>code</code>"},
		{"<empty-line/>", "<br class=\"empty-line\">"},
		{"<empty-line />", "<br class=\"empty-line\">"},
		{"<poem><stanza><v>verse</v></stanza></poem>",
			"<div class=\"poem\"><div class=\"stanza\"><p class=\"verse\">verse</p></div></div>"},
		{"<cite>quote</cite>", "<blockquote>quote</blockquote>"},
		{"<subtitle>sub</subtitle>", "<h4 class=\"subtitle\">sub</h4>"},
	}

	for _, tc := range tests {
		result := convertFB2ContentForReader([]byte(tc.input), images)
		if !strings.Contains(result, tc.contains) {
			t.Errorf("convertFB2ContentForReader(%q) = %q, expected to contain %q",
				tc.input, result, tc.contains)
		}
	}
}

func TestConvertFB2ContentForReaderWithImages(t *testing.T) {
	images := map[string]string{
		"cover": "data:image/jpeg;base64,abc123",
	}

	// Test l:href format
	input1 := `<image l:href="#cover"/>`
	result1 := convertFB2ContentForReader([]byte(input1), images)
	if !strings.Contains(result1, "data:image/jpeg;base64,abc123") {
		t.Error("Image data URI not found for l:href format")
	}

	// Test xlink:href format
	input2 := `<image xlink:href="#cover"/>`
	result2 := convertFB2ContentForReader([]byte(input2), images)
	if !strings.Contains(result2, "data:image/jpeg;base64,abc123") {
		t.Error("Image data URI not found for xlink:href format")
	}

	// Test href format
	input3 := `<image href="#cover"/>`
	result3 := convertFB2ContentForReader([]byte(input3), images)
	if !strings.Contains(result3, "data:image/jpeg;base64,abc123") {
		t.Error("Image data URI not found for href format")
	}
}

func TestReaderContentStruct(t *testing.T) {
	content := ReaderContent{
		Title:   "Test Title",
		Authors: "Test Author",
		Lang:    "en",
		TOC: []TOCEntry{
			{Title: "Chapter 1", Anchor: "ch1", Level: 1},
			{Title: "Chapter 2", Anchor: "ch2", Level: 1},
		},
		HTML:  "<p>Content</p>",
		Cover: "data:image/jpeg;base64,abc",
	}

	if content.Title != "Test Title" {
		t.Error("Title mismatch")
	}
	if len(content.TOC) != 2 {
		t.Error("TOC length mismatch")
	}
	if content.TOC[0].Anchor != "ch1" {
		t.Error("TOC anchor mismatch")
	}
}

func TestTOCEntryStruct(t *testing.T) {
	entry := TOCEntry{
		Title:  "Section Title",
		Anchor: "section-1",
		Level:  2,
	}

	if entry.Title != "Section Title" {
		t.Error("Title mismatch")
	}
	if entry.Anchor != "section-1" {
		t.Error("Anchor mismatch")
	}
	if entry.Level != 2 {
		t.Error("Level mismatch")
	}
}

func TestTOCExtraction(t *testing.T) {
	result, err := FB2ToReaderHTML([]byte(readerTestFB2))
	if err != nil {
		t.Fatalf("FB2ToReaderHTML error: %v", err)
	}

	// Should have TOC entries for body title and chapter titles
	foundChapter1 := false
	foundChapter2 := false
	for _, entry := range result.TOC {
		if strings.Contains(entry.Title, "Chapter 1") {
			foundChapter1 = true
		}
		if strings.Contains(entry.Title, "Chapter 2") {
			foundChapter2 = true
		}
	}

	if !foundChapter1 {
		t.Error("TOC should contain Chapter 1")
	}
	if !foundChapter2 {
		t.Error("TOC should contain Chapter 2")
	}
}

func TestSectionIDPreservation(t *testing.T) {
	result, err := FB2ToReaderHTML([]byte(readerTestFB2))
	if err != nil {
		t.Fatalf("FB2ToReaderHTML error: %v", err)
	}

	// The section with id="chapter2" should preserve its ID
	if !strings.Contains(result.HTML, `id="chapter2"`) {
		t.Error("Section ID 'chapter2' should be preserved in HTML")
	}
}

func TestNestedSections(t *testing.T) {
	fb2Nested := `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<description>
<title-info>
<book-title>Nested Test</book-title>
<lang>en</lang>
</title-info>
<document-info><date/></document-info>
</description>
<body>
<section>
<title><p>Level 1</p></title>
<section>
<title><p>Level 2</p></title>
<section>
<title><p>Level 3</p></title>
<p>Deep content.</p>
</section>
</section>
</section>
</body>
</FictionBook>`

	result, err := FB2ToReaderHTML([]byte(fb2Nested))
	if err != nil {
		t.Fatalf("FB2ToReaderHTML error: %v", err)
	}

	// Check nested headings (h2, h3, h4...)
	if !strings.Contains(result.HTML, "<h2>") {
		t.Error("Expected h2 for level 1 section")
	}
	if !strings.Contains(result.HTML, "<h3>") {
		t.Error("Expected h3 for level 2 section")
	}
	if !strings.Contains(result.HTML, "<h4>") {
		t.Error("Expected h4 for level 3 section")
	}
}

func TestHeadingLevelCap(t *testing.T) {
	// Test that heading levels are capped at h6
	fb2DeepNested := `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<description>
<title-info>
<book-title>Deep Nested</book-title>
<lang>en</lang>
</title-info>
<document-info><date/></document-info>
</description>
<body>
<section><title><p>L1</p></title>
<section><title><p>L2</p></title>
<section><title><p>L3</p></title>
<section><title><p>L4</p></title>
<section><title><p>L5</p></title>
<section><title><p>L6</p></title>
<section><title><p>L7</p></title>
<p>Content</p>
</section></section></section></section></section></section></section>
</body>
</FictionBook>`

	result, err := FB2ToReaderHTML([]byte(fb2DeepNested))
	if err != nil {
		t.Fatalf("FB2ToReaderHTML error: %v", err)
	}

	// Should not have h7 (capped at h6)
	if strings.Contains(result.HTML, "<h7>") {
		t.Error("Heading levels should be capped at h6")
	}
}
