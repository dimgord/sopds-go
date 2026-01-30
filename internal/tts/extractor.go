package tts

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// TextChunk represents a chunk of text for TTS processing
type TextChunk struct {
	Index int
	Title string // Chapter/section title (empty for continuation chunks)
	Text  string // Plain text content
}

// ExtractTextFromFB2 parses FB2 and returns text chunks for TTS
func ExtractTextFromFB2(data []byte, chunkSize int) ([]TextChunk, string, error) {
	// Handle BOM if present
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})

	var doc fb2Document
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, "", fmt.Errorf("failed to parse FB2: %w", err)
	}

	// Get language
	lang := strings.TrimSpace(doc.Description.TitleInfo.Lang)
	if lang == "" {
		lang = "en"
	}

	// Extract all text from bodies
	var sections []sectionText
	for _, body := range doc.Bodies {
		extractBodyText(body, &sections, 1)
	}

	// Split into chunks
	chunks := splitIntoChunks(sections, chunkSize)

	return chunks, lang, nil
}

// sectionText holds text for a section with its title
type sectionText struct {
	title string
	text  string
	level int
}

// extractBodyText extracts text from a body
func extractBodyText(body fb2Body, sections *[]sectionText, level int) {
	// Body title
	if body.Title != nil {
		title := extractPlainTextFromContent(body.Title.Content)
		if title != "" {
			*sections = append(*sections, sectionText{
				title: title,
				level: level,
			})
		}
	}

	// Epigraphs
	for _, ep := range body.Epigraph {
		text := extractPlainTextFromContent(ep.Content)
		if text != "" {
			if len(*sections) > 0 {
				// Append to last section
				(*sections)[len(*sections)-1].text += text + "\n\n"
			}
		}
	}

	// Sections
	for _, section := range body.Sections {
		extractSectionText(section, sections, level+1)
	}
}

// extractSectionText extracts text from a section recursively
func extractSectionText(s fb2Section, sections *[]sectionText, level int) {
	var sec sectionText
	sec.level = level

	// Title
	if s.Title != nil {
		sec.title = extractPlainTextFromContent(s.Title.Content)
	}

	// Epigraphs
	for _, ep := range s.Epigraph {
		text := extractPlainTextFromContent(ep.Content)
		if text != "" {
			sec.text += text + "\n\n"
		}
	}

	// Main content
	content := extractPlainTextFromContent(s.Content)
	if content != "" {
		sec.text += content
	}

	// Add section if it has content or title
	if sec.title != "" || sec.text != "" {
		*sections = append(*sections, sec)
	}

	// Nested sections
	for _, sub := range s.Sections {
		extractSectionText(sub, sections, level+1)
	}
}

// extractPlainTextFromContent extracts plain text from FB2 XML content
func extractPlainTextFromContent(content []byte) string {
	if len(content) == 0 {
		return ""
	}

	s := string(content)

	// Replace paragraph and line breaks with newlines
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = strings.ReplaceAll(s, "<empty-line/>", "\n")
	s = strings.ReplaceAll(s, "<empty-line />", "\n")
	s = strings.ReplaceAll(s, "</v>", "\n") // verse lines
	s = strings.ReplaceAll(s, "</stanza>", "\n\n")

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
	text := result.String()

	// Normalize multiple newlines
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}

	// Trim and clean up each line
	lines := strings.Split(text, "\n")
	var cleanLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}

	return strings.Join(cleanLines, "\n")
}

// splitIntoChunks splits sections into chunks of approximately chunkSize characters
func splitIntoChunks(sections []sectionText, chunkSize int) []TextChunk {
	if chunkSize <= 0 {
		chunkSize = 5000
	}

	var chunks []TextChunk
	var currentChunk TextChunk
	currentChunk.Index = 0

	for _, sec := range sections {
		// If this section has a title, it starts a new logical chunk
		if sec.title != "" {
			// Save current chunk if it has content
			if strings.TrimSpace(currentChunk.Text) != "" {
				chunks = append(chunks, currentChunk)
				currentChunk = TextChunk{Index: len(chunks)}
			}
			currentChunk.Title = sec.title
		}

		// Add section text
		textToAdd := sec.text
		if currentChunk.Title != "" && len(currentChunk.Text) == 0 {
			// Announce the chapter title in the audio
			textToAdd = sec.title + ".\n\n" + textToAdd
		}

		// If adding this text would exceed chunk size, split it
		for len(textToAdd) > 0 {
			remaining := chunkSize - len(currentChunk.Text)
			if remaining <= 0 {
				// Current chunk is full, start new one
				chunks = append(chunks, currentChunk)
				currentChunk = TextChunk{
					Index: len(chunks),
					Title: "", // Continuation chunks have no title
				}
				remaining = chunkSize
			}

			if len(textToAdd) <= remaining {
				// Fits in current chunk
				currentChunk.Text += textToAdd
				textToAdd = ""
			} else {
				// Need to split - try to split at sentence or paragraph boundary
				splitPoint := findSplitPoint(textToAdd, remaining)
				currentChunk.Text += textToAdd[:splitPoint]
				textToAdd = strings.TrimSpace(textToAdd[splitPoint:])
			}
		}
	}

	// Don't forget the last chunk
	if strings.TrimSpace(currentChunk.Text) != "" {
		chunks = append(chunks, currentChunk)
	}

	return chunks
}

// findSplitPoint finds a good place to split text (sentence or paragraph boundary)
func findSplitPoint(text string, maxLen int) int {
	if maxLen >= len(text) {
		return len(text)
	}

	// Look for paragraph break first (preferred)
	searchRange := text[:maxLen]

	// Try to find the last paragraph break
	if idx := strings.LastIndex(searchRange, "\n\n"); idx > maxLen/2 {
		return idx + 2
	}

	// Try to find the last sentence end
	sentenceEnds := []string{". ", "! ", "? ", ".\n", "!\n", "?\n"}
	bestIdx := -1
	for _, end := range sentenceEnds {
		if idx := strings.LastIndex(searchRange, end); idx > bestIdx && idx > maxLen/2 {
			bestIdx = idx + len(end)
		}
	}
	if bestIdx > 0 {
		return bestIdx
	}

	// Try to find the last newline
	if idx := strings.LastIndex(searchRange, "\n"); idx > maxLen/2 {
		return idx + 1
	}

	// Try to find the last space
	if idx := strings.LastIndex(searchRange, " "); idx > maxLen/2 {
		return idx + 1
	}

	// Give up and split at maxLen
	return maxLen
}

// FB2 XML structures (copied from converter for self-containment)
type fb2Document struct {
	XMLName     xml.Name       `xml:"FictionBook"`
	Description fb2Description `xml:"description"`
	Bodies      []fb2Body      `xml:"body"`
}

type fb2Description struct {
	TitleInfo fb2TitleInfo `xml:"title-info"`
}

type fb2TitleInfo struct {
	Lang      string       `xml:"lang"`
	BookTitle string       `xml:"book-title"`
	Authors   []fb2Author  `xml:"author"`
	Coverpage *fb2Cover    `xml:"coverpage"`
}

type fb2Author struct {
	FirstName  string `xml:"first-name"`
	MiddleName string `xml:"middle-name"`
	LastName   string `xml:"last-name"`
	Nickname   string `xml:"nickname"`
}

type fb2Cover struct {
	Images []fb2Image `xml:"image"`
}

type fb2Image struct {
	Href      string `xml:"href,attr"`
	XlinkHref string `xml:"http://www.w3.org/1999/xlink href,attr"`
}

type fb2Body struct {
	Name     string       `xml:"name,attr"`
	Title    *fb2Title    `xml:"title"`
	Epigraph []fb2Epigraph `xml:"epigraph"`
	Sections []fb2Section `xml:"section"`
}

type fb2Title struct {
	Content []byte `xml:",innerxml"`
}

type fb2Epigraph struct {
	Content []byte `xml:",innerxml"`
}

type fb2Section struct {
	ID       string        `xml:"id,attr"`
	Title    *fb2Title     `xml:"title"`
	Epigraph []fb2Epigraph `xml:"epigraph"`
	Content  []byte        `xml:",innerxml"`
	Sections []fb2Section  `xml:"section"`
}
