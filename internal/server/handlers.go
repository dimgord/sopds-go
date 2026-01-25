package server

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
	"github.com/go-chi/chi/v5"
	go7z "github.com/saracen/go7z"
	"github.com/sopds/sopds-go/internal/database"
	"github.com/sopds/sopds-go/internal/infrastructure/persistence"
	"github.com/sopds/sopds-go/internal/opds"
)

// handleMainMenu renders the main OPDS menu
func (s *Server) handleMainMenu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	// Get database info
	info, err := s.svc.GetDBInfo(ctx, false)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get database info")
		return
	}

	// Get new books info
	var newInfo *database.NewInfo
	if s.config.Scanner.Duplicates != "none" {
		newInfo, _ = s.svc.GetNewInfo(ctx, 7) // Last 7 days
	}

	feed := builder.MainMenu(info, newInfo)
	s.writeOPDS(w, feed)
}

// handleOpenSearch returns the OpenSearch description document
func (s *Server) handleOpenSearch(w http.ResponseWriter, r *http.Request) {
	builder := s.getFeedBuilder(r)
	w.Header().Set("Content-Type", "application/opensearchdescription+xml; charset=utf-8")
	w.Write([]byte(builder.OpenSearchDescription()))
}

// handleCatalogs renders the root catalog listing
func (s *Server) handleCatalogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	page := getPage(r)
	pagination := database.NewPagination(page, s.config.Server.Port) // Use port as limit for now

	// Get root catalogs (parent_id is NULL)
	items, err := s.svc.GetItemsInCatalog(ctx, 0, pagination, false)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get catalogs")
		return
	}

	feed := builder.NewFeedWithPath("Catalogs", "catalogs", "/catalogs", "/")
	for _, item := range items {
		if item.ItemType == "catalog" {
			entry := builder.NavigationEntry(
				item.Name,
				fmt.Sprintf("catalog:%d", item.ID),
				fmt.Sprintf("%s/catalogs/%d", s.getBaseURL(r), item.ID),
				"Browse catalog",
			)
			feed.Entries = append(feed.Entries, entry)
		} else {
			book := &database.Book{
				ID:           item.ID,
				Filename:     item.Name,
				Path:         item.Path,
				Title:        item.Title,
				Format:       item.Format,
				Filesize:     item.Filesize,
				Annotation:   item.Annotation,
				DocDate:      item.DocDate,
				RegisterDate: item.Date,
				Cover:        item.Cover,
				CoverType:    item.CoverType,
			}
			entry := builder.AcquisitionEntry(book, nil, nil, nil)
			feed.Entries = append(feed.Entries, entry)
		}
	}

	s.writeOPDS(w, feed)
}

// handleCatalog renders a specific catalog
func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	catID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid catalog ID")
		return
	}

	page := getPage(r)
	pagination := database.NewPagination(page, 50)

	items, err := s.svc.GetItemsInCatalog(ctx, catID, pagination, false)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get catalog items")
		return
	}

	cat, _ := s.svc.GetCatalog(ctx, catID)
	title := "Catalog"
	if cat != nil {
		title = cat.Name
	}

	feed := builder.NewFeedWithPath(title, fmt.Sprintf("catalog:%d", catID), fmt.Sprintf("/catalogs/%d", catID), "/catalogs")
	for _, item := range items {
		if item.ItemType == "catalog" {
			entry := builder.NavigationEntry(
				item.Name,
				fmt.Sprintf("catalog:%d", item.ID),
				fmt.Sprintf("%s/catalogs/%d", s.getBaseURL(r), item.ID),
				"Browse catalog",
			)
			feed.Entries = append(feed.Entries, entry)
		} else {
			book := &database.Book{
				ID:           item.ID,
				Filename:     item.Name,
				Path:         item.Path,
				Title:        item.Title,
				Format:       item.Format,
				Filesize:     item.Filesize,
				Annotation:   item.Annotation,
				DocDate:      item.DocDate,
				RegisterDate: item.Date,
				Cover:        item.Cover,
				CoverType:    item.CoverType,
			}
			authors, _ := s.svc.GetBookAuthors(ctx, item.ID)
			genres, _ := s.svc.GetBookGenres(ctx, item.ID)
			series, _ := s.svc.GetBookSeries(ctx, item.ID)
			entry := builder.AcquisitionEntry(book, authors, genres, series)
			feed.Entries = append(feed.Entries, entry)
		}
	}

	s.writeOPDS(w, feed)
}

// handleAuthors renders the author listing
func (s *Server) handleAuthors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	letter := r.URL.Query().Get("letter")
	page := getPage(r)
	pagination := database.NewPagination(page, 50)

	if letter == "" {
		// Show alphabet
		feed := builder.NewFeedWithPath("Authors", "authors", "/authors", "/")
		letters := []string{"А", "Б", "В", "Г", "Д", "Е", "Ж", "З", "И", "К", "Л", "М", "Н", "О", "П", "Р", "С", "Т", "У", "Ф", "Х", "Ц", "Ч", "Ш", "Щ", "Э", "Ю", "Я",
			"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z"}
		for _, l := range letters {
			entry := builder.NavigationEntry(
				l,
				fmt.Sprintf("authors:letter:%s", l),
				fmt.Sprintf("%s/authors?letter=%s", s.getBaseURL(r), url.QueryEscape(l)),
				"",
			)
			feed.Entries = append(feed.Entries, entry)
		}
		s.writeOPDS(w, feed)
		return
	}

	// Show authors starting with letter
	authors, err := s.svc.GetAuthorsByLetter(ctx, letter, pagination)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get authors")
		return
	}

	feed := builder.NewFeedWithPath(fmt.Sprintf("Authors: %s", letter), fmt.Sprintf("authors:letter:%s", letter), fmt.Sprintf("/authors?letter=%s", url.QueryEscape(letter)), "/authors")
	for _, author := range authors {
		entry := builder.NavigationEntry(
			author.FullName(),
			fmt.Sprintf("author:%d", author.ID),
			fmt.Sprintf("%s/authors/%d", s.getBaseURL(r), author.ID),
			"",
		)
		feed.Entries = append(feed.Entries, entry)
	}

	// Add pagination links
	totalPages := int((pagination.TotalCount + int64(pagination.Limit) - 1) / int64(pagination.Limit))
	builder.Pagination(feed, fmt.Sprintf("/authors?letter=%s", url.QueryEscape(letter)), page, totalPages)

	s.writeOPDS(w, feed)
}

// handleAuthor renders books by a specific author
func (s *Server) handleAuthor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	authorID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid author ID")
		return
	}

	page := getPage(r)
	pagination := database.NewPagination(page, 50)

	books, err := s.svc.GetBooksForAuthor(ctx, authorID, pagination, false)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get books")
		return
	}

	feed := builder.NewFeedWithPath("Author's Books", fmt.Sprintf("author:%d", authorID), fmt.Sprintf("/authors/%d", authorID), "/authors")
	for _, book := range books {
		authors, _ := s.svc.GetBookAuthors(ctx, book.ID)
		genres, _ := s.svc.GetBookGenres(ctx, book.ID)
		series, _ := s.svc.GetBookSeries(ctx, book.ID)
		entry := builder.AcquisitionEntry(&book, authors, genres, series)
		feed.Entries = append(feed.Entries, entry)
	}

	// Add pagination links
	totalPages := int((pagination.TotalCount + int64(pagination.Limit) - 1) / int64(pagination.Limit))
	builder.Pagination(feed, fmt.Sprintf("/authors/%d", authorID), page, totalPages)

	s.writeOPDS(w, feed)
}

// handleTitles renders books by title
func (s *Server) handleTitles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	letter := r.URL.Query().Get("letter")
	page := getPage(r)
	pagination := database.NewPagination(page, 50)

	if letter == "" {
		// Show alphabet
		feed := builder.NewFeedWithPath("Titles", "titles", "/titles", "/")
		letters := []string{"А", "Б", "В", "Г", "Д", "Е", "Ж", "З", "И", "К", "Л", "М", "Н", "О", "П", "Р", "С", "Т", "У", "Ф", "Х", "Ц", "Ч", "Ш", "Щ", "Э", "Ю", "Я",
			"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z"}
		for _, l := range letters {
			entry := builder.NavigationEntry(
				l,
				fmt.Sprintf("titles:letter:%s", l),
				fmt.Sprintf("%s/titles?letter=%s", s.getBaseURL(r), url.QueryEscape(l)),
				"",
			)
			feed.Entries = append(feed.Entries, entry)
		}
		s.writeOPDS(w, feed)
		return
	}

	books, err := s.svc.GetBooksForTitle(ctx, letter, pagination, false, 0)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get books")
		return
	}

	feed := builder.NewFeedWithPath(fmt.Sprintf("Titles: %s", letter), fmt.Sprintf("titles:letter:%s", letter), fmt.Sprintf("/titles?letter=%s", url.QueryEscape(letter)), "/titles")
	for _, book := range books {
		authors, _ := s.svc.GetBookAuthors(ctx, book.ID)
		genres, _ := s.svc.GetBookGenres(ctx, book.ID)
		series, _ := s.svc.GetBookSeries(ctx, book.ID)
		entry := builder.AcquisitionEntry(&book, authors, genres, series)
		feed.Entries = append(feed.Entries, entry)
	}

	// Add pagination links
	totalPages := int((pagination.TotalCount + int64(pagination.Limit) - 1) / int64(pagination.Limit))
	builder.Pagination(feed, fmt.Sprintf("/titles?letter=%s", url.QueryEscape(letter)), page, totalPages)

	s.writeOPDS(w, feed)
}

// handleGenres renders genre sections or genres in a section
func (s *Server) handleGenres(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	section := r.URL.Query().Get("section")

	if section == "" {
		// Show genre sections
		sections, err := s.svc.GetGenreSections(ctx)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get genres")
			return
		}

		feed := builder.NewFeedWithPath("Genres", "genres", "/genres", "/")
		for _, sec := range sections {
			entry := builder.NavigationEntry(
				sec,
				fmt.Sprintf("genre:section:%s", sec),
				fmt.Sprintf("%s/genres?section=%s", s.getBaseURL(r), url.QueryEscape(sec)),
				"",
			)
			feed.Entries = append(feed.Entries, entry)
		}
		s.writeOPDS(w, feed)
		return
	}

	// Show genres in section
	genres, err := s.svc.GetGenresInSection(ctx, section)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get genres")
		return
	}

	feed := builder.NewFeedWithPath(fmt.Sprintf("Genres: %s", section), fmt.Sprintf("genre:section:%s", section), fmt.Sprintf("/genres?section=%s", url.QueryEscape(section)), "/genres")
	for _, genre := range genres {
		entry := builder.NavigationEntry(
			genre.Subsection,
			fmt.Sprintf("genre:%d", genre.ID),
			fmt.Sprintf("%s/genres/%d", s.getBaseURL(r), genre.ID),
			"",
		)
		feed.Entries = append(feed.Entries, entry)
	}
	s.writeOPDS(w, feed)
}

// handleGenre renders books in a genre
func (s *Server) handleGenre(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	genreID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid genre ID")
		return
	}

	page := getPage(r)
	pagination := database.NewPagination(page, 50)

	books, err := s.svc.GetBooksForGenre(ctx, genreID, pagination, false)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get books")
		return
	}

	feed := builder.NewFeedWithPath("Genre Books", fmt.Sprintf("genre:%d", genreID), fmt.Sprintf("/genres/%d", genreID), "/genres")
	for _, book := range books {
		authors, _ := s.svc.GetBookAuthors(ctx, book.ID)
		genres, _ := s.svc.GetBookGenres(ctx, book.ID)
		series, _ := s.svc.GetBookSeries(ctx, book.ID)
		entry := builder.AcquisitionEntry(&book, authors, genres, series)
		feed.Entries = append(feed.Entries, entry)
	}

	// Add pagination links
	totalPages := int((pagination.TotalCount + int64(pagination.Limit) - 1) / int64(pagination.Limit))
	builder.Pagination(feed, fmt.Sprintf("/genres/%d", genreID), page, totalPages)

	s.writeOPDS(w, feed)
}

// handleSeriesList renders series listing
func (s *Server) handleSeriesList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	letter := r.URL.Query().Get("letter")
	page := getPage(r)
	pagination := database.NewPagination(page, 50)

	if letter == "" {
		// Show alphabet
		feed := builder.NewFeedWithPath("Series", "series", "/series", "/")
		letters := []string{"А", "Б", "В", "Г", "Д", "Е", "Ж", "З", "И", "К", "Л", "М", "Н", "О", "П", "Р", "С", "Т", "У", "Ф", "Х", "Ц", "Ч", "Ш", "Щ", "Э", "Ю", "Я",
			"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z"}
		for _, l := range letters {
			entry := builder.NavigationEntry(
				l,
				fmt.Sprintf("series:letter:%s", l),
				fmt.Sprintf("%s/series?letter=%s", s.getBaseURL(r), url.QueryEscape(l)),
				"",
			)
			feed.Entries = append(feed.Entries, entry)
		}
		s.writeOPDS(w, feed)
		return
	}

	seriesList, err := s.svc.GetSeriesByLetter(ctx, letter, pagination)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get series")
		return
	}

	feed := builder.NewFeedWithPath(fmt.Sprintf("Series: %s", letter), fmt.Sprintf("series:letter:%s", letter), fmt.Sprintf("/series?letter=%s", url.QueryEscape(letter)), "/series")
	for _, ser := range seriesList {
		entry := builder.NavigationEntry(
			ser.Name,
			fmt.Sprintf("series:%d", ser.ID),
			fmt.Sprintf("%s/series/%d", s.getBaseURL(r), ser.ID),
			"",
		)
		feed.Entries = append(feed.Entries, entry)
	}

	// Add pagination links
	totalPages := int((pagination.TotalCount + int64(pagination.Limit) - 1) / int64(pagination.Limit))
	builder.Pagination(feed, fmt.Sprintf("/series?letter=%s", url.QueryEscape(letter)), page, totalPages)

	s.writeOPDS(w, feed)
}

// handleSeries renders books in a series
func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	seriesID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	page := getPage(r)
	pagination := database.NewPagination(page, 50)

	books, err := s.svc.GetBooksForSeries(ctx, seriesID, pagination, false)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get books")
		return
	}

	feed := builder.NewFeedWithPath("Series Books", fmt.Sprintf("series:%d", seriesID), fmt.Sprintf("/series/%d", seriesID), "/series")
	for _, book := range books {
		authors, _ := s.svc.GetBookAuthors(ctx, book.ID)
		genres, _ := s.svc.GetBookGenres(ctx, book.ID)
		series, _ := s.svc.GetBookSeries(ctx, book.ID)
		entry := builder.AcquisitionEntry(&book, authors, genres, series)
		feed.Entries = append(feed.Entries, entry)
	}

	// Add pagination links
	totalPages := int((pagination.TotalCount + int64(pagination.Limit) - 1) / int64(pagination.Limit))
	builder.Pagination(feed, fmt.Sprintf("/series/%d", seriesID), page, totalPages)

	s.writeOPDS(w, feed)
}

// handleNew renders new books
func (s *Server) handleNew(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	books, err := s.svc.GetLastBooks(ctx, 50, 7)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get new books")
		return
	}

	feed := builder.NewFeedWithPath("New Books", "new", "/new", "/")
	for _, book := range books {
		authors, _ := s.svc.GetBookAuthors(ctx, book.ID)
		genres, _ := s.svc.GetBookGenres(ctx, book.ID)
		series, _ := s.svc.GetBookSeries(ctx, book.ID)
		entry := builder.AcquisitionEntry(&book, authors, genres, series)
		feed.Entries = append(feed.Entries, entry)
	}

	s.writeOPDS(w, feed)
}

// handleSearch handles search requests
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	query := r.URL.Query().Get("q")
	if query == "" {
		// Return OpenSearch description
		w.Header().Set("Content-Type", "application/opensearchdescription+xml")
		w.Write([]byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
  <ShortName>%s</ShortName>
  <Description>Search books</Description>
  <Url type="application/atom+xml;profile=opds-catalog" template="%s/search?q={searchTerms}"/>
</OpenSearchDescription>`, s.config.Site.Title, s.getBaseURL(r))))
		return
	}

	page := getPage(r)
	pagination := database.NewPagination(page, 50)

	// OPDS search includes annotation by default for compatibility
	opts := persistence.SearchOptions{
		TitleQuery:        query,
		IncludeAnnotation: true,
	}
	books, err := s.svc.SearchBooks(ctx, opts, pagination)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to search books")
		return
	}

	feed := builder.NewFeedWithPath(fmt.Sprintf("Search: %s", query), fmt.Sprintf("search:%s", query), fmt.Sprintf("/search?q=%s", url.QueryEscape(query)), "/")
	for _, book := range books {
		authors, _ := s.svc.GetBookAuthors(ctx, book.ID)
		genres, _ := s.svc.GetBookGenres(ctx, book.ID)
		series, _ := s.svc.GetBookSeries(ctx, book.ID)
		entry := builder.AcquisitionEntry(&book, authors, genres, series)
		feed.Entries = append(feed.Entries, entry)
	}

	// Add pagination links
	totalPages := int((pagination.TotalCount + int64(pagination.Limit) - 1) / int64(pagination.Limit))
	builder.Pagination(feed, fmt.Sprintf("/search?q=%s", url.QueryEscape(query)), page, totalPages)

	s.writeOPDS(w, feed)
}

// handleDownload serves book file download
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	bookID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	book, err := s.svc.GetBook(ctx, bookID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "Book not found")
		return
	}

	// Construct file path
	var filePath string
	if book.CatType == database.CatNormal {
		filePath = filepath.Join(s.config.Library.Root, book.Path, book.Filename)
	} else {
		// Book is in ZIP archive
		s.serveFromZip(w, r, book)
		return
	}

	// Set content headers
	w.Header().Set("Content-Type", opds.MimeType(book.Format))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, book.Filename))

	http.ServeFile(w, r, filePath)
}

func (s *Server) serveFromZip(w http.ResponseWriter, r *http.Request, book *database.Book) {
	// The path format is: zipfile.zip/internal/path/to/file
	// Split the path to find the ZIP file
	fullPath := filepath.Join(s.config.Library.Root, book.Path)
	parts := strings.Split(book.Path, string(filepath.Separator))

	if len(parts) == 0 {
		s.writeError(w, http.StatusNotFound, "Invalid book path")
		return
	}

	// Find where the .zip extension is in the path
	idx := -1
	for i, part := range parts {
		if strings.HasSuffix(strings.ToLower(part), ".zip") {
			idx = i
			break
		}
	}

	if idx < 0 {
		// Try treating the whole path as the ZIP file path
		if strings.HasSuffix(strings.ToLower(book.Path), ".zip") {
			idx = len(parts) - 1
		} else {
			s.writeError(w, http.StatusNotFound, "Book not in ZIP")
			return
		}
	}

	// Reconstruct paths
	zipFilePath := filepath.Join(s.config.Library.Root, filepath.Join(parts[:idx+1]...))
	var internalPath string
	if idx+1 < len(parts) {
		internalPath = filepath.Join(append(parts[idx+1:], book.Filename)...)
	} else {
		internalPath = book.Filename
	}

	// Use fullPath for debugging if needed
	_ = fullPath

	zr, err := zip.OpenReader(zipFilePath)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "ZIP not found")
		return
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == internalPath || f.Name == book.Filename {
			rc, err := f.Open()
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, "Failed to open file in ZIP")
				return
			}
			defer rc.Close()

			w.Header().Set("Content-Type", opds.MimeType(book.Format))
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, book.Filename))
			io.Copy(w, rc)
			return
		}
	}

	s.writeError(w, http.StatusNotFound, "File not found in ZIP")
}

// handleCover serves book cover image
func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	bookID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	book, err := s.svc.GetBook(ctx, bookID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "Book not found")
		return
	}

	format := strings.ToLower(book.Format)

	// Check if it's an audiobook (including archives containing audio)
	if book.IsAudiobook || isAudioFormat(format) {
		s.serveAudioCover(w, r, book)
		return
	}

	// Only FB2 files have embedded covers
	if format != "fb2" {
		s.writeError(w, http.StatusNotFound, "Cover not available for this format")
		return
	}

	// Read the FB2 file
	var fb2Data []byte
	if book.CatType == database.CatNormal {
		filePath := filepath.Join(s.config.Library.Root, book.Path, book.Filename)
		fb2Data, err = os.ReadFile(filePath)
		if err != nil {
			s.writeError(w, http.StatusNotFound, "Book file not found")
			return
		}
	} else {
		fb2Data, err = s.readFromZip(book)
		if err != nil {
			s.writeError(w, http.StatusNotFound, "Failed to read book from ZIP")
			return
		}
	}

	// Extract cover from FB2
	coverData, coverType, err := extractFB2Cover(fb2Data)
	if err != nil || len(coverData) == 0 {
		s.writeError(w, http.StatusNotFound, "Cover not found in book")
		return
	}

	// Serve the cover image
	w.Header().Set("Content-Type", coverType)
	w.Header().Set("Content-Length", strconv.Itoa(len(coverData)))
	w.Header().Set("Cache-Control", "public, max-age=86400") // Cache for 24 hours
	w.Write(coverData)
}

// extractFB2Cover extracts cover image from FB2 data
func extractFB2Cover(data []byte) ([]byte, string, error) {
	// Handle BOM
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})

	// Simple struct for cover extraction
	type fb2CoverDoc struct {
		Description struct {
			TitleInfo struct {
				Coverpage struct {
					Images []struct {
						Href      string `xml:"href,attr"`
						XlinkHref string `xml:"http://www.w3.org/1999/xlink href,attr"`
					} `xml:"image"`
				} `xml:"coverpage"`
			} `xml:"title-info"`
		} `xml:"description"`
		Binaries []struct {
			ID          string `xml:"id,attr"`
			ContentType string `xml:"content-type,attr"`
			Data        string `xml:",chardata"`
		} `xml:"binary"`
	}

	var doc fb2CoverDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, "", err
	}

	// Find cover ID
	var coverID string
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

	if coverID == "" {
		return nil, "", fmt.Errorf("no cover found")
	}

	// Find binary with cover data
	for _, bin := range doc.Binaries {
		if strings.EqualFold(bin.ID, coverID) {
			// Decode base64
			cleanData := strings.ReplaceAll(bin.Data, "\n", "")
			cleanData = strings.ReplaceAll(cleanData, "\r", "")
			cleanData = strings.ReplaceAll(cleanData, " ", "")

			coverData, err := base64.StdEncoding.DecodeString(cleanData)
			if err != nil {
				return nil, "", err
			}

			contentType := bin.ContentType
			if contentType == "" {
				contentType = "image/jpeg"
			}

			return coverData, contentType, nil
		}
	}

	return nil, "", fmt.Errorf("cover binary not found")
}

// isAudioFormat checks if the format is an audio format
func isAudioFormat(format string) bool {
	switch strings.ToLower(format) {
	case "mp3", "m4b", "m4a", "flac", "ogg", "opus", "aac":
		return true
	}
	return false
}

// serveAudioCover extracts and serves cover from audio file or archive
func (s *Server) serveAudioCover(w http.ResponseWriter, r *http.Request, book *database.Book) {
	var audioPath string
	ext := strings.ToLower(filepath.Ext(book.Filename))

	// Handle folder-based audiobooks differently
	if strings.ToLower(book.Format) == "folder" {
		// For folder audiobooks, get the first track path from chapters JSON
		folderPath := filepath.Join(s.config.Library.Root, book.Path)

		// Try to get first track for @eaDir cover lookup
		if book.Chapters != "" {
			var structure struct {
				Tracks []struct {
					Path string `json:"path"`
				} `json:"tracks"`
			}
			if err := json.Unmarshal([]byte(book.Chapters), &structure); err == nil && len(structure.Tracks) > 0 {
				// Use first track path for @eaDir lookup
				audioPath = structure.Tracks[0].Path
			}
		}

		// Fallback: construct path to first file in folder
		if audioPath == "" {
			audioPath = folderPath
		}

		// First, try @eaDir for the first track
		if coverData, coverType := s.getEaDirCover(audioPath); len(coverData) > 0 {
			w.Header().Set("Content-Type", coverType)
			w.Header().Set("Content-Length", strconv.Itoa(len(coverData)))
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Write(coverData)
			return
		}

		// Try folder cover in the audiobook folder
		if coverData, coverType := s.getFolderCoverInDir(folderPath); len(coverData) > 0 {
			w.Header().Set("Content-Type", coverType)
			w.Header().Set("Content-Length", strconv.Itoa(len(coverData)))
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Write(coverData)
			return
		}

		// Try extracting from first audio file
		if audioPath != "" && audioPath != folderPath {
			if f, err := os.Open(audioPath); err == nil {
				defer f.Close()
				if m, err := tag.ReadFrom(f); err == nil {
					if pic := m.Picture(); pic != nil && len(pic.Data) > 0 {
						contentType := pic.MIMEType
						if contentType == "" {
							contentType = "image/jpeg"
						}
						w.Header().Set("Content-Type", contentType)
						w.Header().Set("Content-Length", strconv.Itoa(len(pic.Data)))
						w.Header().Set("Cache-Control", "public, max-age=86400")
						w.Write(pic.Data)
						return
					}
				}
			}
		}

		s.writeError(w, http.StatusNotFound, "Cover not found in audiobook folder")
		return
	}

	// Regular audio file or archive
	audioPath = filepath.Join(s.config.Library.Root, book.Path, book.Filename)

	// First, try @eaDir (Synology NAS pre-generated thumbnails)
	if coverData, coverType := s.getEaDirCover(audioPath); len(coverData) > 0 {
		w.Header().Set("Content-Type", coverType)
		w.Header().Set("Content-Length", strconv.Itoa(len(coverData)))
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(coverData)
		return
	}

	// Try folder cover (cover.jpg, folder.jpg in same directory)
	if coverData, coverType := s.getFolderCover(audioPath); len(coverData) > 0 {
		w.Header().Set("Content-Type", coverType)
		w.Header().Set("Content-Length", strconv.Itoa(len(coverData)))
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(coverData)
		return
	}

	// Check if file is an archive (7z or zip containing audio)
	if ext == ".7z" || ext == ".zip" || book.CatType != database.CatNormal {
		coverData, coverType, err := s.extractAudioCoverFromArchive(book)
		if err != nil || len(coverData) == 0 {
			s.writeError(w, http.StatusNotFound, "Cover not found in audiobook")
			return
		}
		w.Header().Set("Content-Type", coverType)
		w.Header().Set("Content-Length", strconv.Itoa(len(coverData)))
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(coverData)
		return
	}

	// Open the audio file and extract cover directly
	f, err := os.Open(audioPath)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "Audio file not found")
		return
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "Failed to read audio tags")
		return
	}

	pic := m.Picture()
	if pic == nil || len(pic.Data) == 0 {
		s.writeError(w, http.StatusNotFound, "Cover not found in audio file")
		return
	}

	contentType := pic.MIMEType
	if contentType == "" {
		contentType = "image/jpeg"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(pic.Data)))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(pic.Data)
}

// getEaDirCover looks for Synology @eaDir pre-generated thumbnails
func (s *Server) getEaDirCover(audioPath string) ([]byte, string) {
	dir := filepath.Dir(audioPath)
	filename := filepath.Base(audioPath)
	eaDir := filepath.Join(dir, "@eaDir")

	// Check if @eaDir exists
	if _, err := os.Stat(eaDir); os.IsNotExist(err) {
		return nil, ""
	}

	// Try various Synology thumbnail patterns
	patterns := []string{
		// Synology Audio Station album art (SYNOAUDIO_01APIC_XX.jpg)
		filepath.Join(eaDir, filename, "SYNOAUDIO_01APIC_03.jpg"),
		filepath.Join(eaDir, filename, "SYNOAUDIO_01APIC_00.jpg"),
		filepath.Join(eaDir, filename, "SYNOAUDIO_01APIC_01.jpg"),
		filepath.Join(eaDir, filename, "SYNOAUDIO_01APIC_02.jpg"),
		// Synology Photo Station thumbnails
		filepath.Join(eaDir, filename, "SYNOPHOTO_THUMB_XL.jpg"),
		filepath.Join(eaDir, filename, "SYNOPHOTO_THUMB_L.jpg"),
		filepath.Join(eaDir, filename, "SYNOPHOTO_THUMB_M.jpg"),
		filepath.Join(eaDir, filename, "SYNOPHOTO_THUMB_SM.jpg"),
		// Alternative patterns
		filepath.Join(eaDir, filename+".jpg"),
		filepath.Join(eaDir, filename, "cover.jpg"),
		filepath.Join(eaDir, filename, "folder.jpg"),
	}

	for _, pattern := range patterns {
		if data, err := os.ReadFile(pattern); err == nil && len(data) > 0 {
			return data, "image/jpeg"
		}
	}

	// Also check for folder-level cover in @eaDir
	folderPatterns := []string{
		filepath.Join(eaDir, "folder.jpg"),
		filepath.Join(eaDir, "cover.jpg"),
		filepath.Join(eaDir, "SYNOPHOTO_THUMB_XL.jpg"),
	}

	for _, pattern := range folderPatterns {
		if data, err := os.ReadFile(pattern); err == nil && len(data) > 0 {
			return data, "image/jpeg"
		}
	}

	return nil, ""
}

// getFolderCover looks for cover.jpg or folder.jpg in the audio file's directory
func (s *Server) getFolderCover(audioPath string) ([]byte, string) {
	dir := filepath.Dir(audioPath)

	patterns := []string{
		filepath.Join(dir, "cover.jpg"),
		filepath.Join(dir, "Cover.jpg"),
		filepath.Join(dir, "folder.jpg"),
		filepath.Join(dir, "Folder.jpg"),
		filepath.Join(dir, "cover.png"),
		filepath.Join(dir, "Cover.png"),
		filepath.Join(dir, "folder.png"),
		filepath.Join(dir, "Folder.png"),
	}

	for _, pattern := range patterns {
		if data, err := os.ReadFile(pattern); err == nil && len(data) > 0 {
			mimeType := "image/jpeg"
			if strings.HasSuffix(strings.ToLower(pattern), ".png") {
				mimeType = "image/png"
			}
			return data, mimeType
		}
	}

	return nil, ""
}

// getFolderCoverInDir looks for cover.jpg or folder.jpg in the specified directory
func (s *Server) getFolderCoverInDir(dir string) ([]byte, string) {
	patterns := []string{
		filepath.Join(dir, "cover.jpg"),
		filepath.Join(dir, "Cover.jpg"),
		filepath.Join(dir, "folder.jpg"),
		filepath.Join(dir, "Folder.jpg"),
		filepath.Join(dir, "cover.png"),
		filepath.Join(dir, "Cover.png"),
		filepath.Join(dir, "folder.png"),
		filepath.Join(dir, "Folder.png"),
	}

	for _, pattern := range patterns {
		if data, err := os.ReadFile(pattern); err == nil && len(data) > 0 {
			mimeType := "image/jpeg"
			if strings.HasSuffix(strings.ToLower(pattern), ".png") {
				mimeType = "image/png"
			}
			return data, mimeType
		}
	}

	return nil, ""
}

// extractAudioCoverFromArchive extracts cover from first audio file in archive
func (s *Server) extractAudioCoverFromArchive(book *database.Book) ([]byte, string, error) {
	// For standalone archives, the filename is the archive itself
	// For books inside archives, the path contains the archive
	var archivePath string
	ext := strings.ToLower(filepath.Ext(book.Filename))
	if ext == ".7z" || ext == ".zip" {
		// Standalone archive file
		archivePath = filepath.Join(s.config.Library.Root, book.Path, book.Filename)
	} else {
		// Book inside an archive - path contains the archive
		archivePath = filepath.Join(s.config.Library.Root, book.Path)
	}

	// Try ZIP first
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return s.extractAudioCoverFromZip(archivePath)
	}

	// Try 7z
	if strings.HasSuffix(strings.ToLower(archivePath), ".7z") {
		return s.extractAudioCoverFrom7z(archivePath)
	}

	return nil, "", fmt.Errorf("unsupported archive format")
}

// extractAudioCoverFromZip extracts cover from first audio file in ZIP
func (s *Server) extractAudioCoverFromZip(zipPath string) ([]byte, string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, "", err
	}
	defer zr.Close()

	// Find first audio file
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if !isAudioFormat(strings.TrimPrefix(ext, ".")) {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}

		// Buffer the data for ReadSeeker
		var buf bytes.Buffer
		_, err = io.Copy(&buf, rc)
		rc.Close()
		if err != nil {
			continue
		}

		m, err := tag.ReadFrom(bytes.NewReader(buf.Bytes()))
		if err != nil {
			continue
		}

		pic := m.Picture()
		if pic != nil && len(pic.Data) > 0 {
			contentType := pic.MIMEType
			if contentType == "" {
				contentType = "image/jpeg"
			}
			return pic.Data, contentType, nil
		}
	}

	return nil, "", fmt.Errorf("no cover found in archive")
}

// extractAudioCoverFrom7z extracts cover from first audio file in 7z
func (s *Server) extractAudioCoverFrom7z(szPath string) ([]byte, string, error) {
	sz, err := go7z.OpenReader(szPath)
	if err != nil {
		return nil, "", err
	}
	defer sz.Close()

	for {
		hdr, err := sz.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
		if hdr.IsEmptyStream {
			continue
		}

		ext := strings.ToLower(filepath.Ext(hdr.Name))
		if !isAudioFormat(strings.TrimPrefix(ext, ".")) {
			continue
		}

		// Read file to temp buffer
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, sz); err != nil {
			continue
		}

		m, err := tag.ReadFrom(bytes.NewReader(buf.Bytes()))
		if err != nil {
			continue
		}

		pic := m.Picture()
		if pic != nil && len(pic.Data) > 0 {
			contentType := pic.MIMEType
			if contentType == "" {
				contentType = "image/jpeg"
			}
			return pic.Data, contentType, nil
		}
	}

	return nil, "", fmt.Errorf("no cover found in 7z archive")
}

// handleConvertEPUB converts book to EPUB
func (s *Server) handleConvertEPUB(w http.ResponseWriter, r *http.Request) {
	s.handleConvert(w, r, "epub")
}

// handleConvertMOBI converts book to MOBI
func (s *Server) handleConvertMOBI(w http.ResponseWriter, r *http.Request) {
	s.handleConvert(w, r, "mobi")
}

// handleConvert handles book format conversion
func (s *Server) handleConvert(w http.ResponseWriter, r *http.Request, format string) {
	ctx := r.Context()

	bookID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	book, err := s.svc.GetBook(ctx, bookID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "Book not found")
		return
	}

	// Only FB2 files can be converted
	if strings.ToLower(book.Format) != "fb2" {
		s.writeError(w, http.StatusBadRequest, "Only FB2 files can be converted")
		return
	}

	// Read the FB2 file content
	var fb2Data []byte
	if book.CatType == database.CatNormal {
		filePath := filepath.Join(s.config.Library.Root, book.Path, book.Filename)
		fb2Data, err = os.ReadFile(filePath)
		if err != nil {
			s.writeError(w, http.StatusNotFound, "Book file not found")
			return
		}
	} else {
		// Book is in ZIP archive
		fb2Data, err = s.readFromZip(book)
		if err != nil {
			s.writeError(w, http.StatusNotFound, "Failed to read book from ZIP")
			return
		}
	}

	// Create temp file for output
	tempDir := s.config.Converters.TempDir
	if tempDir == "" {
		tempDir = os.TempDir()
	}

	outputFile, err := os.CreateTemp(tempDir, fmt.Sprintf("book-*.%s", format))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to create temp file")
		return
	}
	outputPath := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputPath)

	// Perform conversion
	switch format {
	case "epub":
		err = s.converter.FB2DataToEPUB(fb2Data, outputPath)
	case "mobi":
		err = s.converter.FB2DataToMOBI(fb2Data, outputPath)
	default:
		s.writeError(w, http.StatusBadRequest, "Unsupported format")
		return
	}

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Conversion failed: %v", err))
		return
	}

	// Serve the converted file
	outputFilename := strings.TrimSuffix(book.Filename, filepath.Ext(book.Filename)) + "." + format

	w.Header().Set("Content-Type", opds.MimeType(format))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, outputFilename))

	http.ServeFile(w, r, outputPath)
}

// readFromZip reads a book file from a ZIP archive
func (s *Server) readFromZip(book *database.Book) ([]byte, error) {
	parts := strings.Split(book.Path, string(filepath.Separator))

	// Find where the .zip extension is in the path
	idx := -1
	for i, part := range parts {
		if strings.HasSuffix(strings.ToLower(part), ".zip") {
			idx = i
			break
		}
	}

	if idx < 0 {
		if strings.HasSuffix(strings.ToLower(book.Path), ".zip") {
			idx = len(parts) - 1
		} else {
			return nil, fmt.Errorf("book not in ZIP")
		}
	}

	zipFilePath := filepath.Join(s.config.Library.Root, filepath.Join(parts[:idx+1]...))
	var internalPath string
	if idx+1 < len(parts) {
		internalPath = filepath.Join(append(parts[idx+1:], book.Filename)...)
	} else {
		internalPath = book.Filename
	}

	zr, err := zip.OpenReader(zipFilePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == internalPath || f.Name == book.Filename {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}

	return nil, fmt.Errorf("file not found in ZIP")
}

// Bookshelf handlers

// handleBookshelf shows user's bookshelf (OPDS)
func (s *Server) handleBookshelf(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	builder := s.getFeedBuilder(r)

	user := s.getUser(r)
	if user == "" {
		user = "anonymous"
	}

	page := getPage(r)
	pagination := database.NewPagination(page, 50)

	books, err := s.svc.GetBookShelf(ctx, user, pagination)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get bookshelf")
		return
	}

	feed := builder.NewFeedWithPath("My Bookshelf", "bookshelf", "/bookshelf", "/")

	for _, book := range books {
		authors, _ := s.svc.GetBookAuthors(ctx, book.ID)
		genres, _ := s.svc.GetBookGenres(ctx, book.ID)
		series, _ := s.svc.GetBookSeries(ctx, book.ID)
		entry := builder.AcquisitionEntry(&book, authors, genres, series)
		feed.Entries = append(feed.Entries, entry)
	}

	// Add pagination
	count, _ := s.svc.CountBookShelf(ctx, user)
	if count > int64(pagination.Offset()+len(books)) {
		feed.Links = append(feed.Links, opds.Link{
			Rel:  "next",
			Href: fmt.Sprintf("%s/bookshelf?page=%d", s.getBaseURL(r), page+1),
			Type: "application/atom+xml;profile=opds-catalog",
		})
	}

	s.writeOPDS(w, feed)
}

// handleBookshelfAdd adds a book to user's bookshelf
func (s *Server) handleBookshelfAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	bookID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	user := s.getUser(r)
	if user == "" {
		user = "anonymous"
	}

	if err := s.svc.AddBookShelf(ctx, user, bookID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to add to bookshelf")
		return
	}

	// Redirect back or return success
	referer := r.Header.Get("Referer")
	if referer != "" {
		http.Redirect(w, r, referer, http.StatusSeeOther)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Added to bookshelf"))
}

// handleBookshelfRemove removes a book from user's bookshelf
func (s *Server) handleBookshelfRemove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	bookID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	user := s.getUser(r)
	if user == "" {
		user = "anonymous"
	}

	if err := s.svc.RemoveBookShelf(ctx, user, bookID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to remove from bookshelf")
		return
	}

	// Redirect back or return success
	referer := r.Header.Get("Referer")
	if referer != "" {
		http.Redirect(w, r, referer, http.StatusSeeOther)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Removed from bookshelf"))
}

// getUser returns the authenticated username or empty string
func (s *Server) getUser(r *http.Request) string {
	if user := r.Context().Value("username"); user != nil {
		return user.(string)
	}
	return ""
}

// Helper functions

func getPage(r *http.Request) int {
	pageStr := r.URL.Query().Get("page")
	if pageStr == "" {
		return 0
	}
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 0 {
		return 0
	}
	return page
}

// MimeType helper exposed for handlers
func MimeType(format string) string {
	return opds.MimeType(format)
}

func init() {
	// Make opds.MimeType accessible
}
