package converter

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Converter handles ebook format conversions
type Converter struct {
	EbookConvertPath string // Path to calibre's ebook-convert
}

// New creates a new Converter
func New(ebookConvertPath string) *Converter {
	return &Converter{
		EbookConvertPath: ebookConvertPath,
	}
}

// FB2ToEPUB converts FB2 file to EPUB format
func (c *Converter) FB2ToEPUB(fb2Path string, epubPath string) error {
	// Read FB2 file
	data, err := os.ReadFile(fb2Path)
	if err != nil {
		return fmt.Errorf("failed to read FB2 file: %w", err)
	}

	return c.FB2DataToEPUB(data, epubPath)
}

// FB2DataToEPUB converts FB2 data to EPUB format
func (c *Converter) FB2DataToEPUB(data []byte, epubPath string) error {
	// Handle BOM if present
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})

	// Parse FB2
	var doc fb2Document
	if err := xml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("failed to parse FB2: %w", err)
	}

	// Create EPUB
	return createEPUB(&doc, epubPath)
}

// getEbookConvertPath returns the path to ebook-convert, using system PATH if not configured
func (c *Converter) getEbookConvertPath() (string, error) {
	if c.EbookConvertPath != "" {
		return c.EbookConvertPath, nil
	}
	// Try to find ebook-convert in PATH
	path, err := exec.LookPath("ebook-convert")
	if err != nil {
		return "", fmt.Errorf("ebook-convert not found in PATH and not configured")
	}
	return path, nil
}

// FB2ToMOBI converts FB2 file to MOBI format using ebook-convert
func (c *Converter) FB2ToMOBI(fb2Path string, mobiPath string) error {
	ebookConvert, err := c.getEbookConvertPath()
	if err != nil {
		return err
	}

	// First convert to EPUB
	tempEpub := mobiPath + ".epub"
	defer os.Remove(tempEpub)

	if err := c.FB2ToEPUB(fb2Path, tempEpub); err != nil {
		return fmt.Errorf("failed to create intermediate EPUB: %w", err)
	}

	// Then convert EPUB to MOBI using ebook-convert
	cmd := exec.Command(ebookConvert, tempEpub, mobiPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ebook-convert failed: %v, output: %s", err, string(output))
	}

	return nil
}

// FB2DataToMOBI converts FB2 data to MOBI format
func (c *Converter) FB2DataToMOBI(data []byte, mobiPath string) error {
	if _, err := c.getEbookConvertPath(); err != nil {
		return err
	}

	// Create temp FB2 file
	tempFb2, err := os.CreateTemp("", "fb2-*.fb2")
	if err != nil {
		return err
	}
	defer os.Remove(tempFb2.Name())

	if _, err := tempFb2.Write(data); err != nil {
		tempFb2.Close()
		return err
	}
	tempFb2.Close()

	return c.FB2ToMOBI(tempFb2.Name(), mobiPath)
}

// FB2 XML structures
type fb2Document struct {
	XMLName     xml.Name       `xml:"FictionBook"`
	Description fb2Description `xml:"description"`
	Bodies      []fb2Body      `xml:"body"`
	Binaries    []fb2Binary    `xml:"binary"`
}

type fb2Description struct {
	TitleInfo    fb2TitleInfo    `xml:"title-info"`
	DocumentInfo fb2DocumentInfo `xml:"document-info"`
}

type fb2TitleInfo struct {
	Genres     []string       `xml:"genre"`
	Authors    []fb2Person    `xml:"author"`
	BookTitle  string         `xml:"book-title"`
	Annotation *fb2Annotation `xml:"annotation"`
	Keywords   string         `xml:"keywords"`
	Date       *fb2Date       `xml:"date"`
	Coverpage  *fb2Coverpage  `xml:"coverpage"`
	Lang       string         `xml:"lang"`
	Sequences  []fb2Sequence  `xml:"sequence"`
}

type fb2Person struct {
	FirstName  string `xml:"first-name"`
	MiddleName string `xml:"middle-name"`
	LastName   string `xml:"last-name"`
	Nickname   string `xml:"nickname"`
}

type fb2Annotation struct {
	Content []byte `xml:",innerxml"`
}

type fb2Date struct {
	Value string `xml:"value,attr"`
	Text  string `xml:",chardata"`
}

type fb2Coverpage struct {
	Images []fb2Image `xml:"image"`
}

type fb2Image struct {
	Href      string `xml:"href,attr"`
	XlinkHref string `xml:"http://www.w3.org/1999/xlink href,attr"`
}

type fb2Sequence struct {
	Name   string `xml:"name,attr"`
	Number int    `xml:"number,attr"`
}

type fb2DocumentInfo struct {
	Date *fb2Date `xml:"date"`
}

type fb2Body struct {
	Name     string        `xml:"name,attr"`
	Title    *fb2Title     `xml:"title"`
	Epigraph []fb2Epigraph `xml:"epigraph"`
	Sections []fb2Section  `xml:"section"`
	Content  []byte        `xml:",innerxml"`
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
	Sections []fb2Section  `xml:"section"`
	Content  []byte        `xml:",innerxml"`
}

type fb2Binary struct {
	ID          string `xml:"id,attr"`
	ContentType string `xml:"content-type,attr"`
	Data        string `xml:",chardata"`
}

// EPUB creation
func createEPUB(doc *fb2Document, epubPath string) error {
	// Create ZIP file
	file, err := os.Create(epubPath)
	if err != nil {
		return err
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	defer zw.Close()

	bookUUID := uuid.New().String()
	title := strings.TrimSpace(doc.Description.TitleInfo.BookTitle)
	if title == "" {
		title = "Untitled"
	}

	lang := strings.TrimSpace(doc.Description.TitleInfo.Lang)
	if lang == "" {
		lang = "ru"
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

	// Write mimetype (must be first, uncompressed)
	mimeWriter, err := zw.CreateHeader(&zip.FileHeader{
		Name:   "mimetype",
		Method: zip.Store,
	})
	if err != nil {
		return err
	}
	mimeWriter.Write([]byte("application/epub+zip"))

	// Write META-INF/container.xml
	containerXML := `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`
	if err := writeToZip(zw, "META-INF/container.xml", []byte(containerXML)); err != nil {
		return err
	}

	// Process binaries (images)
	images := make(map[string]string) // id -> filename
	var imageItems []string
	for _, bin := range doc.Binaries {
		ext := getImageExtension(bin.ContentType)
		filename := fmt.Sprintf("images/%s%s", bin.ID, ext)
		images[bin.ID] = filename

		// Decode base64
		cleanData := strings.ReplaceAll(bin.Data, "\n", "")
		cleanData = strings.ReplaceAll(cleanData, "\r", "")
		cleanData = strings.ReplaceAll(cleanData, " ", "")
		imgData, err := base64.StdEncoding.DecodeString(cleanData)
		if err == nil {
			writeToZip(zw, "OEBPS/"+filename, imgData)
			imageItems = append(imageItems, fmt.Sprintf(`    <item id="%s" href="%s" media-type="%s"/>`,
				html.EscapeString(bin.ID), filename, bin.ContentType))
		}
	}

	// Find cover image
	var coverID string
	if doc.Description.TitleInfo.Coverpage != nil {
		for _, img := range doc.Description.TitleInfo.Coverpage.Images {
			href := img.Href
			if href == "" {
				href = img.XlinkHref
			}
			if strings.HasPrefix(href, "#") {
				coverID = strings.TrimPrefix(href, "#")
				break
			}
		}
	}

	// Convert FB2 body to XHTML
	bodyContent := convertBodiestoXHTML(doc.Bodies, images)

	// Write content XHTML
	contentXHTML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="%s">
<head>
  <title>%s</title>
  <link rel="stylesheet" type="text/css" href="stylesheet.css"/>
</head>
<body>
%s
</body>
</html>`, lang, html.EscapeString(title), bodyContent)
	if err := writeToZip(zw, "OEBPS/content.xhtml", []byte(contentXHTML)); err != nil {
		return err
	}

	// Write stylesheet
	stylesheet := getDefaultStylesheet()
	if err := writeToZip(zw, "OEBPS/stylesheet.css", []byte(stylesheet)); err != nil {
		return err
	}

	// Write content.opf
	coverMeta := ""
	if coverID != "" {
		coverMeta = fmt.Sprintf(`    <meta name="cover" content="%s"/>`, html.EscapeString(coverID))
	}

	contentOPF := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="BookId">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
    <dc:title>%s</dc:title>
    <dc:creator>%s</dc:creator>
    <dc:language>%s</dc:language>
    <dc:identifier id="BookId">urn:uuid:%s</dc:identifier>
    <dc:date>%s</dc:date>
%s
  </metadata>
  <manifest>
    <item id="content" href="content.xhtml" media-type="application/xhtml+xml"/>
    <item id="stylesheet" href="stylesheet.css" media-type="text/css"/>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
%s
  </manifest>
  <spine toc="ncx">
    <itemref idref="content"/>
  </spine>
</package>`,
		html.EscapeString(title),
		html.EscapeString(strings.Join(authors, ", ")),
		lang,
		bookUUID,
		time.Now().Format("2006-01-02"),
		coverMeta,
		strings.Join(imageItems, "\n"))
	if err := writeToZip(zw, "OEBPS/content.opf", []byte(contentOPF)); err != nil {
		return err
	}

	// Write toc.ncx
	tocNCX := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE ncx PUBLIC "-//NISO//DTD ncx 2005-1//EN" "http://www.daisy.org/z3986/2005/ncx-2005-1.dtd">
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <head>
    <meta name="dtb:uid" content="urn:uuid:%s"/>
    <meta name="dtb:depth" content="1"/>
  </head>
  <docTitle>
    <text>%s</text>
  </docTitle>
  <navMap>
    <navPoint id="content" playOrder="1">
      <navLabel>
        <text>%s</text>
      </navLabel>
      <content src="content.xhtml"/>
    </navPoint>
  </navMap>
</ncx>`, bookUUID, html.EscapeString(title), html.EscapeString(title))
	if err := writeToZip(zw, "OEBPS/toc.ncx", []byte(tocNCX)); err != nil {
		return err
	}

	return nil
}

func writeToZip(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func formatAuthorName(p fb2Person) string {
	parts := []string{}
	if p.FirstName != "" {
		parts = append(parts, strings.TrimSpace(p.FirstName))
	}
	if p.MiddleName != "" {
		parts = append(parts, strings.TrimSpace(p.MiddleName))
	}
	if p.LastName != "" {
		parts = append(parts, strings.TrimSpace(p.LastName))
	}
	if len(parts) == 0 && p.Nickname != "" {
		return strings.TrimSpace(p.Nickname)
	}
	return strings.Join(parts, " ")
}

func getImageExtension(contentType string) string {
	switch strings.ToLower(contentType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}

func convertBodiestoXHTML(bodies []fb2Body, images map[string]string) string {
	var buf bytes.Buffer

	for _, body := range bodies {
		// Body title
		if body.Title != nil {
			buf.WriteString("<h1>")
			buf.WriteString(convertFB2Content(body.Title.Content, images))
			buf.WriteString("</h1>\n")
		}

		// Body epigraphs
		for _, ep := range body.Epigraph {
			buf.WriteString("<div class=\"epigraph\">")
			buf.WriteString(convertFB2Content(ep.Content, images))
			buf.WriteString("</div>\n")
		}

		// Sections
		for _, section := range body.Sections {
			buf.WriteString(convertSection(section, 1, images))
		}
	}

	return buf.String()
}

func convertSection(s fb2Section, level int, images map[string]string) string {
	var buf bytes.Buffer

	// Section div
	if s.ID != "" {
		buf.WriteString(fmt.Sprintf("<div id=\"%s\" class=\"section\">\n", html.EscapeString(s.ID)))
	} else {
		buf.WriteString("<div class=\"section\">\n")
	}

	// Title
	if s.Title != nil {
		hLevel := level + 1
		if hLevel > 6 {
			hLevel = 6
		}
		buf.WriteString(fmt.Sprintf("<h%d>", hLevel))
		buf.WriteString(convertFB2Content(s.Title.Content, images))
		buf.WriteString(fmt.Sprintf("</h%d>\n", hLevel))
	}

	// Epigraphs
	for _, ep := range s.Epigraph {
		buf.WriteString("<div class=\"epigraph\">")
		buf.WriteString(convertFB2Content(ep.Content, images))
		buf.WriteString("</div>\n")
	}

	// Content
	content := convertFB2Content(s.Content, images)
	buf.WriteString(content)

	// Nested sections
	for _, sub := range s.Sections {
		buf.WriteString(convertSection(sub, level+1, images))
	}

	buf.WriteString("</div>\n")

	return buf.String()
}

func convertFB2Content(content []byte, images map[string]string) string {
	if len(content) == 0 {
		return ""
	}

	s := string(content)

	// Replace FB2 tags with XHTML equivalents
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
		{"<text-author>", "<p class=\"author\">"},
		{"</text-author>", "</p>"},
		{"<empty-line/>", "<br/>"},
		{"<empty-line />", "<br/>"},
		{"<subtitle>", "<h4 class=\"subtitle\">"},
		{"</subtitle>", "</h4>"},
	}

	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.old, r.new)
	}

	// Handle images
	for id, filename := range images {
		// Various image reference formats in FB2
		patterns := []string{
			fmt.Sprintf(`<image l:href="#%s"`, id),
			fmt.Sprintf(`<image xlink:href="#%s"`, id),
			fmt.Sprintf(`<image href="#%s"`, id),
		}
		for _, p := range patterns {
			if strings.Contains(s, p) {
				imgTag := fmt.Sprintf(`<img src="%s" alt=""/>`, filename)
				// Remove the entire image tag and replace
				s = strings.ReplaceAll(s, p+`/>`, imgTag)
				s = strings.ReplaceAll(s, p+` />`, imgTag)
			}
		}
	}

	// Remove leftover FB2 namespace prefixes
	s = strings.ReplaceAll(s, "l:", "")
	s = strings.ReplaceAll(s, "xlink:", "")

	// Handle internal links (a href)
	// FB2: <a l:href="#note1" type="note">
	// XHTML: <a href="#note1">
	s = strings.ReplaceAll(s, `type="note"`, "")
	s = strings.ReplaceAll(s, `<a href="`, `<a href="`)

	// Clean up nested sections (they get parsed as content too)
	// Remove section tags from content
	s = strings.ReplaceAll(s, "<section>", "")
	s = strings.ReplaceAll(s, "</section>", "")

	return s
}

func getDefaultStylesheet() string {
	return `body {
  font-family: Georgia, serif;
  margin: 1em;
  line-height: 1.6;
}

h1, h2, h3, h4, h5, h6 {
  font-family: Arial, sans-serif;
  margin-top: 1.5em;
  margin-bottom: 0.5em;
}

h1 { font-size: 1.8em; text-align: center; }
h2 { font-size: 1.5em; }
h3 { font-size: 1.3em; }
h4 { font-size: 1.1em; }

p {
  margin: 0.5em 0;
  text-indent: 1.5em;
}

.section {
  margin-bottom: 1em;
}

.epigraph {
  font-style: italic;
  margin: 1em 2em;
  text-align: right;
}

.poem {
  margin: 1em 2em;
}

.stanza {
  margin-bottom: 1em;
}

.verse {
  text-indent: 0;
  margin: 0;
}

.author {
  text-align: right;
  font-style: italic;
}

.subtitle {
  text-align: center;
  font-style: italic;
}

blockquote {
  margin: 1em 2em;
  font-style: italic;
}

img {
  max-width: 100%;
  height: auto;
  display: block;
  margin: 1em auto;
}

em { font-style: italic; }
strong { font-weight: bold; }
`
}

// ConvertFromReader converts FB2 from reader to specified format
func (c *Converter) ConvertFromReader(r io.Reader, format string, outputPath string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	switch strings.ToLower(format) {
	case "epub":
		return c.FB2DataToEPUB(data, outputPath)
	case "mobi":
		return c.FB2DataToMOBI(data, outputPath)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

// GetOutputPath returns the output path for a converted file
func GetOutputPath(inputPath, format string) string {
	ext := filepath.Ext(inputPath)
	base := strings.TrimSuffix(inputPath, ext)
	if strings.HasSuffix(strings.ToLower(base), ".fb2") {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return base + "." + strings.ToLower(format)
}
