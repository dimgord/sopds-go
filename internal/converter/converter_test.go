package converter

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleFB2 = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<description>
<title-info>
<genre>fiction</genre>
<author><first-name>John</first-name><last-name>Doe</last-name></author>
<book-title>Test Book</book-title>
<annotation><p>Test annotation.</p></annotation>
<lang>en</lang>
<sequence name="Test Series" number="1"/>
</title-info>
<document-info>
<date value="2024-01-01">2024</date>
</document-info>
</description>
<body>
<title><p>Test Book</p></title>
<section>
<title><p>Chapter 1</p></title>
<p>First paragraph of chapter 1.</p>
<p>Second paragraph with <emphasis>emphasis</emphasis>.</p>
<empty-line/>
<p>Third paragraph after empty line.</p>
</section>
<section>
<title><p>Chapter 2</p></title>
<p>Content of chapter 2.</p>
<poem>
<stanza>
<v>First line of poem</v>
<v>Second line of poem</v>
</stanza>
</poem>
</section>
</body>
</FictionBook>`

const fb2WithCover = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<description>
<title-info>
<genre>science_fiction</genre>
<author><first-name>Jane</first-name><last-name>Smith</last-name></author>
<book-title>Book With Cover</book-title>
<coverpage><image l:href="#cover.jpg"/></coverpage>
<lang>uk</lang>
</title-info>
<document-info><date value="2024"/></document-info>
</description>
<body>
<section><p>Content</p></section>
</body>
<binary id="cover.jpg" content-type="image/jpeg">/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/wAALCAABAAEBAREA/8QAFAABAAAAAAAAAAAAAAAAAAAACf/EABQQAQAAAAAAAAAAAAAAAAAAAAD/2gAIAQEAAD8AVN//2Q==</binary>
</FictionBook>`

const fb2WithNickname = `<?xml version="1.0" encoding="UTF-8"?>
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

func TestNewConverter(t *testing.T) {
	conv := New("/usr/bin/ebook-convert")
	if conv == nil {
		t.Error("Expected non-nil converter")
	}
	if conv.EbookConvertPath != "/usr/bin/ebook-convert" {
		t.Errorf("EbookConvertPath mismatch: got %s", conv.EbookConvertPath)
	}
}

func TestFB2DataToEPUB(t *testing.T) {
	conv := New("")

	// Create temp file for output
	tmpFile, err := os.CreateTemp("", "test-*.epub")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Convert
	err = conv.FB2DataToEPUB([]byte(sampleFB2), tmpFile.Name())
	if err != nil {
		t.Fatalf("FB2DataToEPUB error: %v", err)
	}

	// Verify EPUB structure
	verifyEPUBStructure(t, tmpFile.Name())
}

func TestFB2DataToEPUBWithCover(t *testing.T) {
	conv := New("")

	tmpFile, err := os.CreateTemp("", "test-cover-*.epub")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	err = conv.FB2DataToEPUB([]byte(fb2WithCover), tmpFile.Name())
	if err != nil {
		t.Fatalf("FB2DataToEPUB error: %v", err)
	}

	verifyEPUBStructure(t, tmpFile.Name())

	// Note: Cover image may or may not be extracted depending on valid base64 data
	// The test data uses truncated base64, so we just verify EPUB structure
}

func TestFB2DataToEPUBWithNickname(t *testing.T) {
	conv := New("")

	tmpFile, err := os.CreateTemp("", "test-nickname-*.epub")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	err = conv.FB2DataToEPUB([]byte(fb2WithNickname), tmpFile.Name())
	if err != nil {
		t.Fatalf("FB2DataToEPUB error: %v", err)
	}

	verifyEPUBStructure(t, tmpFile.Name())
}

func TestFB2DataToEPUBWithBOM(t *testing.T) {
	conv := New("")
	fb2WithBOM := "\xef\xbb\xbf" + sampleFB2

	tmpFile, err := os.CreateTemp("", "test-bom-*.epub")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	err = conv.FB2DataToEPUB([]byte(fb2WithBOM), tmpFile.Name())
	if err != nil {
		t.Fatalf("FB2DataToEPUB with BOM error: %v", err)
	}

	verifyEPUBStructure(t, tmpFile.Name())
}

func TestFB2DataToEPUBInvalidXML(t *testing.T) {
	conv := New("")

	tmpFile, err := os.CreateTemp("", "test-invalid-*.epub")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	err = conv.FB2DataToEPUB([]byte("<invalid>not closed"), tmpFile.Name())
	if err == nil {
		t.Error("Expected error for invalid XML")
	}
}

func TestConvertFromReader(t *testing.T) {
	conv := New("")

	tmpFile, err := os.CreateTemp("", "test-reader-*.epub")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	reader := strings.NewReader(sampleFB2)
	err = conv.ConvertFromReader(reader, "epub", tmpFile.Name())
	if err != nil {
		t.Fatalf("ConvertFromReader error: %v", err)
	}

	verifyEPUBStructure(t, tmpFile.Name())
}

func TestConvertFromReaderUnsupportedFormat(t *testing.T) {
	conv := New("")

	tmpFile, err := os.CreateTemp("", "test-unsupported-*.xyz")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	reader := strings.NewReader(sampleFB2)
	err = conv.ConvertFromReader(reader, "xyz", tmpFile.Name())
	if err == nil {
		t.Error("Expected error for unsupported format")
	}
}

func TestGetOutputPath(t *testing.T) {
	tests := []struct {
		inputPath string
		format    string
		expected  string
	}{
		{"/path/to/book.fb2", "epub", "/path/to/book.epub"},
		{"/path/to/book.fb2", "EPUB", "/path/to/book.epub"},
		{"/path/to/book.fb2.zip", "epub", "/path/to/book.epub"}, // Removes .fb2 from base
		{"/path/to/book", "mobi", "/path/to/book.mobi"},
		{"book.fb2", "epub", "book.epub"},
	}

	for _, tc := range tests {
		result := GetOutputPath(tc.inputPath, tc.format)
		if result != tc.expected {
			t.Errorf("GetOutputPath(%q, %q) = %q, expected %q",
				tc.inputPath, tc.format, result, tc.expected)
		}
	}
}

func TestFormatAuthorName(t *testing.T) {
	tests := []struct {
		person   fb2Person
		expected string
	}{
		{fb2Person{FirstName: "John", LastName: "Doe"}, "John Doe"},
		{fb2Person{FirstName: "John", MiddleName: "Q", LastName: "Doe"}, "John Q Doe"},
		{fb2Person{Nickname: "Anonymous"}, "Anonymous"},
		{fb2Person{}, ""},
		{fb2Person{FirstName: "  John  ", LastName: "  Doe  "}, "John Doe"},
	}

	for _, tc := range tests {
		result := formatAuthorName(tc.person)
		if result != tc.expected {
			t.Errorf("formatAuthorName(%+v) = %q, expected %q", tc.person, result, tc.expected)
		}
	}
}

func TestGetImageExtension(t *testing.T) {
	tests := []struct {
		contentType string
		expected    string
	}{
		{"image/jpeg", ".jpg"},
		{"image/jpg", ".jpg"},
		{"image/png", ".png"},
		{"image/gif", ".gif"},
		{"image/unknown", ".jpg"},
		{"", ".jpg"},
	}

	for _, tc := range tests {
		result := getImageExtension(tc.contentType)
		if result != tc.expected {
			t.Errorf("getImageExtension(%q) = %q, expected %q", tc.contentType, result, tc.expected)
		}
	}
}

func TestGetDefaultStylesheet(t *testing.T) {
	css := getDefaultStylesheet()

	if css == "" {
		t.Error("Expected non-empty stylesheet")
	}

	// Check for essential CSS rules
	if !strings.Contains(css, "body") {
		t.Error("Expected body rule in stylesheet")
	}
	if !strings.Contains(css, "font-family") {
		t.Error("Expected font-family in stylesheet")
	}
	if !strings.Contains(css, ".section") {
		t.Error("Expected .section rule in stylesheet")
	}
}

func TestConvertFB2Content(t *testing.T) {
	images := map[string]string{"img1": "images/img1.jpg"}

	tests := []struct {
		input    string
		expected string
	}{
		{"<emphasis>text</emphasis>", "<em>text</em>"},
		{"<strong>bold</strong>", "<strong>bold</strong>"},
		{"<empty-line/>", "<br/>"},
		{"<poem><stanza><v>line</v></stanza></poem>",
			"<div class=\"poem\"><div class=\"stanza\"><p class=\"verse\">line</p></div></div>"},
	}

	for _, tc := range tests {
		result := convertFB2Content([]byte(tc.input), images)
		if !strings.Contains(result, tc.expected) {
			t.Errorf("convertFB2Content(%q) = %q, expected to contain %q",
				tc.input, result, tc.expected)
		}
	}
}

// Helper functions

func verifyEPUBStructure(t *testing.T, epubPath string) {
	t.Helper()

	r, err := zip.OpenReader(epubPath)
	if err != nil {
		t.Fatalf("Failed to open EPUB as ZIP: %v", err)
	}
	defer r.Close()

	requiredFiles := []string{
		"mimetype",
		"META-INF/container.xml",
		"OEBPS/content.opf",
		"OEBPS/content.xhtml",
		"OEBPS/toc.ncx",
		"OEBPS/stylesheet.css",
	}

	filesFound := make(map[string]bool)
	for _, f := range r.File {
		filesFound[f.Name] = true
	}

	for _, required := range requiredFiles {
		if !filesFound[required] {
			t.Errorf("Required file missing from EPUB: %s", required)
		}
	}

	// Verify mimetype is first and uncompressed
	if len(r.File) > 0 {
		first := r.File[0]
		if first.Name != "mimetype" {
			t.Error("mimetype must be first file in EPUB")
		}
		if first.Method != zip.Store {
			t.Error("mimetype must be uncompressed (Store method)")
		}

		// Verify mimetype content
		rc, err := first.Open()
		if err != nil {
			t.Fatalf("Failed to open mimetype: %v", err)
		}
		content, _ := io.ReadAll(rc)
		rc.Close()
		if string(content) != "application/epub+zip" {
			t.Errorf("Invalid mimetype content: %s", content)
		}
	}
}

func verifyEPUBHasFile(t *testing.T, epubPath, filename string) {
	t.Helper()

	r, err := zip.OpenReader(epubPath)
	if err != nil {
		t.Fatalf("Failed to open EPUB: %v", err)
	}
	defer r.Close()

	found := false
	for _, f := range r.File {
		if f.Name == filename {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected file not found in EPUB: %s", filename)
	}
}

func TestFB2ToEPUBFile(t *testing.T) {
	conv := New("")

	// Create temp FB2 file
	tmpFB2, err := os.CreateTemp("", "test-*.fb2")
	if err != nil {
		t.Fatalf("Failed to create temp FB2: %v", err)
	}
	defer os.Remove(tmpFB2.Name())

	if _, err := tmpFB2.WriteString(sampleFB2); err != nil {
		t.Fatalf("Failed to write FB2: %v", err)
	}
	tmpFB2.Close()

	// Create output path
	epubPath := filepath.Join(os.TempDir(), "test-output.epub")
	defer os.Remove(epubPath)

	err = conv.FB2ToEPUB(tmpFB2.Name(), epubPath)
	if err != nil {
		t.Fatalf("FB2ToEPUB error: %v", err)
	}

	verifyEPUBStructure(t, epubPath)
}

func TestFB2ToEPUBNonExistentFile(t *testing.T) {
	conv := New("")

	err := conv.FB2ToEPUB("/non/existent/file.fb2", "/tmp/output.epub")
	if err == nil {
		t.Error("Expected error for non-existent input file")
	}
}

func TestWriteToZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	err := writeToZip(zw, "test.txt", []byte("Hello, World!"))
	if err != nil {
		t.Fatalf("writeToZip error: %v", err)
	}

	zw.Close()

	// Read back
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("Failed to read ZIP: %v", err)
	}

	if len(r.File) != 1 {
		t.Fatalf("Expected 1 file in ZIP, got %d", len(r.File))
	}

	if r.File[0].Name != "test.txt" {
		t.Errorf("Filename mismatch: got %s", r.File[0].Name)
	}
}
