package converter

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"os"
	"os/exec"
	"strings"
)

// ReaderContent contains the converted content for the web reader
type ReaderContent struct {
	Title   string
	Authors string
	Lang    string
	TOC     []TOCEntry
	HTML    string            // Main content with inline images
	Cover   string            // Cover image as data URI (optional)
}

// TOCEntry represents a table of contents entry
type TOCEntry struct {
	Title  string
	Anchor string
	Level  int
}

// FB2ToReaderHTML converts FB2 data to HTML suitable for the web reader
func FB2ToReaderHTML(data []byte) (*ReaderContent, error) {
	// Handle BOM if present
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})

	// Parse FB2
	var doc fb2Document
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse FB2: %w", err)
	}

	result := &ReaderContent{}

	// Extract metadata
	result.Title = strings.TrimSpace(doc.Description.TitleInfo.BookTitle)
	if result.Title == "" {
		result.Title = "Untitled"
	}

	result.Lang = strings.TrimSpace(doc.Description.TitleInfo.Lang)
	if result.Lang == "" {
		result.Lang = "en"
	}

	// Get authors
	var authors []string
	for _, a := range doc.Description.TitleInfo.Authors {
		name := formatAuthorName(a)
		if name != "" {
			authors = append(authors, name)
		}
	}
	if len(authors) == 0 {
		authors = []string{"Unknown Author"}
	}
	result.Authors = strings.Join(authors, ", ")

	// Process binaries (images) - convert to data URIs
	imageDataURIs := make(map[string]string) // id -> data URI
	for _, bin := range doc.Binaries {
		cleanData := strings.ReplaceAll(bin.Data, "\n", "")
		cleanData = strings.ReplaceAll(cleanData, "\r", "")
		cleanData = strings.ReplaceAll(cleanData, " ", "")

		// Create data URI
		contentType := bin.ContentType
		if contentType == "" {
			contentType = "image/jpeg"
		}
		dataURI := fmt.Sprintf("data:%s;base64,%s", contentType, cleanData)
		imageDataURIs[bin.ID] = dataURI
	}

	// Find and set cover image
	if doc.Description.TitleInfo.Coverpage != nil {
		for _, img := range doc.Description.TitleInfo.Coverpage.Images {
			href := img.Href
			if href == "" {
				href = img.XlinkHref
			}
			if strings.HasPrefix(href, "#") {
				coverID := strings.TrimPrefix(href, "#")
				if dataURI, ok := imageDataURIs[coverID]; ok {
					result.Cover = dataURI
					break
				}
			}
		}
	}

	// Convert bodies to HTML and extract TOC
	var toc []TOCEntry
	var sectionCounter int
	htmlContent := convertBodiesForReader(doc.Bodies, imageDataURIs, &toc, &sectionCounter)

	result.TOC = toc
	result.HTML = htmlContent

	return result, nil
}

// convertBodiesForReader converts FB2 bodies to HTML with TOC extraction
func convertBodiesForReader(bodies []fb2Body, images map[string]string, toc *[]TOCEntry, counter *int) string {
	var buf bytes.Buffer

	for _, body := range bodies {
		// Body title
		if body.Title != nil {
			*counter++
			anchor := fmt.Sprintf("section-%d", *counter)
			title := extractPlainText(body.Title.Content)
			if title != "" {
				*toc = append(*toc, TOCEntry{Title: title, Anchor: anchor, Level: 1})
			}
			buf.WriteString(fmt.Sprintf("<h1 id=\"%s\">", anchor))
			buf.WriteString(convertFB2ContentForReader(body.Title.Content, images))
			buf.WriteString("</h1>\n")
		}

		// Body epigraphs
		for _, ep := range body.Epigraph {
			buf.WriteString("<div class=\"epigraph\">")
			buf.WriteString(convertFB2ContentForReader(ep.Content, images))
			buf.WriteString("</div>\n")
		}

		// Sections
		for _, section := range body.Sections {
			buf.WriteString(convertSectionForReader(section, 1, images, toc, counter))
		}
	}

	return buf.String()
}

// convertSectionForReader converts a section to HTML with TOC extraction
func convertSectionForReader(s fb2Section, level int, images map[string]string, toc *[]TOCEntry, counter *int) string {
	var buf bytes.Buffer

	*counter++
	anchor := fmt.Sprintf("section-%d", *counter)

	// Use existing ID or generate one
	sectionID := s.ID
	if sectionID == "" {
		sectionID = anchor
	}

	buf.WriteString(fmt.Sprintf("<div id=\"%s\" class=\"section\">\n", html.EscapeString(sectionID)))

	// Title
	if s.Title != nil {
		title := extractPlainText(s.Title.Content)
		if title != "" {
			*toc = append(*toc, TOCEntry{Title: title, Anchor: sectionID, Level: level + 1})
		}

		hLevel := level + 1
		if hLevel > 6 {
			hLevel = 6
		}
		buf.WriteString(fmt.Sprintf("<h%d>", hLevel))
		buf.WriteString(convertFB2ContentForReader(s.Title.Content, images))
		buf.WriteString(fmt.Sprintf("</h%d>\n", hLevel))
	}

	// Epigraphs
	for _, ep := range s.Epigraph {
		buf.WriteString("<div class=\"epigraph\">")
		buf.WriteString(convertFB2ContentForReader(ep.Content, images))
		buf.WriteString("</div>\n")
	}

	// Content
	content := convertFB2ContentForReader(s.Content, images)
	buf.WriteString(content)

	// Nested sections
	for _, sub := range s.Sections {
		buf.WriteString(convertSectionForReader(sub, level+1, images, toc, counter))
	}

	buf.WriteString("</div>\n")

	return buf.String()
}

// convertFB2ContentForReader converts FB2 content to HTML with data URI images
func convertFB2ContentForReader(content []byte, images map[string]string) string {
	if len(content) == 0 {
		return ""
	}

	s := string(content)

	// Replace FB2 tags with HTML equivalents
	replacements := []struct{ old, new string }{
		{"<p>", "<p>"},
		{"</p>", "</p>"},
		{"<emphasis>", "<em>"},
		{"</emphasis>", "</em>"},
		{"<strong>", "<strong>"},
		{"</strong>", "</strong>"},
		{"<strikethrough>", "<del>"},
		{"</strikethrough>", "</del>"},
		{"<sub>", "<sub>"},
		{"</sub>", "</sub>"},
		{"<sup>", "<sup>"},
		{"</sup>", "</sup>"},
		{"<code>", "<code>"},
		{"</code>", "</code>"},
		{"<poem>", "<div class=\"poem\">"},
		{"</poem>", "</div>"},
		{"<stanza>", "<div class=\"stanza\">"},
		{"</stanza>", "</div>"},
		{"<v>", "<p class=\"verse\">"},
		{"</v>", "</p>"},
		{"<cite>", "<blockquote>"},
		{"</cite>", "</blockquote>"},
		{"<text-author>", "<p class=\"text-author\">"},
		{"</text-author>", "</p>"},
		{"<empty-line/>", "<br class=\"empty-line\">"},
		{"<empty-line />", "<br class=\"empty-line\">"},
		{"<subtitle>", "<h4 class=\"subtitle\">"},
		{"</subtitle>", "</h4>"},
		{"<epigraph>", "<div class=\"epigraph\">"},
		{"</epigraph>", "</div>"},
		{"<title>", "<div class=\"inline-title\">"},
		{"</title>", "</div>"},
	}

	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.old, r.new)
	}

	// Handle images - convert to data URIs
	for id, dataURI := range images {
		// Various image reference formats in FB2
		patterns := []string{
			fmt.Sprintf(`<image l:href="#%s"`, id),
			fmt.Sprintf(`<image xlink:href="#%s"`, id),
			fmt.Sprintf(`<image href="#%s"`, id),
		}
		for _, p := range patterns {
			if strings.Contains(s, p) {
				imgTag := fmt.Sprintf(`<img src="%s" alt="" class="book-image">`, dataURI)
				s = strings.ReplaceAll(s, p+`/>`, imgTag)
				s = strings.ReplaceAll(s, p+` />`, imgTag)
			}
		}
	}

	// Remove leftover FB2 namespace prefixes
	s = strings.ReplaceAll(s, "l:", "")
	s = strings.ReplaceAll(s, "xlink:", "")

	// Handle internal links
	s = strings.ReplaceAll(s, `type="note"`, "")

	// Clean up section tags from content
	s = strings.ReplaceAll(s, "<section>", "")
	s = strings.ReplaceAll(s, "</section>", "")

	return s
}

// extractPlainText extracts plain text from FB2 content (for TOC titles)
func extractPlainText(content []byte) string {
	if len(content) == 0 {
		return ""
	}

	s := string(content)

	// Remove all XML/HTML tags
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}

	// Clean up whitespace
	text := strings.TrimSpace(result.String())
	text = strings.Join(strings.Fields(text), " ")

	return text
}

// ConvertToFB2 converts other formats to FB2 using ebook-convert
func (c *Converter) ConvertToFB2(data []byte, format string, tempDir string) ([]byte, error) {
	ebookConvert, err := c.getEbookConvertPath()
	if err != nil {
		return nil, fmt.Errorf("ebook-convert not available: %w", err)
	}

	// Determine input extension
	ext := "." + strings.ToLower(format)

	// Create temp input file
	inputFile, err := os.CreateTemp(tempDir, "input-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp input file: %w", err)
	}
	inputPath := inputFile.Name()
	defer os.Remove(inputPath)

	if _, err := inputFile.Write(data); err != nil {
		inputFile.Close()
		return nil, fmt.Errorf("failed to write input file: %w", err)
	}
	inputFile.Close()

	// Create temp output file path
	outputFile, err := os.CreateTemp(tempDir, "output-*.fb2")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp output file: %w", err)
	}
	outputPath := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputPath)

	// Run ebook-convert
	cmd := exec.Command(ebookConvert, inputPath, outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ebook-convert failed: %v, output: %s", err, string(output))
	}

	// Read result
	fb2Data, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read converted file: %w", err)
	}

	return fb2Data, nil
}

