package opds

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/sopds/sopds-go/internal/config"
	"github.com/sopds/sopds-go/internal/database"
)

// OPDS Atom feed namespaces
const (
	NSAtom   = "http://www.w3.org/2005/Atom"
	NSOPDS   = "http://opds-spec.org/2010/catalog"
	NSDublin = "http://purl.org/dc/terms/"
	NSXLink  = "http://www.w3.org/1999/xlink"
)

// Feed represents an OPDS Atom feed
type Feed struct {
	XMLName   xml.Name `xml:"feed"`
	NSAtom    string   `xml:"xmlns,attr"`
	NSOPDS    string   `xml:"xmlns:opds,attr,omitempty"`
	NSDublin  string   `xml:"xmlns:dc,attr,omitempty"`
	Title     string   `xml:"title"`
	Subtitle  string   `xml:"subtitle,omitempty"`
	ID        string   `xml:"id"`
	Updated   string   `xml:"updated"`
	Icon      string   `xml:"icon,omitempty"`
	Author    *Author  `xml:"author,omitempty"`
	Links     []Link   `xml:"link"`
	Entries   []Entry  `xml:"entry"`
}

// Author represents a feed or entry author
type Author struct {
	Name  string `xml:"name"`
	URI   string `xml:"uri,omitempty"`
	Email string `xml:"email,omitempty"`
}

// Link represents an Atom link
type Link struct {
	Rel      string `xml:"rel,attr,omitempty"`
	Href     string `xml:"href,attr"`
	Type     string `xml:"type,attr,omitempty"`
	Title    string `xml:"title,attr,omitempty"`
	FacetGrp string `xml:"opds:facetGroup,attr,omitempty"`
	Active   bool   `xml:"opds:activeFacet,attr,omitempty"`
}

// Entry represents an Atom entry (navigation or acquisition)
type Entry struct {
	Title     string    `xml:"title"`
	ID        string    `xml:"id"`
	Updated   string    `xml:"updated"`
	Published string    `xml:"published,omitempty"`
	Author    *Author   `xml:"author,omitempty"`
	Links     []Link    `xml:"link"`
	Summary   string    `xml:"summary,omitempty"`
	Content   *Content  `xml:"content,omitempty"`
	Category  *Category `xml:"category,omitempty"`
}

// Content represents entry content
type Content struct {
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}

// Category represents an entry category
type Category struct {
	Scheme string `xml:"scheme,attr,omitempty"`
	Term   string `xml:"term,attr"`
	Label  string `xml:"label,attr,omitempty"`
}

// FeedBuilder builds OPDS feeds
type FeedBuilder struct {
	cfg      *config.Config
	baseURL  string
	now      string
}

// NewFeedBuilder creates a new feed builder
func NewFeedBuilder(cfg *config.Config, baseURL string) *FeedBuilder {
	return &FeedBuilder{
		cfg:     cfg,
		baseURL: baseURL,
		now:     time.Now().UTC().Format(time.RFC3339),
	}
}

// NewFeed creates a new empty feed with standard settings (for root)
func (b *FeedBuilder) NewFeed(title, id string) *Feed {
	return b.NewFeedWithPath(title, id, "/", "")
}

// NewFeedWithPath creates a feed with correct self and up links
func (b *FeedBuilder) NewFeedWithPath(title, id, selfPath, upPath string) *Feed {
	links := []Link{
		{Rel: "start", Href: b.baseURL + "/", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
		{Rel: "self", Href: b.baseURL + selfPath, Type: "application/atom+xml;profile=opds-catalog"},
		{Rel: "search", Href: b.baseURL + "/opensearch.xml", Type: "application/opensearchdescription+xml"},
	}

	// Add up link for non-root feeds
	if upPath != "" {
		links = append(links, Link{
			Rel:  "up",
			Href: b.baseURL + upPath,
			Type: "application/atom+xml;profile=opds-catalog",
		})
	}

	// Ensure ID is a proper URN
	feedID := id
	if len(id) > 0 && id[0] != 'u' || !strings.HasPrefix(id, "urn:") {
		feedID = fmt.Sprintf("urn:sopds:%s", id)
	}

	return &Feed{
		NSAtom:   NSAtom,
		NSOPDS:   NSOPDS,
		NSDublin: NSDublin,
		Title:    title,
		Subtitle: b.cfg.Site.Subtitle,
		ID:       feedID,
		Updated:  b.now,
		Icon:     b.cfg.Site.Icon,
		Author: &Author{
			Name:  b.cfg.Site.Author,
			Email: b.cfg.Site.Email,
		},
		Links: links,
	}
}

// OpenSearchDescription returns the OpenSearch description XML
func (b *FeedBuilder) OpenSearchDescription() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
  <ShortName>` + b.cfg.Site.Title + `</ShortName>
  <Description>Search ` + b.cfg.Site.Title + `</Description>
  <InputEncoding>UTF-8</InputEncoding>
  <OutputEncoding>UTF-8</OutputEncoding>
  <Url type="application/atom+xml;profile=opds-catalog" template="` + b.baseURL + `/search?q={searchTerms}"/>
</OpenSearchDescription>`
}

// NavigationEntry creates a navigation entry (folder/category)
func (b *FeedBuilder) NavigationEntry(title, id, href, content string) Entry {
	return Entry{
		Title:   title,
		ID:      fmt.Sprintf("urn:sopds:nav:%s", id),
		Updated: b.now,
		Links: []Link{
			{
				Rel:  "subsection",
				Href: href,
				Type: "application/atom+xml;profile=opds-catalog;kind=navigation",
			},
		},
		Content: &Content{
			Type: "text",
			Text: content,
		},
	}
}

// AcquisitionEntry creates an acquisition entry (book)
func (b *FeedBuilder) AcquisitionEntry(book *database.Book, authors []database.Author, genres []database.Genre, series []database.BookSeries) Entry {
	entry := Entry{
		Title:   book.Title,
		ID:      fmt.Sprintf("urn:book:%d", book.ID),
		Updated: book.RegisterDate.Format(time.RFC3339),
	}

	if len(authors) > 0 {
		entry.Author = &Author{
			Name: authors[0].FullName(),
		}
	}

	// Add download links for each format
	entry.Links = append(entry.Links, Link{
		Rel:   "http://opds-spec.org/acquisition/open-access",
		Href:  fmt.Sprintf("%s/book/%d/download", b.baseURL, book.ID),
		Type:  MimeType(book.Format),
		Title: fmt.Sprintf("Download %s", book.Format),
	})

	// Add cover link if available
	if book.Cover != "" {
		entry.Links = append(entry.Links, Link{
			Rel:  "http://opds-spec.org/image",
			Href: fmt.Sprintf("%s/book/%d/cover", b.baseURL, book.ID),
			Type: book.CoverType,
		})
		entry.Links = append(entry.Links, Link{
			Rel:  "http://opds-spec.org/image/thumbnail",
			Href: fmt.Sprintf("%s/book/%d/cover?size=thumb", b.baseURL, book.ID),
			Type: book.CoverType,
		})
	}

	// Build content with metadata
	content := ""
	if book.Annotation != "" {
		content = book.Annotation
	}

	// Add author info
	if len(authors) > 0 {
		authorNames := make([]string, len(authors))
		for i, a := range authors {
			authorNames[i] = a.FullName()
		}
		content += fmt.Sprintf("\nAuthors: %s", joinStrings(authorNames, ", "))
	}

	// Add series info
	if len(series) > 0 {
		for _, s := range series {
			if s.SerNo > 0 {
				content += fmt.Sprintf("\nSeries: %s #%d", s.Name, s.SerNo)
			} else {
				content += fmt.Sprintf("\nSeries: %s", s.Name)
			}
		}
	}

	// Add file info
	content += fmt.Sprintf("\nFormat: %s, Size: %s", book.Format, formatSize(book.Filesize))

	entry.Content = &Content{
		Type: "text",
		Text: content,
	}

	// Add genre category
	if len(genres) > 0 {
		entry.Category = &Category{
			Term:  genres[0].Genre,
			Label: genres[0].Subsection,
		}
	}

	return entry
}

// MainMenu creates the main menu feed
func (b *FeedBuilder) MainMenu(info *database.DBInfo, newInfo *database.NewInfo) *Feed {
	feed := b.NewFeed(b.cfg.Site.MainTitle, b.cfg.Site.ID)

	// Add navigation entries
	feed.Entries = []Entry{
		b.NavigationEntry("By Catalogs", "catalogs", b.baseURL+"/catalogs",
			fmt.Sprintf("%d catalogs", info.CatalogsCount)),
		b.NavigationEntry("By Authors", "authors", b.baseURL+"/authors",
			fmt.Sprintf("%d authors", info.AuthorsCount)),
		b.NavigationEntry("By Titles", "titles", b.baseURL+"/titles",
			fmt.Sprintf("%d books", info.BooksCount)),
		b.NavigationEntry("By Genres", "genres", b.baseURL+"/genres",
			fmt.Sprintf("%d genres", info.GenresCount)),
		b.NavigationEntry("By Series", "series", b.baseURL+"/series",
			fmt.Sprintf("%d series", info.SeriesCount)),
	}

	if newInfo != nil && newInfo.NewBooks > 0 {
		feed.Entries = append(feed.Entries,
			b.NavigationEntry("New Books", "new", b.baseURL+"/new",
				fmt.Sprintf("%d new books", newInfo.NewBooks)))
	}

	return feed
}

// Pagination adds pagination links to a feed
func (b *FeedBuilder) Pagination(feed *Feed, basePath string, page, totalPages int) {
	// Determine separator: use & if basePath already has query params, otherwise ?
	sep := "?"
	if strings.Contains(basePath, "?") {
		sep = "&"
	}

	if page > 0 {
		feed.Links = append(feed.Links, Link{
			Rel:  "prev",
			Href: fmt.Sprintf("%s%s%spage=%d", b.baseURL, basePath, sep, page-1),
			Type: "application/atom+xml;profile=opds-catalog",
		})
		feed.Links = append(feed.Links, Link{
			Rel:  "first",
			Href: fmt.Sprintf("%s%s%spage=0", b.baseURL, basePath, sep),
			Type: "application/atom+xml;profile=opds-catalog",
		})
	}

	if page < totalPages-1 {
		feed.Links = append(feed.Links, Link{
			Rel:  "next",
			Href: fmt.Sprintf("%s%s%spage=%d", b.baseURL, basePath, sep, page+1),
			Type: "application/atom+xml;profile=opds-catalog",
		})
		feed.Links = append(feed.Links, Link{
			Rel:  "last",
			Href: fmt.Sprintf("%s%s%spage=%d", b.baseURL, basePath, sep, totalPages-1),
			Type: "application/atom+xml;profile=opds-catalog",
		})
	}
}

// Render renders the feed to XML bytes
func (feed *Feed) Render() ([]byte, error) {
	output, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), output...), nil
}

// Helper functions

// MimeType returns the MIME type for a book format
func MimeType(format string) string {
	switch format {
	// Ebook formats
	case "fb2":
		return "application/fb2+xml"
	case "epub":
		return "application/epub+zip"
	case "mobi":
		return "application/x-mobipocket-ebook"
	case "pdf":
		return "application/pdf"
	case "djvu":
		return "image/vnd.djvu"
	case "txt":
		return "text/plain"
	case "rtf":
		return "application/rtf"
	case "doc":
		return "application/msword"
	// Audio formats
	case "mp3":
		return "audio/mpeg"
	case "m4b", "m4a", "aac":
		return "audio/mp4"
	case "flac":
		return "audio/flac"
	case "ogg", "opus":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}

// FormatDuration formats duration in seconds as "Xh Ym"
func FormatDuration(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	if h > 0 {
		if m > 0 {
			return fmt.Sprintf("%dh %dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
