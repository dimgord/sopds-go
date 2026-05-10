package opds

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/dimgord/sopds-go/internal/config"
	"github.com/dimgord/sopds-go/internal/database"
)

func TestMimeType(t *testing.T) {
	tests := []struct {
		format   string
		expected string
	}{
		// Ebook formats
		{"fb2", "application/fb2+xml"},
		{"epub", "application/epub+zip"},
		{"mobi", "application/x-mobipocket-ebook"},
		{"pdf", "application/pdf"},
		{"djvu", "image/vnd.djvu"},
		{"txt", "text/plain"},
		{"rtf", "application/rtf"},
		{"doc", "application/msword"},
		// Audio formats
		{"mp3", "audio/mpeg"},
		{"m4b", "audio/mp4"},
		{"m4a", "audio/mp4"},
		{"aac", "audio/mp4"},
		{"flac", "audio/flac"},
		{"ogg", "audio/ogg"},
		{"opus", "audio/ogg"},
		{"wav", "audio/wav"},
		// Unknown
		{"unknown", "application/octet-stream"},
		{"", "application/octet-stream"},
	}

	for _, tc := range tests {
		result := MimeType(tc.format)
		if result != tc.expected {
			t.Errorf("MimeType(%q) = %q, expected %q", tc.format, result, tc.expected)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds  int
		expected string
	}{
		{0, ""},
		{-1, ""},
		{30, "0m"},
		{60, "1m"},
		{90, "1m"},
		{3600, "1h"},
		{3660, "1h 1m"},
		{7200, "2h"},
		{7320, "2h 2m"},
		{45, "0m"},
	}

	for _, tc := range tests {
		result := FormatDuration(tc.seconds)
		if result != tc.expected {
			t.Errorf("FormatDuration(%d) = %q, expected %q", tc.seconds, result, tc.expected)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tc := range tests {
		result := formatSize(tc.bytes)
		if result != tc.expected {
			t.Errorf("formatSize(%d) = %q, expected %q", tc.bytes, result, tc.expected)
		}
	}
}

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		strs     []string
		sep      string
		expected string
	}{
		{[]string{}, ", ", ""},
		{[]string{"one"}, ", ", "one"},
		{[]string{"one", "two"}, ", ", "one, two"},
		{[]string{"a", "b", "c"}, "-", "a-b-c"},
	}

	for _, tc := range tests {
		result := joinStrings(tc.strs, tc.sep)
		if result != tc.expected {
			t.Errorf("joinStrings(%v, %q) = %q, expected %q",
				tc.strs, tc.sep, result, tc.expected)
		}
	}
}

func TestNewFeedBuilder(t *testing.T) {
	cfg := &config.Config{
		Site: config.SiteConfig{
			Title: "Test Library",
		},
	}

	builder := NewFeedBuilder(cfg, "http://localhost:8080/opds")

	if builder == nil {
		t.Fatal("NewFeedBuilder returned nil")
	}
	if builder.cfg != cfg {
		t.Error("FeedBuilder.cfg not set correctly")
	}
	if builder.baseURL != "http://localhost:8080/opds" {
		t.Errorf("FeedBuilder.baseURL = %q, expected http://localhost:8080/opds", builder.baseURL)
	}
}

func TestNewFeed(t *testing.T) {
	cfg := &config.Config{
		Site: config.SiteConfig{
			Title:    "Test Library",
			Subtitle: "Test Subtitle",
			Author:   "Test Author",
			Email:    "test@example.com",
		},
	}

	builder := NewFeedBuilder(cfg, "http://localhost:8080/opds")
	feed := builder.NewFeed("My Feed", "myfeed")

	if feed.Title != "My Feed" {
		t.Errorf("Feed.Title = %q, expected My Feed", feed.Title)
	}
	if feed.ID != "urn:sopds:myfeed" {
		t.Errorf("Feed.ID = %q, expected urn:sopds:myfeed", feed.ID)
	}
	if feed.NSAtom != NSAtom {
		t.Errorf("Feed.NSAtom = %q, expected %q", feed.NSAtom, NSAtom)
	}
}

func TestNewFeedWithPath(t *testing.T) {
	cfg := &config.Config{}
	builder := NewFeedBuilder(cfg, "http://localhost:8080/opds")
	feed := builder.NewFeedWithPath("Authors", "authors", "/authors", "/")

	// Check self link
	foundSelf := false
	foundUp := false
	for _, link := range feed.Links {
		if link.Rel == "self" && link.Href == "http://localhost:8080/opds/authors" {
			foundSelf = true
		}
		if link.Rel == "up" && link.Href == "http://localhost:8080/opds/" {
			foundUp = true
		}
	}

	if !foundSelf {
		t.Error("Feed missing self link with correct path")
	}
	if !foundUp {
		t.Error("Feed missing up link")
	}
}

func TestNavigationEntry(t *testing.T) {
	cfg := &config.Config{}
	builder := NewFeedBuilder(cfg, "http://localhost:8080/opds")

	entry := builder.NavigationEntry("Authors", "authors", "/opds/authors", "100 authors")

	if entry.Title != "Authors" {
		t.Errorf("Entry.Title = %q, expected Authors", entry.Title)
	}
	if entry.ID != "urn:sopds:nav:authors" {
		t.Errorf("Entry.ID = %q, expected urn:sopds:nav:authors", entry.ID)
	}
	if entry.Content.Text != "100 authors" {
		t.Errorf("Entry.Content.Text = %q, expected 100 authors", entry.Content.Text)
	}
	if len(entry.Links) != 1 {
		t.Fatalf("Entry should have 1 link, got %d", len(entry.Links))
	}
	if entry.Links[0].Rel != "subsection" {
		t.Errorf("Link.Rel = %q, expected subsection", entry.Links[0].Rel)
	}
}

func TestAcquisitionEntry(t *testing.T) {
	cfg := &config.Config{}
	builder := NewFeedBuilder(cfg, "http://localhost:8080/opds")

	book := &database.Book{
		ID:           1,
		Title:        "Test Book",
		Format:       "fb2",
		Filesize:     1024,
		Annotation:   "Test annotation",
		RegisterDate: time.Now(),
		Cover:        "cover.jpg",
		CoverType:    "image/jpeg",
	}
	authors := []database.Author{{ID: 1, FirstName: "John", LastName: "Doe"}}
	genres := []database.Genre{{ID: 1, Genre: "fiction", Subsection: "Science Fiction"}}
	series := []database.BookSeries{{SeriesID: 1, Name: "Test Series", SerNo: 1}}

	entry := builder.AcquisitionEntry(book, authors, genres, series)

	if entry.Title != "Test Book" {
		t.Errorf("Entry.Title = %q, expected Test Book", entry.Title)
	}
	if entry.ID != "urn:book:1" {
		t.Errorf("Entry.ID = %q, expected urn:book:1", entry.ID)
	}
	if entry.Author.Name != "Doe John" {
		t.Errorf("Entry.Author.Name = %q, expected Doe John", entry.Author.Name)
	}

	// Check for download link
	foundDownload := false
	foundCover := false
	for _, link := range entry.Links {
		if strings.Contains(link.Rel, "acquisition") {
			foundDownload = true
		}
		if strings.Contains(link.Rel, "image") {
			foundCover = true
		}
	}

	if !foundDownload {
		t.Error("Entry missing acquisition link")
	}
	if !foundCover {
		t.Error("Entry missing cover image link")
	}

	// Check category
	if entry.Category == nil {
		t.Fatal("Entry.Category is nil")
	}
	if entry.Category.Term != "fiction" {
		t.Errorf("Category.Term = %q, expected fiction", entry.Category.Term)
	}
}

func TestPagination(t *testing.T) {
	cfg := &config.Config{}
	builder := NewFeedBuilder(cfg, "http://localhost:8080/opds")
	feed := builder.NewFeed("Test", "test")

	// Test middle page
	builder.Pagination(feed, "/authors", 2, 5)

	foundPrev := false
	foundNext := false
	foundFirst := false
	foundLast := false

	for _, link := range feed.Links {
		switch link.Rel {
		case "prev":
			foundPrev = true
			if !strings.Contains(link.Href, "page=1") {
				t.Errorf("Prev link should have page=1, got %q", link.Href)
			}
		case "next":
			foundNext = true
			if !strings.Contains(link.Href, "page=3") {
				t.Errorf("Next link should have page=3, got %q", link.Href)
			}
		case "first":
			foundFirst = true
		case "last":
			foundLast = true
		}
	}

	if !foundPrev {
		t.Error("Missing prev link for middle page")
	}
	if !foundNext {
		t.Error("Missing next link for middle page")
	}
	if !foundFirst {
		t.Error("Missing first link for middle page")
	}
	if !foundLast {
		t.Error("Missing last link for middle page")
	}
}

func TestPaginationFirstPage(t *testing.T) {
	cfg := &config.Config{}
	builder := NewFeedBuilder(cfg, "http://localhost:8080/opds")
	feed := builder.NewFeed("Test", "test")

	builder.Pagination(feed, "/authors", 0, 5)

	for _, link := range feed.Links {
		if link.Rel == "prev" || link.Rel == "first" {
			t.Errorf("First page should not have %s link", link.Rel)
		}
	}
}

func TestPaginationLastPage(t *testing.T) {
	cfg := &config.Config{}
	builder := NewFeedBuilder(cfg, "http://localhost:8080/opds")
	feed := builder.NewFeed("Test", "test")

	builder.Pagination(feed, "/authors", 4, 5)

	for _, link := range feed.Links {
		if link.Rel == "next" || link.Rel == "last" {
			t.Errorf("Last page should not have %s link", link.Rel)
		}
	}
}

func TestPaginationWithQueryParams(t *testing.T) {
	cfg := &config.Config{}
	builder := NewFeedBuilder(cfg, "http://localhost:8080/opds")
	feed := builder.NewFeed("Test", "test")

	builder.Pagination(feed, "/search?q=test", 1, 5)

	for _, link := range feed.Links {
		if link.Rel == "next" {
			if !strings.Contains(link.Href, "&page=") {
				t.Errorf("Should use & separator for existing query params, got %q", link.Href)
			}
		}
	}
}

func TestFeedRender(t *testing.T) {
	feed := &Feed{
		NSAtom:  NSAtom,
		Title:   "Test Feed",
		ID:      "urn:test",
		Updated: "2024-01-01T00:00:00Z",
	}

	data, err := feed.Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	// Should start with XML header
	if !strings.HasPrefix(string(data), "<?xml") {
		t.Error("Rendered XML should start with XML header")
	}

	// Should be valid XML
	var parsed Feed
	if err := xml.Unmarshal(data, &parsed); err != nil {
		t.Errorf("Rendered XML is invalid: %v", err)
	}

	if parsed.Title != "Test Feed" {
		t.Errorf("Parsed Title = %q, expected Test Feed", parsed.Title)
	}
}

func TestMainMenu(t *testing.T) {
	cfg := &config.Config{
		Site: config.SiteConfig{
			MainTitle: "My Library",
			ID:        "mylib",
		},
	}
	builder := NewFeedBuilder(cfg, "http://localhost:8080/opds")

	info := &database.DBInfo{
		BooksCount:    100,
		AuthorsCount:  50,
		CatalogsCount: 10,
		GenresCount:   20,
		SeriesCount:   30,
	}
	newInfo := &database.NewInfo{NewBooks: 5}

	feed := builder.MainMenu(info, newInfo)

	if feed.Title != "My Library" {
		t.Errorf("Feed.Title = %q, expected My Library", feed.Title)
	}

	// Should have 6 entries (catalogs, authors, titles, genres, series, new)
	if len(feed.Entries) != 6 {
		t.Errorf("Feed should have 6 entries, got %d", len(feed.Entries))
	}
}

func TestOpenSearchDescription(t *testing.T) {
	cfg := &config.Config{
		Site: config.SiteConfig{
			Title: "Test Library",
		},
	}
	builder := NewFeedBuilder(cfg, "http://localhost:8080/opds")

	xml := builder.OpenSearchDescription()

	if !strings.Contains(xml, "Test Library") {
		t.Error("OpenSearch should contain site title")
	}
	if !strings.Contains(xml, "{searchTerms}") {
		t.Error("OpenSearch should contain searchTerms template")
	}
	if !strings.Contains(xml, "http://localhost:8080/opds/search") {
		t.Error("OpenSearch should contain search URL")
	}
}

func TestLinkStruct(t *testing.T) {
	link := Link{
		Rel:   "self",
		Href:  "http://example.com",
		Type:  "application/atom+xml",
		Title: "Test Link",
	}

	if link.Rel != "self" {
		t.Errorf("Link.Rel = %q, expected self", link.Rel)
	}
	if link.Href != "http://example.com" {
		t.Errorf("Link.Href = %q, expected http://example.com", link.Href)
	}
}

func TestEntryStruct(t *testing.T) {
	entry := Entry{
		Title:   "Test Entry",
		ID:      "urn:test:1",
		Updated: "2024-01-01T00:00:00Z",
		Content: &Content{Type: "text", Text: "Test content"},
	}

	if entry.Title != "Test Entry" {
		t.Errorf("Entry.Title = %q, expected Test Entry", entry.Title)
	}
	if entry.Content.Text != "Test content" {
		t.Errorf("Entry.Content.Text = %q, expected Test content", entry.Content.Text)
	}
}
