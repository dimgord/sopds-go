package scanner

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"io"
	"strings"

	"github.com/dimgord/sopds-go/internal/database"
)

// FB2Metadata holds extracted FB2 book metadata
type FB2Metadata struct {
	Title      string
	Authors    []database.Author
	Genres     []string
	Series     []SeriesInfo
	Annotation string
	Lang       string
	DocDate    string
	Cover      []byte
	CoverType  string
}

// SeriesInfo holds series name and book number
type SeriesInfo struct {
	Name   string
	Number int
}

// FB2Parser parses FB2 ebook format
type FB2Parser struct {
	readCover bool
}

// NewFB2Parser creates a new FB2 parser
func NewFB2Parser(readCover bool) *FB2Parser {
	return &FB2Parser{
		readCover: readCover,
	}
}

// FB2 document structure for XML parsing
type fb2Document struct {
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
	SrcLang    string         `xml:"src-lang"`
	Sequences  []fb2Sequence  `xml:"sequence"`
}

type fb2Person struct {
	FirstName  string `xml:"first-name"`
	MiddleName string `xml:"middle-name"`
	LastName   string `xml:"last-name"`
	Nickname   string `xml:"nickname"`
}

type fb2Annotation struct {
	Paragraphs []string `xml:"p"`
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
	LHref     string `xml:"http://www.w3.org/ns/xlink href,attr"`
}

type fb2Sequence struct {
	Name   string `xml:"name,attr"`
	Number int    `xml:"number,attr"`
}

type fb2DocumentInfo struct {
	Date *fb2Date `xml:"date"`
}

type fb2Body struct {
	Name     string       `xml:"name,attr"`
	Sections []fb2Section `xml:"section"`
}

type fb2Section struct {
	Title string `xml:"title"`
}

type fb2Binary struct {
	ID          string `xml:"id,attr"`
	ContentType string `xml:"content-type,attr"`
	Data        string `xml:",chardata"`
}

// Parse parses FB2 content from reader
func (p *FB2Parser) Parse(r io.Reader) (*FB2Metadata, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// Handle BOM if present
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})

	var doc fb2Document
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	meta := &FB2Metadata{}

	// Extract title
	meta.Title = strings.TrimSpace(doc.Description.TitleInfo.BookTitle)

	// Extract language
	meta.Lang = strings.TrimSpace(doc.Description.TitleInfo.Lang)

	// Extract genres
	for _, g := range doc.Description.TitleInfo.Genres {
		genre := strings.TrimSpace(g)
		if genre != "" {
			meta.Genres = append(meta.Genres, genre)
		}
	}

	// Extract authors
	for _, author := range doc.Description.TitleInfo.Authors {
		a := database.Author{
			FirstName: strings.TrimSpace(author.FirstName),
			LastName:  strings.TrimSpace(author.LastName),
		}
		// If no first/last name, use nickname
		if a.FirstName == "" && a.LastName == "" && author.Nickname != "" {
			a.LastName = strings.TrimSpace(author.Nickname)
		}
		meta.Authors = append(meta.Authors, a)
	}

	// Extract annotation
	if doc.Description.TitleInfo.Annotation != nil {
		meta.Annotation = strings.Join(doc.Description.TitleInfo.Annotation.Paragraphs, "\n")
	}

	// Extract date
	if doc.Description.DocumentInfo.Date != nil {
		if doc.Description.DocumentInfo.Date.Value != "" {
			meta.DocDate = doc.Description.DocumentInfo.Date.Value
		} else {
			meta.DocDate = strings.TrimSpace(doc.Description.DocumentInfo.Date.Text)
		}
	}

	// Extract series
	for _, seq := range doc.Description.TitleInfo.Sequences {
		if seq.Name != "" {
			meta.Series = append(meta.Series, SeriesInfo{
				Name:   strings.TrimSpace(seq.Name),
				Number: seq.Number,
			})
		}
	}

	// Extract cover if requested
	if p.readCover && doc.Description.TitleInfo.Coverpage != nil {
		coverID := ""
		for _, img := range doc.Description.TitleInfo.Coverpage.Images {
			// Try different href attributes
			href := img.Href
			if href == "" {
				href = img.XlinkHref
			}
			if href == "" {
				href = img.LHref
			}
			// Extract ID from href (remove # prefix)
			if strings.HasPrefix(href, "#") {
				coverID = strings.TrimPrefix(href, "#")
				break
			}
		}

		if coverID != "" {
			for _, bin := range doc.Binaries {
				if strings.EqualFold(bin.ID, coverID) {
					// Decode base64 data
					cleanData := strings.ReplaceAll(bin.Data, "\n", "")
					cleanData = strings.ReplaceAll(cleanData, "\r", "")
					cleanData = strings.ReplaceAll(cleanData, " ", "")

					coverData, err := base64.StdEncoding.DecodeString(cleanData)
					if err == nil {
						meta.Cover = coverData
						meta.CoverType = bin.ContentType
					}
					break
				}
			}
		}
	}

	return meta, nil
}

// ParsePartial parses only the beginning of FB2 file (faster, no cover)
func (p *FB2Parser) ParsePartial(r io.Reader, maxBytes int) (*FB2Metadata, error) {
	// Read limited data
	limitReader := io.LimitReader(r, int64(maxBytes))
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, err
	}

	// Handle BOM if present
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})

	// Try to find </description> and truncate there for faster parsing
	descEnd := bytes.Index(data, []byte("</description>"))
	if descEnd > 0 {
		// Add closing tags to make valid XML
		data = append(data[:descEnd+14], []byte("</FictionBook>")...)
	}

	var doc fb2Document
	// Try parsing - may fail with partial data
	if err := xml.Unmarshal(data, &doc); err != nil {
		// If partial parsing fails, return what we can
		return &FB2Metadata{}, nil
	}

	meta := &FB2Metadata{}

	// Extract title
	meta.Title = strings.TrimSpace(doc.Description.TitleInfo.BookTitle)

	// Extract language
	meta.Lang = strings.TrimSpace(doc.Description.TitleInfo.Lang)

	// Extract genres
	for _, g := range doc.Description.TitleInfo.Genres {
		genre := strings.TrimSpace(g)
		if genre != "" {
			meta.Genres = append(meta.Genres, genre)
		}
	}

	// Extract authors
	for _, author := range doc.Description.TitleInfo.Authors {
		a := database.Author{
			FirstName: strings.TrimSpace(author.FirstName),
			LastName:  strings.TrimSpace(author.LastName),
		}
		if a.FirstName == "" && a.LastName == "" && author.Nickname != "" {
			a.LastName = strings.TrimSpace(author.Nickname)
		}
		meta.Authors = append(meta.Authors, a)
	}

	// Extract annotation
	if doc.Description.TitleInfo.Annotation != nil {
		meta.Annotation = strings.Join(doc.Description.TitleInfo.Annotation.Paragraphs, "\n")
	}

	// Extract date
	if doc.Description.DocumentInfo.Date != nil {
		if doc.Description.DocumentInfo.Date.Value != "" {
			meta.DocDate = doc.Description.DocumentInfo.Date.Value
		} else {
			meta.DocDate = strings.TrimSpace(doc.Description.DocumentInfo.Date.Text)
		}
	}

	// Extract series
	for _, seq := range doc.Description.TitleInfo.Sequences {
		if seq.Name != "" {
			meta.Series = append(meta.Series, SeriesInfo{
				Name:   strings.TrimSpace(seq.Name),
				Number: seq.Number,
			})
		}
	}

	return meta, nil
}
