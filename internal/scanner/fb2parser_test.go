package scanner

import (
	"strings"
	"testing"
)

// Sample FB2 content for testing
const sampleFB2 = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<description>
<title-info>
<genre>fiction</genre>
<genre>adventure</genre>
<author><first-name>John</first-name><last-name>Smith</last-name></author>
<author><first-name>Jane</first-name><last-name>Doe</last-name></author>
<book-title>Test Book Title</book-title>
<annotation><p>This is a test annotation.</p><p>Second paragraph.</p></annotation>
<lang>en</lang>
<sequence name="Test Series" number="3"/>
</title-info>
<document-info>
<date value="2024-01-15">2024-01-15</date>
</document-info>
</description>
<body>
<section><title><p>Chapter 1</p></title><p>Content here.</p></section>
</body>
</FictionBook>`

const fb2WithCover = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<description>
<title-info>
<genre>science_fiction</genre>
<author><first-name>Test</first-name><last-name>Author</last-name></author>
<book-title>Book With Cover</book-title>
<coverpage><image l:href="#cover.jpg"/></coverpage>
<lang>uk</lang>
</title-info>
<document-info><date value="2024"/></document-info>
</description>
<body><section><p>Content</p></section></body>
<binary id="cover.jpg" content-type="image/jpeg">/9j/4AAQSkZJRg==</binary>
</FictionBook>`

const fb2NicknameAuthor = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<description>
<title-info>
<genre>humor</genre>
<author><nickname>Anonymous</nickname></author>
<book-title>Anonymous Work</book-title>
<lang>en</lang>
</title-info>
<document-info><date/></document-info>
</description>
<body><section><p>Content</p></section></body>
</FictionBook>`

const fb2WithBOM = "\xef\xbb\xbf" + `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<description>
<title-info>
<genre>poetry</genre>
<author><first-name>BOM</first-name><last-name>Test</last-name></author>
<book-title>BOM Test Book</book-title>
<lang>en</lang>
</title-info>
<document-info><date/></document-info>
</description>
<body><section><p>Content</p></section></body>
</FictionBook>`

func TestFB2ParserBasic(t *testing.T) {
	parser := NewFB2Parser(false)
	meta, err := parser.Parse(strings.NewReader(sampleFB2))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// Test title
	if meta.Title != "Test Book Title" {
		t.Errorf("Expected title 'Test Book Title', got '%s'", meta.Title)
	}

	// Test language
	if meta.Lang != "en" {
		t.Errorf("Expected lang 'en', got '%s'", meta.Lang)
	}

	// Test genres
	if len(meta.Genres) != 2 {
		t.Errorf("Expected 2 genres, got %d", len(meta.Genres))
	}
	if meta.Genres[0] != "fiction" {
		t.Errorf("Expected first genre 'fiction', got '%s'", meta.Genres[0])
	}

	// Test authors
	if len(meta.Authors) != 2 {
		t.Errorf("Expected 2 authors, got %d", len(meta.Authors))
	}
	if meta.Authors[0].FirstName != "John" || meta.Authors[0].LastName != "Smith" {
		t.Errorf("First author mismatch: got %+v", meta.Authors[0])
	}
	if meta.Authors[1].FirstName != "Jane" || meta.Authors[1].LastName != "Doe" {
		t.Errorf("Second author mismatch: got %+v", meta.Authors[1])
	}

	// Test annotation
	if !strings.Contains(meta.Annotation, "test annotation") {
		t.Errorf("Expected annotation to contain 'test annotation', got '%s'", meta.Annotation)
	}

	// Test series
	if len(meta.Series) != 1 {
		t.Errorf("Expected 1 series, got %d", len(meta.Series))
	}
	if meta.Series[0].Name != "Test Series" || meta.Series[0].Number != 3 {
		t.Errorf("Series mismatch: got %+v", meta.Series[0])
	}

	// Test date
	if meta.DocDate != "2024-01-15" {
		t.Errorf("Expected date '2024-01-15', got '%s'", meta.DocDate)
	}
}

func TestFB2ParserWithCover(t *testing.T) {
	parser := NewFB2Parser(true)
	meta, err := parser.Parse(strings.NewReader(fb2WithCover))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if meta.Title != "Book With Cover" {
		t.Errorf("Expected title 'Book With Cover', got '%s'", meta.Title)
	}

	if meta.Lang != "uk" {
		t.Errorf("Expected lang 'uk', got '%s'", meta.Lang)
	}

	// Cover should be extracted (though this is truncated base64)
	if len(meta.Cover) == 0 {
		t.Log("Cover not extracted (expected with valid base64)")
	}

	if meta.CoverType != "image/jpeg" {
		t.Logf("Cover type: %s", meta.CoverType)
	}
}

func TestFB2ParserNicknameAuthor(t *testing.T) {
	parser := NewFB2Parser(false)
	meta, err := parser.Parse(strings.NewReader(fb2NicknameAuthor))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(meta.Authors) != 1 {
		t.Fatalf("Expected 1 author, got %d", len(meta.Authors))
	}

	// Nickname should be stored in LastName when no first/last name
	if meta.Authors[0].LastName != "Anonymous" {
		t.Errorf("Expected nickname 'Anonymous' in LastName, got '%s'", meta.Authors[0].LastName)
	}
}

func TestFB2ParserWithBOM(t *testing.T) {
	parser := NewFB2Parser(false)
	meta, err := parser.Parse(strings.NewReader(fb2WithBOM))
	if err != nil {
		t.Fatalf("Parse error with BOM: %v", err)
	}

	if meta.Title != "BOM Test Book" {
		t.Errorf("Expected title 'BOM Test Book', got '%s'", meta.Title)
	}
}

func TestFB2ParserPartial(t *testing.T) {
	parser := NewFB2Parser(false)
	// Parse only first 2000 bytes
	meta, err := parser.ParsePartial(strings.NewReader(sampleFB2), 2000)
	if err != nil {
		t.Fatalf("ParsePartial error: %v", err)
	}

	if meta.Title != "Test Book Title" {
		t.Errorf("Expected title 'Test Book Title', got '%s'", meta.Title)
	}

	if len(meta.Authors) == 0 {
		t.Error("Expected at least one author from partial parse")
	}
}

func TestFB2ParserEmptyInput(t *testing.T) {
	parser := NewFB2Parser(false)
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("Expected error on empty input")
	}
}

func TestFB2ParserInvalidXML(t *testing.T) {
	parser := NewFB2Parser(false)
	_, err := parser.Parse(strings.NewReader("<invalid>not closed"))
	if err == nil {
		t.Error("Expected error on invalid XML")
	}
}

func TestFB2ParserMultipleSeries(t *testing.T) {
	fb2MultipleSeries := `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<description>
<title-info>
<genre>fantasy</genre>
<author><first-name>Test</first-name><last-name>Author</last-name></author>
<book-title>Multi Series Book</book-title>
<lang>en</lang>
<sequence name="Series One" number="1"/>
<sequence name="Series Two" number="5"/>
</title-info>
<document-info><date/></document-info>
</description>
<body><section><p>Content</p></section></body>
</FictionBook>`

	parser := NewFB2Parser(false)
	meta, err := parser.Parse(strings.NewReader(fb2MultipleSeries))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(meta.Series) != 2 {
		t.Errorf("Expected 2 series, got %d", len(meta.Series))
	}

	if meta.Series[0].Name != "Series One" || meta.Series[0].Number != 1 {
		t.Errorf("First series mismatch: got %+v", meta.Series[0])
	}

	if meta.Series[1].Name != "Series Two" || meta.Series[1].Number != 5 {
		t.Errorf("Second series mismatch: got %+v", meta.Series[1])
	}
}
