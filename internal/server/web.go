package server

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/sopds/sopds-go/internal/database"
)

// Cached ebook-convert availability check
var (
	ebookConvertAvailable     bool
	ebookConvertChecked       bool
	ebookConvertCheckMu       sync.Once
)

// checkEbookConvert checks if ebook-convert is available
func checkEbookConvert(configPath string) bool {
	ebookConvertCheckMu.Do(func() {
		path := configPath
		if path == "" {
			path = "ebook-convert"
		}
		_, err := exec.LookPath(path)
		ebookConvertAvailable = err == nil
		ebookConvertChecked = true
	})
	return ebookConvertAvailable
}

// Template data structures
type PageData struct {
	Title       string
	SiteTitle   string
	WebPrefix   string
	OPDSPrefix  string
	Query       string
	Prefix      string // 1, 2, or 3 char prefix for drilling down
	Page        int
	PageSize    int
	HasMore     bool
	PrevPage    int
	NextPage    int
	CurrentPath string
	HasEPUB     bool
	HasMOBI     bool
}

// Available page sizes
var pageSizes = []int{10, 50, 100, 200, 0} // 0 means "all"
const defaultPageSize = 50

func getPageSize(r *http.Request) int {
	sizeStr := r.URL.Query().Get("size")
	if sizeStr == "" {
		return defaultPageSize
	}
	if sizeStr == "all" {
		return 0
	}
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size < 0 {
		return defaultPageSize
	}
	// Validate against allowed sizes
	for _, s := range pageSizes {
		if s == size {
			return size
		}
	}
	return defaultPageSize
}

type MainMenuData struct {
	PageData
	Stats    *database.DBInfo
	NewBooks int64
}

type BooksData struct {
	PageData
	Books      []BookView
	TotalCount int
	// Filter options (collected from results)
	Languages  []string
	FirstNames []string
	LastNames  []string
	Genres     []LinkedItem
	// Current filter values
	FilterLang      string
	FilterFirstName string
	FilterLastName  string
	FilterGenre     int64
	FilterGenreName string
}

type BookView struct {
	ID             int64
	Title          string
	Authors        []LinkedItem
	Genres         []LinkedItem
	Series         []LinkedItem
	Lang           string
	LangName       string
	Format         string
	Size           string
	Annotation     string
	HasCover       bool
	CanEPUB        bool
	CanMOBI        bool
	DuplicateOf    int64 // ID of original book if this is a duplicate
	DuplicateCount int   // Number of duplicates this book has
}

type LinkedItem struct {
	ID   int64
	Name string
}

type AuthorsData struct {
	PageData
	Prefixes []string // 2-char or 3-char prefixes
	Authors  []AuthorView
	IsIndex  bool
}

type AuthorView struct {
	ID   int64
	Name string
}

type GenresData struct {
	PageData
	Sections []string
	Genres   []GenreView
}

type GenreView struct {
	ID   int64
	Name string
}

type SeriesData struct {
	PageData
	Prefixes []string
	Series   []SeriesView
	IsIndex  bool
}

type SeriesView struct {
	ID   int64
	Name string
}

type LanguagesData struct {
	PageData
	Languages []LanguageView
}

type LanguageView struct {
	Code  string
	Name  string
	Count int64
}

type CatalogsData struct {
	PageData
	Items []CatalogItem
}

type CatalogItem struct {
	ID       int64
	Name     string
	IsFolder bool
	Book     *BookView
}

// Web handlers

func (s *Server) handleWebHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	info, _ := s.db.GetDBInfo(ctx, false)
	var newBooks int64
	if newInfo, err := s.db.GetNewInfo(ctx, 7); err == nil && newInfo != nil {
		newBooks = newInfo.NewBooks
	}

	data := MainMenuData{
		PageData: PageData{
			Title:      "Library",
			SiteTitle:  s.config.Site.Title,
			WebPrefix:  s.config.Server.WebPrefix,
			OPDSPrefix: s.config.Server.OPDSPrefix,
			HasEPUB:    true, // Internal converter always available
			HasMOBI:    checkEbookConvert(s.config.Converters.FB2ToMOBI),
		},
		Stats:    info,
		NewBooks: newBooks,
	}

	s.renderTemplate(w, "main", data)
}

func (s *Server) handleWebSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query().Get("q")
	page := getPage(r)
	pageSize := getPageSize(r)

	// Parse filter parameters
	filterLang := r.URL.Query().Get("lang")
	filterFirstName := r.URL.Query().Get("fname")
	filterLastName := r.URL.Query().Get("lname")
	filterGenreStr := r.URL.Query().Get("genre")
	var filterGenre int64
	if filterGenreStr != "" {
		filterGenre, _ = strconv.ParseInt(filterGenreStr, 10, 64)
	}

	if query == "" {
		data := PageData{
			Title:      "Search",
			SiteTitle:  s.config.Site.Title,
			WebPrefix:  s.config.Server.WebPrefix,
			OPDSPrefix: s.config.Server.OPDSPrefix,
		}
		s.renderTemplate(w, "search", data)
		return
	}

	// Build filters
	var filters *database.SearchFilters
	if filterLang != "" || filterFirstName != "" || filterLastName != "" || filterGenre > 0 {
		filters = &database.SearchFilters{
			Lang:      filterLang,
			FirstName: filterFirstName,
			LastName:  filterLastName,
			GenreID:   filterGenre,
		}
	}

	// For "all", use large limit; otherwise use pageSize
	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)
	books, err := s.db.SearchBooks(ctx, query, pagination, false, filters)
	if err != nil {
		s.renderError(w, "Search failed", err)
		return
	}

	// Convert books to view and collect filter options from results
	bookViews, filterOpts := s.booksToViewWithFilters(ctx, books)

	// Look up genre name if filter is active
	var filterGenreName string
	if filterGenre > 0 {
		for _, g := range filterOpts.Genres {
			if g.ID == filterGenre {
				filterGenreName = g.Name
				break
			}
		}
		if filterGenreName == "" {
			// Genre not in current results, look it up
			if genre, err := s.db.GetGenre(ctx, filterGenre); err == nil {
				filterGenreName = genre.Subsection
				if filterGenreName == "" {
					filterGenreName = genre.Genre
				}
			}
		}
	}

	hasMore := pageSize > 0 && len(books) >= pageSize

	data := BooksData{
		PageData: PageData{
			Title:       fmt.Sprintf("Search: %s", query),
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			Query:       query,
			Page:        page,
			PageSize:    pageSize,
			HasMore:     hasMore,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			CurrentPath: s.config.Server.WebPrefix + "/search",
			HasEPUB:     true, // Internal converter always available
			HasMOBI:     checkEbookConvert(s.config.Converters.FB2ToMOBI),
		},
		Books:           bookViews,
		TotalCount:      int(pagination.TotalCount),
		Languages:       filterOpts.Languages,
		FirstNames:      filterOpts.FirstNames,
		LastNames:       filterOpts.LastNames,
		Genres:          filterOpts.Genres,
		FilterLang:      filterLang,
		FilterFirstName: filterFirstName,
		FilterLastName:  filterLastName,
		FilterGenre:     filterGenre,
		FilterGenreName: filterGenreName,
	}

	s.renderTemplate(w, "books", data)
}

func (s *Server) handleWebAuthors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	prefix := r.URL.Query().Get("prefix")
	page := getPage(r)

	// First level: show 1-char prefixes
	if prefix == "" {
		prefixes := s.getAuthorPrefixes(ctx, "", 1)
		data := AuthorsData{
			PageData: PageData{
				Title:      "Authors",
				SiteTitle:  s.config.Site.Title,
				WebPrefix:  s.config.Server.WebPrefix,
				OPDSPrefix: s.config.Server.OPDSPrefix,
			},
			Prefixes: prefixes,
			IsIndex:  true,
		}
		s.renderTemplate(w, "authors_index", data)
		return
	}

	// Check how many authors match this prefix
	count := s.countAuthorsByPrefix(ctx, prefix)

	// If more than 100 and prefix < 3 chars, drill down
	if count > 100 && len(prefix) < 3 {
		prefixes := s.getAuthorPrefixes(ctx, prefix, len(prefix)+1)
		data := AuthorsData{
			PageData: PageData{
				Title:      fmt.Sprintf("Authors: %s", prefix),
				SiteTitle:  s.config.Site.Title,
				WebPrefix:  s.config.Server.WebPrefix,
				OPDSPrefix: s.config.Server.OPDSPrefix,
				Prefix:     prefix,
			},
			Prefixes: prefixes,
			IsIndex:  true,
		}
		s.renderTemplate(w, "authors_index", data)
		return
	}

	// Show actual authors list
	pagination := database.NewPagination(page, 100)
	authors, err := s.db.GetAuthorsByPrefix(ctx, prefix, pagination)
	if err != nil {
		s.renderError(w, "Failed to get authors", err)
		return
	}

	var authorViews []AuthorView
	for _, a := range authors {
		authorViews = append(authorViews, AuthorView{ID: a.ID, Name: a.FullName()})
	}

	data := AuthorsData{
		PageData: PageData{
			Title:       fmt.Sprintf("Authors: %s", prefix),
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			Prefix:      prefix,
			Page:        page,
			HasMore:     len(authors) >= 100,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			CurrentPath: s.config.Server.WebPrefix + "/authors",
		},
		Authors: authorViews,
		IsIndex: false,
	}

	s.renderTemplate(w, "authors", data)
}

func (s *Server) handleWebAuthor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	authorID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.renderError(w, "Invalid author ID", err)
		return
	}

	page := getPage(r)
	pageSize := getPageSize(r)
	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.db.GetBooksForAuthor(ctx, authorID, pagination, false)
	if err != nil {
		s.renderError(w, "Failed to get books", err)
		return
	}

	authorName := "Author"
	if len(books) > 0 {
		if authors, err := s.db.GetBookAuthors(ctx, books[0].ID); err == nil {
			for _, a := range authors {
				if a.ID == authorID {
					authorName = a.FullName()
					break
				}
			}
		}
	}

	hasMore := pageSize > 0 && len(books) >= pageSize

	data := BooksData{
		PageData: PageData{
			Title:       authorName,
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			Page:        page,
			PageSize:    pageSize,
			HasMore:     hasMore,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			CurrentPath: fmt.Sprintf("%s/authors/%d", s.config.Server.WebPrefix, authorID),
			HasEPUB:     true, // Internal converter always available
			HasMOBI:     checkEbookConvert(s.config.Converters.FB2ToMOBI),
		},
		Books: s.booksToView(ctx, books),
	}

	s.renderTemplate(w, "books", data)
}

func (s *Server) handleWebGenres(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	section := r.URL.Query().Get("section")

	if section == "" {
		sections, err := s.db.GetGenreSections(ctx)
		if err != nil {
			s.renderError(w, "Failed to get genres", err)
			return
		}

		data := GenresData{
			PageData: PageData{
				Title:      "Genres",
				SiteTitle:  s.config.Site.Title,
				WebPrefix:  s.config.Server.WebPrefix,
				OPDSPrefix: s.config.Server.OPDSPrefix,
			},
			Sections: sections,
		}
		s.renderTemplate(w, "genres_index", data)
		return
	}

	genres, err := s.db.GetGenresInSection(ctx, section)
	if err != nil {
		s.renderError(w, "Failed to get genres", err)
		return
	}

	var genreViews []GenreView
	for _, g := range genres {
		name := g.Subsection
		if name == "" {
			name = g.Genre
		}
		genreViews = append(genreViews, GenreView{ID: g.ID, Name: name})
	}

	data := GenresData{
		PageData: PageData{
			Title:      section,
			SiteTitle:  s.config.Site.Title,
			WebPrefix:  s.config.Server.WebPrefix,
			OPDSPrefix: s.config.Server.OPDSPrefix,
		},
		Genres: genreViews,
	}

	s.renderTemplate(w, "genres", data)
}

func (s *Server) handleWebGenre(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	genreID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.renderError(w, "Invalid genre ID", err)
		return
	}

	page := getPage(r)
	pageSize := getPageSize(r)
	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.db.GetBooksForGenre(ctx, genreID, pagination, false)
	if err != nil {
		s.renderError(w, "Failed to get books", err)
		return
	}

	hasMore := pageSize > 0 && len(books) >= pageSize

	data := BooksData{
		PageData: PageData{
			Title:       "Genre Books",
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			Page:        page,
			PageSize:    pageSize,
			HasMore:     hasMore,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			CurrentPath: fmt.Sprintf("%s/genres/%d", s.config.Server.WebPrefix, genreID),
			HasEPUB:     true, // Internal converter always available
			HasMOBI:     checkEbookConvert(s.config.Converters.FB2ToMOBI),
		},
		Books: s.booksToView(ctx, books),
	}

	s.renderTemplate(w, "books", data)
}

func (s *Server) handleWebSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	prefix := r.URL.Query().Get("prefix")
	page := getPage(r)

	if prefix == "" {
		prefixes := s.getSeriesPrefixes(ctx, "", 1)
		data := SeriesData{
			PageData: PageData{
				Title:      "Series",
				SiteTitle:  s.config.Site.Title,
				WebPrefix:  s.config.Server.WebPrefix,
				OPDSPrefix: s.config.Server.OPDSPrefix,
			},
			Prefixes: prefixes,
			IsIndex:  true,
		}
		s.renderTemplate(w, "series_index", data)
		return
	}

	count := s.countSeriesByPrefix(ctx, prefix)

	if count > 100 && len(prefix) < 3 {
		prefixes := s.getSeriesPrefixes(ctx, prefix, len(prefix)+1)
		data := SeriesData{
			PageData: PageData{
				Title:      fmt.Sprintf("Series: %s", prefix),
				SiteTitle:  s.config.Site.Title,
				WebPrefix:  s.config.Server.WebPrefix,
				OPDSPrefix: s.config.Server.OPDSPrefix,
				Prefix:     prefix,
			},
			Prefixes: prefixes,
			IsIndex:  true,
		}
		s.renderTemplate(w, "series_index", data)
		return
	}

	pagination := database.NewPagination(page, 100)
	seriesList, err := s.db.GetSeriesByPrefix(ctx, prefix, pagination)
	if err != nil {
		s.renderError(w, "Failed to get series", err)
		return
	}

	var seriesViews []SeriesView
	for _, ser := range seriesList {
		seriesViews = append(seriesViews, SeriesView{ID: ser.ID, Name: ser.Name})
	}

	data := SeriesData{
		PageData: PageData{
			Title:       fmt.Sprintf("Series: %s", prefix),
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			Prefix:      prefix,
			Page:        page,
			HasMore:     len(seriesList) >= 100,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			CurrentPath: s.config.Server.WebPrefix + "/series",
		},
		Series:  seriesViews,
		IsIndex: false,
	}

	s.renderTemplate(w, "series", data)
}

func (s *Server) handleWebSeriesBooks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	seriesID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.renderError(w, "Invalid series ID", err)
		return
	}

	page := getPage(r)
	pageSize := getPageSize(r)
	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.db.GetBooksForSeries(ctx, seriesID, pagination, false)
	if err != nil {
		s.renderError(w, "Failed to get books", err)
		return
	}

	hasMore := pageSize > 0 && len(books) >= pageSize

	data := BooksData{
		PageData: PageData{
			Title:       "Series Books",
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			Page:        page,
			PageSize:    pageSize,
			HasMore:     hasMore,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			CurrentPath: fmt.Sprintf("%s/series/%d", s.config.Server.WebPrefix, seriesID),
			HasEPUB:     true, // Internal converter always available
			HasMOBI:     checkEbookConvert(s.config.Converters.FB2ToMOBI),
		},
		Books: s.booksToView(ctx, books),
	}

	s.renderTemplate(w, "books", data)
}

func (s *Server) handleWebNew(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pageSize := getPageSize(r)
	limit := pageSize
	if limit == 0 {
		limit = 10000
	}

	books, err := s.db.GetLastBooks(ctx, limit, 7)
	if err != nil {
		s.renderError(w, "Failed to get new books", err)
		return
	}

	data := BooksData{
		PageData: PageData{
			Title:       "New Books (Last 7 Days)",
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			PageSize:    pageSize,
			CurrentPath: s.config.Server.WebPrefix + "/new",
			HasEPUB:     true, // Internal converter always available
			HasMOBI:     checkEbookConvert(s.config.Converters.FB2ToMOBI),
		},
		Books: s.booksToView(ctx, books),
	}

	s.renderTemplate(w, "books", data)
}

func (s *Server) handleWebBookshelf(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := s.getWebUser(r)

	page := getPage(r)
	pageSize := getPageSize(r)
	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.db.GetBookShelf(ctx, user, pagination)
	if err != nil {
		s.renderError(w, "Failed to get bookshelf", err)
		return
	}

	count, _ := s.db.CountBookShelf(ctx, user)
	hasMore := pageSize > 0 && count > int64(pagination.Offset()+len(books))

	data := BooksData{
		PageData: PageData{
			Title:       "My Bookshelf",
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			HasEPUB:     true, // Internal converter always available
			HasMOBI:     checkEbookConvert(s.config.Converters.FB2ToMOBI),
			CurrentPath: s.config.Server.WebPrefix + "/bookshelf",
			Page:        page,
			PageSize:    pageSize,
			HasMore:     hasMore,
			PrevPage:    page - 1,
			NextPage:    page + 1,
		},
		Books:      s.booksToView(ctx, books),
		TotalCount: int(count),
	}

	s.renderTemplate(w, "bookshelf", data)
}

// getWebUser returns the authenticated username or "anonymous"
func (s *Server) getWebUser(r *http.Request) string {
	if user := r.Context().Value("username"); user != nil {
		return user.(string)
	}
	return "anonymous"
}

// Language code to human-readable name mapping
var languageNames = map[string]string{
	"ru":      "Русский",
	"en":      "English",
	"uk":      "Українська",
	"be":      "Беларуская",
	"de":      "Deutsch",
	"fr":      "Français",
	"es":      "Español",
	"it":      "Italiano",
	"pl":      "Polski",
	"cs":      "Čeština",
	"bg":      "Български",
	"sr":      "Српски",
	"hr":      "Hrvatski",
	"sk":      "Slovenčina",
	"sl":      "Slovenščina",
	"nl":      "Nederlands",
	"pt":      "Português",
	"ro":      "Română",
	"hu":      "Magyar",
	"sv":      "Svenska",
	"da":      "Dansk",
	"no":      "Norsk",
	"fi":      "Suomi",
	"el":      "Ελληνικά",
	"tr":      "Türkçe",
	"ar":      "العربية",
	"he":      "עברית",
	"zh":      "中文",
	"ja":      "日本語",
	"ko":      "한국어",
	"unknown": "Unknown",
}

func getLanguageName(code string) string {
	if name, ok := languageNames[code]; ok {
		return name
	}
	return code
}

func (s *Server) handleWebLanguages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	langs, err := s.db.GetLanguages(ctx)
	if err != nil {
		s.renderError(w, "Failed to get languages", err)
		return
	}

	var langViews []LanguageView
	for _, l := range langs {
		langViews = append(langViews, LanguageView{
			Code:  l.Code,
			Name:  getLanguageName(l.Code),
			Count: l.Count,
		})
	}

	data := LanguagesData{
		PageData: PageData{
			Title:      "Languages",
			SiteTitle:  s.config.Site.Title,
			WebPrefix:  s.config.Server.WebPrefix,
			OPDSPrefix: s.config.Server.OPDSPrefix,
		},
		Languages: langViews,
	}

	s.renderTemplate(w, "languages", data)
}

func (s *Server) handleWebLanguage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := chi.URLParam(r, "lang")

	page := getPage(r)
	pageSize := getPageSize(r)
	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.db.GetBooksForLanguage(ctx, lang, pagination, false)
	if err != nil {
		s.renderError(w, "Failed to get books", err)
		return
	}

	hasMore := pageSize > 0 && len(books) >= pageSize

	data := BooksData{
		PageData: PageData{
			Title:       fmt.Sprintf("Language: %s", getLanguageName(lang)),
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			Page:        page,
			PageSize:    pageSize,
			HasMore:     hasMore,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			CurrentPath: fmt.Sprintf("%s/languages/%s", s.config.Server.WebPrefix, lang),
			HasEPUB:     true, // Internal converter always available
			HasMOBI:     checkEbookConvert(s.config.Converters.FB2ToMOBI),
		},
		Books: s.booksToView(ctx, books),
	}

	s.renderTemplate(w, "books", data)
}

func (s *Server) handleWebCatalogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page := getPage(r)
	pagination := database.NewPagination(page, 100)

	items, err := s.db.GetItemsInCatalog(ctx, 0, pagination, false)
	if err != nil {
		s.renderError(w, "Failed to get catalogs", err)
		return
	}

	data := CatalogsData{
		PageData: PageData{
			Title:       "Catalogs",
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			Page:        page,
			HasMore:     len(items) >= 100,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			CurrentPath: s.config.Server.WebPrefix + "/catalogs",
		},
		Items: s.catalogItemsToView(items),
	}

	s.renderTemplate(w, "catalogs", data)
}

func (s *Server) handleWebCatalog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	catID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.renderError(w, "Invalid catalog ID", err)
		return
	}

	page := getPage(r)
	pagination := database.NewPagination(page, 100)

	items, err := s.db.GetItemsInCatalog(ctx, catID, pagination, false)
	if err != nil {
		s.renderError(w, "Failed to get catalog", err)
		return
	}

	cat, _ := s.db.GetCatalog(ctx, catID)
	title := "Catalog"
	if cat != nil {
		title = cat.Name
	}

	data := CatalogsData{
		PageData: PageData{
			Title:       title,
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			Page:        page,
			HasMore:     len(items) >= 100,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			CurrentPath: fmt.Sprintf("%s/catalogs/%d", s.config.Server.WebPrefix, catID),
		},
		Items: s.catalogItemsToView(items),
	}

	s.renderTemplate(w, "catalogs", data)
}

func (s *Server) handleWebDuplicates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.renderError(w, "Invalid book ID", err)
		return
	}

	page := getPage(r)
	pageSize := getPageSize(r)
	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.db.GetBookDuplicates(ctx, bookID, pagination)
	if err != nil {
		s.renderError(w, "Failed to get duplicates", err)
		return
	}

	// Get the book title for the page header
	title := "Duplicates"
	if len(books) > 0 {
		title = fmt.Sprintf("Duplicates: %s", books[0].Title)
	}

	hasMore := pageSize > 0 && len(books) >= pageSize

	data := BooksData{
		PageData: PageData{
			Title:       title,
			SiteTitle:   s.config.Site.Title,
			WebPrefix:   s.config.Server.WebPrefix,
			OPDSPrefix:  s.config.Server.OPDSPrefix,
			Page:        page,
			PageSize:    pageSize,
			HasMore:     hasMore,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			CurrentPath: fmt.Sprintf("%s/duplicates/%d", s.config.Server.WebPrefix, bookID),
			HasEPUB:     true,
			HasMOBI:     checkEbookConvert(s.config.Converters.FB2ToMOBI),
		},
		Books:      s.booksToView(ctx, books),
		TotalCount: int(pagination.TotalCount),
	}

	s.renderTemplate(w, "books", data)
}

// Helper functions for prefix-based navigation

func (s *Server) getAuthorPrefixes(ctx context.Context, prefix string, length int) []string {
	query := `SELECT DISTINCT UPPER(LEFT(last_name, $1)) as pfx
	          FROM authors a
	          JOIN bauthors ba ON a.author_id = ba.author_id
	          JOIN books b ON ba.book_id = b.book_id
	          WHERE b.avail <> 0 AND last_name <> ''`
	if prefix != "" {
		query += ` AND UPPER(last_name) LIKE $2`
	}
	query += ` ORDER BY pfx`

	var rows pgx.Rows
	var err error
	if prefix != "" {
		rows, err = s.db.Pool().Query(ctx, query, length, prefix+"%")
	} else {
		rows, err = s.db.Pool().Query(ctx, query, length)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()

	var prefixes []string
	for rows.Next() {
		var pfx string
		if err := rows.Scan(&pfx); err == nil && pfx != "" {
			prefixes = append(prefixes, pfx)
		}
	}
	return prefixes
}

func (s *Server) countAuthorsByPrefix(ctx context.Context, prefix string) int {
	var count int
	s.db.Pool().QueryRow(ctx, `
		SELECT COUNT(DISTINCT a.author_id) FROM authors a
		JOIN bauthors ba ON a.author_id = ba.author_id
		JOIN books b ON ba.book_id = b.book_id
		WHERE b.avail <> 0 AND UPPER(last_name) LIKE $1`, prefix+"%").Scan(&count)
	return count
}

func (s *Server) getSeriesPrefixes(ctx context.Context, prefix string, length int) []string {
	query := `SELECT DISTINCT UPPER(LEFT(ser, $1)) as pfx
	          FROM series s
	          JOIN bseries bs ON s.ser_id = bs.ser_id
	          JOIN books b ON bs.book_id = b.book_id
	          WHERE b.avail <> 0 AND ser <> ''`
	if prefix != "" {
		query += ` AND UPPER(ser) LIKE $2`
	}
	query += ` ORDER BY pfx`

	var rows pgx.Rows
	var err error
	if prefix != "" {
		rows, err = s.db.Pool().Query(ctx, query, length, prefix+"%")
	} else {
		rows, err = s.db.Pool().Query(ctx, query, length)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()

	var prefixes []string
	for rows.Next() {
		var pfx string
		if err := rows.Scan(&pfx); err == nil && pfx != "" {
			prefixes = append(prefixes, pfx)
		}
	}
	return prefixes
}

func (s *Server) countSeriesByPrefix(ctx context.Context, prefix string) int {
	var count int
	s.db.Pool().QueryRow(ctx, `
		SELECT COUNT(DISTINCT s.ser_id) FROM series s
		JOIN bseries bs ON s.ser_id = bs.ser_id
		JOIN books b ON bs.book_id = b.book_id
		WHERE b.avail <> 0 AND UPPER(ser) LIKE $1`, prefix+"%").Scan(&count)
	return count
}

// SearchFilterOptions holds unique filter values collected from search results
type SearchFilterOptions struct {
	Languages  []string
	FirstNames []string
	LastNames  []string
	Genres     []LinkedItem
}

func (s *Server) booksToView(ctx context.Context, books []database.Book) []BookView {
	views, _ := s.booksToViewWithFilters(ctx, books)
	return views
}

func (s *Server) booksToViewWithFilters(ctx context.Context, books []database.Book) ([]BookView, SearchFilterOptions) {
	var views []BookView

	// Maps to collect unique values
	langSet := make(map[string]bool)
	firstNameSet := make(map[string]bool)
	lastNameSet := make(map[string]bool)
	genreMap := make(map[int64]string)

	for _, b := range books {
		authors, _ := s.db.GetBookAuthors(ctx, b.ID)
		genres, _ := s.db.GetBookGenres(ctx, b.ID)
		series, _ := s.db.GetBookSeries(ctx, b.ID)

		var authorLinks []LinkedItem
		for _, a := range authors {
			authorLinks = append(authorLinks, LinkedItem{ID: a.ID, Name: a.FullName()})
			// Collect unique first/last names
			if a.FirstName != "" {
				firstNameSet[a.FirstName] = true
			}
			if a.LastName != "" {
				lastNameSet[a.LastName] = true
			}
		}

		var genreLinks []LinkedItem
		for _, g := range genres {
			name := g.Subsection
			if name == "" {
				name = g.Genre
			}
			genreLinks = append(genreLinks, LinkedItem{ID: g.ID, Name: name})
			// Collect unique genres
			genreMap[g.ID] = name
		}

		var seriesLinks []LinkedItem
		for _, ser := range series {
			seriesLinks = append(seriesLinks, LinkedItem{ID: ser.SeriesID, Name: ser.Name})
		}

		// Collect unique languages
		if b.Lang != "" {
			langSet[b.Lang] = true
		}

		isFB2 := strings.ToLower(b.Format) == "fb2"

		// Get duplicate info
		var dupCount int
		if b.Duplicate == 0 {
			// This is an original - count its duplicates
			dupCount, _ = s.db.GetDuplicateCount(ctx, b.ID)
		}

		views = append(views, BookView{
			ID:             b.ID,
			Title:          b.Title,
			Authors:        authorLinks,
			Genres:         genreLinks,
			Series:         seriesLinks,
			Lang:           b.Lang,
			LangName:       getLanguageName(b.Lang),
			Format:         strings.ToUpper(b.Format),
			Size:           formatSize(b.Filesize),
			Annotation:     truncate(b.Annotation, 300),
			HasCover:       isFB2,
			CanEPUB:        isFB2,
			CanMOBI:        isFB2,
			DuplicateOf:    b.Duplicate,
			DuplicateCount: dupCount,
		})
	}

	// Convert sets to sorted slices
	var opts SearchFilterOptions
	for lang := range langSet {
		opts.Languages = append(opts.Languages, lang)
	}
	sort.Strings(opts.Languages)

	for name := range firstNameSet {
		opts.FirstNames = append(opts.FirstNames, name)
	}
	sort.Strings(opts.FirstNames)

	for name := range lastNameSet {
		opts.LastNames = append(opts.LastNames, name)
	}
	sort.Strings(opts.LastNames)

	for id, name := range genreMap {
		opts.Genres = append(opts.Genres, LinkedItem{ID: id, Name: name})
	}
	sort.Slice(opts.Genres, func(i, j int) bool {
		return opts.Genres[i].Name < opts.Genres[j].Name
	})

	return views, opts
}

func (s *Server) catalogItemsToView(items []database.CatalogItem) []CatalogItem {
	var views []CatalogItem
	for _, item := range items {
		if item.ItemType == "catalog" {
			views = append(views, CatalogItem{
				ID:       item.ID,
				Name:     item.Name,
				IsFolder: true,
			})
		} else {
			canConvert := strings.ToLower(item.Format) == "fb2"
			views = append(views, CatalogItem{
				ID:       item.ID,
				Name:     item.Name,
				IsFolder: false,
				Book: &BookView{
					ID:      item.ID,
					Title:   item.Title,
					Format:  strings.ToUpper(item.Format),
					Size:    formatSize(item.Filesize),
					CanEPUB: canConvert,
					CanMOBI: canConvert,
				},
			})
		}
	}
	return views
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (s *Server) renderError(w http.ResponseWriter, message string, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	data := PageData{
		Title:      "Error",
		SiteTitle:  s.config.Site.Title,
		WebPrefix:  s.config.Server.WebPrefix,
		OPDSPrefix: s.config.Server.OPDSPrefix,
	}
	s.renderTemplate(w, "error", struct {
		PageData
		Error string
	}{data, fmt.Sprintf("%s: %v", message, err)})
}

func (s *Server) renderTemplate(w http.ResponseWriter, name string, data interface{}) {
	funcMap := template.FuncMap{
		"sortPrefixes": func(prefixes []string) []string {
			sorted := make([]string, len(prefixes))
			copy(sorted, prefixes)
			sort.Strings(sorted)
			return sorted
		},
		"sub": func(a, b int) int {
			return a - b
		},
	}

	t, err := template.New("page").Funcs(funcMap).Parse(baseTemplate)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	content, ok := templates[name]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}

	t, err = t.Parse(content)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		http.Error(w, "Render error: "+err.Error(), http.StatusInternalServerError)
	}
}

// Templates - Modern UI

const baseTemplate = `<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{.Title}} - {{.SiteTitle}}</title>
    <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.5.1/css/all.min.css">
    <style>
        :root {
            --primary: #6366f1;
            --primary-dark: #4f46e5;
            --secondary: #0ea5e9;
            --success: #22c55e;
            --warning: #f59e0b;
            --danger: #ef4444;
            --dark: #1e293b;
            --light: #f8fafc;
            --gray: #64748b;
            --border: #e2e8f0;
            --shadow: 0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1);
            --radius: 12px;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            color: var(--dark);
        }
        .container {
            max-width: 1400px;
            margin: 0 auto;
            padding: 20px;
        }
        header {
            background: rgba(255,255,255,0.95);
            backdrop-filter: blur(10px);
            border-radius: var(--radius);
            padding: 20px 30px;
            margin-bottom: 25px;
            box-shadow: var(--shadow);
        }
        .header-top {
            display: flex;
            justify-content: space-between;
            align-items: center;
            flex-wrap: wrap;
            gap: 20px;
        }
        header h1 {
            font-size: 1.8rem;
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            display: flex;
            align-items: center;
            gap: 10px;
        }
        header h1 i { -webkit-text-fill-color: var(--primary); }
        nav {
            display: flex;
            flex-wrap: wrap;
            gap: 8px;
        }
        nav a {
            color: var(--dark);
            text-decoration: none;
            padding: 10px 18px;
            border-radius: 25px;
            font-weight: 500;
            font-size: 0.9rem;
            transition: all 0.3s;
            display: flex;
            align-items: center;
            gap: 8px;
            background: var(--light);
            border: 1px solid var(--border);
        }
        nav a:hover {
            background: var(--primary);
            color: white;
            transform: translateY(-2px);
            box-shadow: 0 4px 12px rgba(99, 102, 241, 0.4);
        }
        .search-form {
            display: flex;
            gap: 10px;
            margin-top: 20px;
            max-width: 600px;
        }
        .search-form input[type="text"] {
            flex: 1;
            padding: 14px 20px;
            border: 2px solid var(--border);
            border-radius: 30px;
            font-size: 1rem;
            transition: all 0.3s;
        }
        .search-form input:focus {
            outline: none;
            border-color: var(--primary);
            box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.1);
        }
        .search-form button {
            padding: 14px 30px;
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            color: white;
            border: none;
            border-radius: 30px;
            cursor: pointer;
            font-weight: 600;
            transition: all 0.3s;
        }
        .search-form button:hover {
            transform: scale(1.05);
            box-shadow: 0 4px 15px rgba(99, 102, 241, 0.4);
        }
        main {
            background: rgba(255,255,255,0.95);
            backdrop-filter: blur(10px);
            border-radius: var(--radius);
            padding: 30px;
            box-shadow: var(--shadow);
        }
        h2 {
            font-size: 1.5rem;
            color: var(--dark);
            margin-bottom: 25px;
            display: flex;
            align-items: center;
            gap: 12px;
        }
        h2 i { color: var(--primary); }
        .prefix-cloud {
            display: flex;
            flex-wrap: wrap;
            gap: 10px;
            margin: 20px 0;
        }
        .prefix-cloud a {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            min-width: 50px;
            padding: 12px 18px;
            background: linear-gradient(135deg, #f8fafc, #e2e8f0);
            border: 1px solid var(--border);
            border-radius: 10px;
            text-decoration: none;
            color: var(--dark);
            font-weight: 600;
            font-size: 1rem;
            transition: all 0.3s;
        }
        .prefix-cloud a:hover {
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            color: white;
            transform: translateY(-3px);
            box-shadow: 0 6px 20px rgba(99, 102, 241, 0.3);
        }
        .book-grid {
            display: grid;
            gap: 20px;
        }
        .book-card {
            background: white;
            border-radius: var(--radius);
            padding: 20px;
            box-shadow: 0 2px 8px rgba(0,0,0,0.06);
            border: 1px solid var(--border);
            transition: all 0.3s;
        }
        .book-card:hover {
            transform: translateY(-4px);
            box-shadow: 0 12px 24px rgba(0,0,0,0.1);
        }
        .book-title {
            font-size: 1.15rem;
            font-weight: 700;
            color: var(--dark);
            margin-bottom: 10px;
        }
        .book-meta {
            display: flex;
            flex-wrap: wrap;
            gap: 15px;
            color: var(--gray);
            font-size: 0.9rem;
            margin-bottom: 10px;
        }
        .book-meta span {
            display: flex;
            align-items: center;
            gap: 6px;
        }
        .book-meta i { color: var(--primary); font-size: 0.85rem; }
        .meta-link {
            color: var(--dark);
            text-decoration: none;
            border-bottom: 1px dotted var(--gray);
            transition: all 0.2s;
        }
        .meta-link:hover {
            color: var(--primary);
            border-bottom-color: var(--primary);
        }
        .more-info {
            color: var(--gray);
            font-size: 0.85em;
            font-style: italic;
            cursor: pointer;
            position: relative;
            display: inline-block;
        }
        .more-info-tooltip {
            visibility: hidden;
            opacity: 0;
            position: absolute;
            bottom: 100%;
            left: 50%;
            transform: translateX(-50%);
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 10px;
            border-radius: 8px;
            z-index: 1000;
            box-shadow: 0 4px 15px rgba(102, 126, 234, 0.4);
            margin-bottom: 8px;
            display: grid;
            grid-template-columns: auto auto auto auto;
            gap: 6px 15px;
            pointer-events: none;
        }
        .more-info:hover .more-info-tooltip {
            visibility: visible;
            opacity: 1;
            pointer-events: auto;
        }
        .more-info-tooltip::after {
            content: '';
            position: absolute;
            top: 100%;
            left: 50%;
            transform: translateX(-50%);
            border: 6px solid transparent;
            border-top-color: #764ba2;
        }
        .more-info-tooltip.below {
            bottom: auto;
            top: 100%;
            margin-bottom: 0;
            margin-top: 8px;
        }
        .more-info-tooltip.below::after {
            top: auto;
            bottom: 100%;
            border-top-color: transparent;
            border-bottom-color: #667eea;
        }
        .more-info-tooltip a {
            color: #fff;
            text-decoration: none;
            white-space: nowrap;
            padding: 2px 4px;
            border-radius: 3px;
        }
        .more-info-tooltip a:hover {
            background: rgba(255,255,255,0.2);
        }
        .book-annotation {
            color: #475569;
            font-size: 0.9rem;
            line-height: 1.6;
            margin: 12px 0;
        }
        .book-actions {
            display: flex;
            flex-wrap: wrap;
            gap: 10px;
            margin-top: 15px;
        }
        .btn {
            display: inline-flex;
            align-items: center;
            gap: 8px;
            padding: 10px 18px;
            border-radius: 8px;
            font-weight: 600;
            font-size: 0.85rem;
            text-decoration: none;
            transition: all 0.3s;
            border: none;
            cursor: pointer;
        }
        .btn-primary {
            background: linear-gradient(135deg, var(--primary), var(--primary-dark));
            color: white;
        }
        .btn-primary:hover {
            transform: translateY(-2px);
            box-shadow: 0 4px 12px rgba(99, 102, 241, 0.4);
        }
        .btn-success {
            background: linear-gradient(135deg, var(--success), #16a34a);
            color: white;
        }
        .btn-success:hover {
            box-shadow: 0 4px 12px rgba(34, 197, 94, 0.4);
        }
        .btn-warning {
            background: linear-gradient(135deg, var(--warning), #d97706);
            color: white;
        }
        .btn-warning:hover {
            box-shadow: 0 4px 12px rgba(245, 158, 11, 0.4);
        }
        .btn-danger {
            background: linear-gradient(135deg, #ef4444, #dc2626);
            color: white;
        }
        .btn-danger:hover {
            box-shadow: 0 4px 12px rgba(239, 68, 68, 0.4);
        }
        .btn-secondary {
            background: linear-gradient(135deg, #6b7280, #4b5563);
            color: white;
        }
        .btn-secondary:hover {
            box-shadow: 0 4px 12px rgba(107, 114, 128, 0.4);
        }
        .book-card-content {
            display: flex;
            gap: 15px;
        }
        .book-cover {
            width: 80px;
            height: 120px;
            object-fit: cover;
            border-radius: 6px;
            flex-shrink: 0;
            box-shadow: 0 2px 8px rgba(0,0,0,0.15);
        }
        .book-info {
            flex: 1;
            min-width: 0;
        }
        .page-size-selector {
            display: flex;
            align-items: center;
            gap: 5px;
        }
        .size-btn {
            padding: 6px 12px;
            background: var(--light);
            border: 1px solid var(--border);
            border-radius: 6px;
            text-decoration: none;
            color: var(--dark);
            font-size: 0.85rem;
            font-weight: 500;
            transition: all 0.2s;
        }
        .size-btn:hover {
            background: var(--primary);
            color: white;
            border-color: var(--primary);
        }
        .size-btn.active {
            background: var(--primary);
            color: white;
            border-color: var(--primary);
        }
        .item-list {
            list-style: none;
        }
        .item-list li {
            border-bottom: 1px solid var(--border);
        }
        .item-list li:last-child { border-bottom: none; }
        .item-list a {
            display: flex;
            align-items: center;
            gap: 12px;
            padding: 15px;
            text-decoration: none;
            color: var(--dark);
            transition: all 0.2s;
        }
        .item-list a:hover {
            background: var(--light);
            padding-left: 25px;
        }
        .item-list i { color: var(--primary); font-size: 1.1rem; }
        .pagination {
            display: flex;
            gap: 12px;
            justify-content: center;
            margin: 30px 0 10px;
        }
        .pagination a {
            padding: 12px 24px;
            background: white;
            border: 2px solid var(--border);
            border-radius: 10px;
            text-decoration: none;
            color: var(--dark);
            font-weight: 600;
            transition: all 0.3s;
        }
        .pagination a:hover {
            background: var(--primary);
            border-color: var(--primary);
            color: white;
        }
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
            gap: 20px;
            margin: 25px 0;
        }
        .stat-card {
            background: linear-gradient(135deg, #f8fafc, white);
            padding: 25px;
            border-radius: var(--radius);
            text-align: center;
            border: 1px solid var(--border);
            transition: all 0.3s;
        }
        .stat-card:hover {
            transform: translateY(-4px);
            box-shadow: var(--shadow);
        }
        .stat-card .number {
            font-size: 2.5rem;
            font-weight: 800;
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }
        .stat-card .label {
            color: var(--gray);
            margin-top: 8px;
            font-weight: 500;
        }
        .menu-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            margin: 25px 0;
        }
        .menu-card {
            background: white;
            padding: 35px 25px;
            border-radius: var(--radius);
            text-align: center;
            text-decoration: none;
            color: var(--dark);
            border: 1px solid var(--border);
            transition: all 0.3s;
        }
        .menu-card:hover {
            transform: translateY(-6px);
            box-shadow: 0 15px 30px rgba(0,0,0,0.12);
            border-color: var(--primary);
        }
        .menu-card i {
            font-size: 3rem;
            margin-bottom: 15px;
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }
        .menu-card .title {
            font-size: 1.2rem;
            font-weight: 700;
        }
        .sections-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
            gap: 15px;
        }
        .section-card {
            background: white;
            padding: 20px;
            border-radius: 10px;
            text-decoration: none;
            color: var(--dark);
            border: 1px solid var(--border);
            transition: all 0.3s;
            display: flex;
            align-items: center;
            gap: 12px;
        }
        .section-card:hover {
            background: var(--primary);
            color: white;
            transform: translateX(5px);
        }
        .section-card i { font-size: 1.2rem; }
        @media (max-width: 768px) {
            .header-top { flex-direction: column; align-items: stretch; }
            nav { justify-content: center; }
            .search-form { max-width: 100%; }
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <div class="header-top">
                <h1><i class="fas fa-book-open"></i> {{.SiteTitle}}</h1>
                <nav>
                    <a href="{{.WebPrefix}}/"><i class="fas fa-home"></i> Home</a>
                    <a href="{{.WebPrefix}}/catalogs"><i class="fas fa-folder"></i> Catalogs</a>
                    <a href="{{.WebPrefix}}/authors"><i class="fas fa-user-pen"></i> Authors</a>
                    <a href="{{.WebPrefix}}/genres"><i class="fas fa-tags"></i> Genres</a>
                    <a href="{{.WebPrefix}}/series"><i class="fas fa-layer-group"></i> Series</a>
                    <a href="{{.WebPrefix}}/languages"><i class="fas fa-globe"></i> Languages</a>
                    <a href="{{.WebPrefix}}/new"><i class="fas fa-sparkles"></i> New</a>
                    <a href="{{.WebPrefix}}/bookshelf"><i class="fas fa-bookmark"></i> Bookshelf</a>
                    <a href="{{.OPDSPrefix}}/"><i class="fas fa-rss"></i> OPDS</a>
                </nav>
            </div>
            <form class="search-form" action="{{.WebPrefix}}/search" method="get">
                <input type="text" name="q" placeholder="Search books by title or author..." value="{{.Query}}">
                <button type="submit"><i class="fas fa-search"></i> Search</button>
            </form>
        </header>
        <main>
            {{template "content" .}}
        </main>
    </div>
    <script>
    function bookshelfAction(btn, url) {
        fetch(url, {method: 'POST', credentials: 'same-origin'})
            .then(resp => {
                if (resp.ok) {
                    if (url.includes('/remove/')) {
                        btn.closest('.book-card').remove();
                        var countEl = document.getElementById('bookshelf-count');
                        if (countEl) {
                            countEl.textContent = parseInt(countEl.textContent) - 1;
                        }
                    } else {
                        btn.innerHTML = '<i class="fas fa-check"></i> Added';
                        btn.classList.remove('btn-secondary');
                        btn.classList.add('btn-success');
                        btn.onclick = null;
                    }
                }
            })
            .catch(err => console.error(err));
        return false;
    }
    </script>
</body>
</html>
{{define "content"}}{{end}}`

var templates = map[string]string{
	"main": `{{define "content"}}
<h2><i class="fas fa-chart-pie"></i> Library Statistics</h2>
{{if .Stats}}
<div class="stats-grid">
    <div class="stat-card">
        <div class="number">{{.Stats.BooksCount}}</div>
        <div class="label"><i class="fas fa-book"></i> Books</div>
    </div>
    <div class="stat-card">
        <div class="number">{{.Stats.AuthorsCount}}</div>
        <div class="label"><i class="fas fa-users"></i> Authors</div>
    </div>
    <div class="stat-card">
        <div class="number">{{.Stats.GenresCount}}</div>
        <div class="label"><i class="fas fa-tags"></i> Genres</div>
    </div>
    <div class="stat-card">
        <div class="number">{{.Stats.SeriesCount}}</div>
        <div class="label"><i class="fas fa-list"></i> Series</div>
    </div>
    {{if .NewBooks}}
    <div class="stat-card">
        <div class="number">{{.NewBooks}}</div>
        <div class="label"><i class="fas fa-star"></i> New (7d)</div>
    </div>
    {{end}}
</div>
{{end}}
<h2><i class="fas fa-compass"></i> Browse Library</h2>
<div class="menu-grid">
    <a href="{{.WebPrefix}}/catalogs" class="menu-card">
        <i class="fas fa-folder-tree"></i>
        <div class="title">Catalogs</div>
    </a>
    <a href="{{.WebPrefix}}/authors" class="menu-card">
        <i class="fas fa-user-pen"></i>
        <div class="title">Authors</div>
    </a>
    <a href="{{.WebPrefix}}/genres" class="menu-card">
        <i class="fas fa-masks-theater"></i>
        <div class="title">Genres</div>
    </a>
    <a href="{{.WebPrefix}}/series" class="menu-card">
        <i class="fas fa-layer-group"></i>
        <div class="title">Series</div>
    </a>
    <a href="{{.WebPrefix}}/languages" class="menu-card">
        <i class="fas fa-globe"></i>
        <div class="title">Languages</div>
    </a>
    <a href="{{.WebPrefix}}/new" class="menu-card">
        <i class="fas fa-wand-magic-sparkles"></i>
        <div class="title">New Books</div>
    </a>
    <a href="{{.WebPrefix}}/search" class="menu-card">
        <i class="fas fa-magnifying-glass"></i>
        <div class="title">Search</div>
    </a>
    <a href="{{.WebPrefix}}/bookshelf" class="menu-card">
        <i class="fas fa-bookmark"></i>
        <div class="title">Bookshelf</div>
    </a>
</div>
{{end}}`,

	"search": `{{define "content"}}
<h2><i class="fas fa-search"></i> Search Books</h2>
<p style="color: var(--gray); font-size: 1.1rem;">Use the search box above to find books by title or author name.</p>
{{end}}`,

	"books": `{{define "content"}}
<div style="display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 15px; margin-bottom: 20px;">
    <h2 style="margin: 0;"><i class="fas fa-book"></i> {{.Title}}{{if .TotalCount}} <span style="color: var(--gray); font-weight: normal; font-size: 0.7em;">({{.TotalCount}} books)</span>{{end}}</h2>
    {{if .CurrentPath}}
    <div class="page-size-selector">
        <span style="color: var(--gray); margin-right: 10px;">Show:</span>
        <a href="{{.CurrentPath}}?size=10{{if .Query}}&q={{.Query}}{{end}}{{if .Prefix}}&prefix={{.Prefix}}{{end}}" class="size-btn{{if eq .PageSize 10}} active{{end}}">10</a>
        <a href="{{.CurrentPath}}?size=50{{if .Query}}&q={{.Query}}{{end}}{{if .Prefix}}&prefix={{.Prefix}}{{end}}" class="size-btn{{if eq .PageSize 50}} active{{end}}">50</a>
        <a href="{{.CurrentPath}}?size=100{{if .Query}}&q={{.Query}}{{end}}{{if .Prefix}}&prefix={{.Prefix}}{{end}}" class="size-btn{{if eq .PageSize 100}} active{{end}}">100</a>
        <a href="{{.CurrentPath}}?size=200{{if .Query}}&q={{.Query}}{{end}}{{if .Prefix}}&prefix={{.Prefix}}{{end}}" class="size-btn{{if eq .PageSize 200}} active{{end}}">200</a>
        <a href="{{.CurrentPath}}?size=all{{if .Query}}&q={{.Query}}{{end}}{{if .Prefix}}&prefix={{.Prefix}}{{end}}" class="size-btn{{if eq .PageSize 0}} active{{end}}">All</a>
    </div>
    {{end}}
</div>
{{if or .Languages .FirstNames .LastNames .Genres}}
<div style="display: flex; gap: 15px; margin-bottom: 20px; flex-wrap: wrap; align-items: center;">
    <span style="color: var(--gray);"><i class="fas fa-filter"></i> Filters:</span>
    {{if and .Languages (not .FilterLang)}}
    <select onchange="applyFilter('lang', this.value)" style="padding: 8px 12px; border-radius: 8px; border: 1px solid var(--border); background: var(--card-bg); color: var(--text);">
        <option value="">All Languages</option>
        {{range .Languages}}<option value="{{.}}">{{.}}</option>{{end}}
    </select>
    {{end}}
    {{if .FilterLang}}
    <span style="padding: 8px 12px; border-radius: 8px; background: var(--accent); color: white;">Lang: {{.FilterLang}} <a href="javascript:applyFilter('lang','')" style="color: white; margin-left: 5px;">×</a></span>
    {{end}}
    {{if and .FirstNames (not .FilterFirstName)}}
    <select onchange="applyFilter('fname', this.value)" style="padding: 8px 12px; border-radius: 8px; border: 1px solid var(--border); background: var(--card-bg); color: var(--text);">
        <option value="">All First Names</option>
        {{range .FirstNames}}<option value="{{.}}">{{.}}</option>{{end}}
    </select>
    {{end}}
    {{if .FilterFirstName}}
    <span style="padding: 8px 12px; border-radius: 8px; background: var(--accent); color: white;">First: {{.FilterFirstName}} <a href="javascript:applyFilter('fname','')" style="color: white; margin-left: 5px;">×</a></span>
    {{end}}
    {{if and .LastNames (not .FilterLastName)}}
    <select onchange="applyFilter('lname', this.value)" style="padding: 8px 12px; border-radius: 8px; border: 1px solid var(--border); background: var(--card-bg); color: var(--text);">
        <option value="">All Last Names</option>
        {{range .LastNames}}<option value="{{.}}">{{.}}</option>{{end}}
    </select>
    {{end}}
    {{if .FilterLastName}}
    <span style="padding: 8px 12px; border-radius: 8px; background: var(--accent); color: white;">Last: {{.FilterLastName}} <a href="javascript:applyFilter('lname','')" style="color: white; margin-left: 5px;">×</a></span>
    {{end}}
    {{if and .Genres (not .FilterGenre)}}
    <select onchange="applyFilter('genre', this.value)" style="padding: 8px 12px; border-radius: 8px; border: 1px solid var(--border); background: var(--card-bg); color: var(--text);">
        <option value="">All Genres</option>
        {{range .Genres}}<option value="{{.ID}}">{{.Name}}</option>{{end}}
    </select>
    {{end}}
    {{if .FilterGenre}}
    <span style="padding: 8px 12px; border-radius: 8px; background: var(--accent); color: white;">Genre: {{.FilterGenreName}} <a href="javascript:applyFilter('genre','')" style="color: white; margin-left: 5px;">×</a></span>
    {{end}}
    {{if or .FilterLang .FilterFirstName .FilterLastName .FilterGenre}}
    <a href="{{.CurrentPath}}?q={{.Query}}" style="color: var(--accent); text-decoration: none;"><i class="fas fa-times"></i> Clear all</a>
    {{end}}
</div>
<script>
function applyFilter(name, value) {
    var url = new URL(window.location.href);
    if (value) {
        url.searchParams.set(name, value);
    } else {
        url.searchParams.delete(name);
    }
    url.searchParams.delete('page');
    window.location.href = url.toString();
}
</script>
{{end}}
{{if .Books}}
<div class="book-grid">
{{range .Books}}
    <div class="book-card">
        <div class="book-card-content">
            {{if .HasCover}}<img src="{{$.OPDSPrefix}}/book/{{.ID}}/cover" class="book-cover" alt="Cover" onerror="this.style.display='none'">{{end}}
            <div class="book-info">
                <div class="book-title">{{.Title}}</div>
                <div class="book-meta">
                    {{if .Authors}}<span><i class="fas fa-user"></i> {{range $i, $a := .Authors}}{{if lt $i 2}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/authors/{{$a.ID}}" class="meta-link">{{$a.Name}}</a>{{end}}{{end}}{{if gt (len .Authors) 2}} <span class="more-info">+{{sub (len .Authors) 2}}<div class="more-info-tooltip{{if gt (len .Authors) 70}} below{{end}}">{{range $i, $a := .Authors}}{{if ge $i 2}}<a href="{{$.WebPrefix}}/authors/{{$a.ID}}">{{$a.Name}}</a>{{end}}{{end}}</div></span>{{end}}</span>{{end}}
                    {{if .Series}}<span><i class="fas fa-layer-group"></i> {{range $i, $s := .Series}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/series/{{$s.ID}}" class="meta-link">{{$s.Name}}</a>{{end}}</span>{{end}}
                    {{if .Lang}}<span><i class="fas fa-globe"></i> <a href="{{$.WebPrefix}}/languages/{{.Lang}}" class="meta-link">{{.LangName}}</a></span>{{end}}
                    <span><i class="fas fa-file"></i> {{.Format}}</span>
                    <span><i class="fas fa-weight-hanging"></i> {{.Size}}</span>
                </div>
                {{if .Genres}}<div class="book-meta"><span><i class="fas fa-tag"></i> {{range $i, $g := .Genres}}{{if lt $i 3}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/genres/{{$g.ID}}" class="meta-link">{{$g.Name}}</a>{{end}}{{end}}{{if gt (len .Genres) 3}} <span class="more-info">+{{sub (len .Genres) 3}}<div class="more-info-tooltip{{if gt (len .Genres) 71}} below{{end}}">{{range $i, $g := .Genres}}{{if ge $i 3}}<a href="{{$.WebPrefix}}/genres/{{$g.ID}}">{{$g.Name}}</a>{{end}}{{end}}</div></span>{{end}}</span></div>{{end}}
                {{if .Annotation}}<div class="book-annotation">{{.Annotation}}</div>{{end}}
            </div>
        </div>
        <div class="book-actions">
            <a href="{{$.OPDSPrefix}}/book/{{.ID}}/download" class="btn btn-primary"><i class="fas fa-download"></i> {{.Format}}</a>
            {{if and $.HasEPUB .CanEPUB}}<a href="{{$.OPDSPrefix}}/book/{{.ID}}/epub" class="btn btn-success"><i class="fas fa-file-arrow-down"></i> EPUB</a>{{end}}
            {{if and $.HasMOBI .CanMOBI}}<a href="{{$.OPDSPrefix}}/book/{{.ID}}/mobi" class="btn btn-warning"><i class="fas fa-file-arrow-down"></i> MOBI</a>{{end}}
            <a href="#" onclick="return bookshelfAction(this, '{{$.WebPrefix}}/bookshelf/add/{{.ID}}')" class="btn btn-secondary"><i class="fas fa-bookmark"></i> Bookshelf</a>
            {{if or (gt .DuplicateCount 0) (gt .DuplicateOf 0)}}<a href="{{$.WebPrefix}}/duplicates/{{.ID}}" class="btn btn-secondary"><i class="fas fa-copy"></i> Duplicates{{if gt .DuplicateCount 0}} ({{.DuplicateCount}}){{end}}</a>{{end}}
        </div>
    </div>
{{end}}
</div>
{{if or (gt .Page 0) .HasMore}}
<div class="pagination">
    {{if gt .Page 0}}<a href="{{.CurrentPath}}?page={{.PrevPage}}{{if .Query}}&q={{.Query}}{{end}}{{if .Prefix}}&prefix={{.Prefix}}{{end}}{{if .PageSize}}&size={{.PageSize}}{{end}}"><i class="fas fa-arrow-left"></i> Previous</a>{{end}}
    {{if .HasMore}}<a href="{{.CurrentPath}}?page={{.NextPage}}{{if .Query}}&q={{.Query}}{{end}}{{if .Prefix}}&prefix={{.Prefix}}{{end}}{{if .PageSize}}&size={{.PageSize}}{{end}}">Next <i class="fas fa-arrow-right"></i></a>{{end}}
</div>
{{end}}
{{else}}
<p style="text-align: center; color: var(--gray); padding: 40px;">No books found.</p>
{{end}}
{{end}}`,

	"bookshelf": `{{define "content"}}
<div style="display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 15px; margin-bottom: 20px;">
    <h2 style="margin: 0;"><i class="fas fa-bookmark"></i> My Bookshelf {{if .TotalCount}}(<span id="bookshelf-count">{{.TotalCount}}</span> books){{end}}</h2>
    <div class="page-size-selector">
        <span style="color: var(--gray); margin-right: 10px;">Show:</span>
        <a href="{{.CurrentPath}}?size=10" class="size-btn{{if eq .PageSize 10}} active{{end}}">10</a>
        <a href="{{.CurrentPath}}?size=50" class="size-btn{{if eq .PageSize 50}} active{{end}}">50</a>
        <a href="{{.CurrentPath}}?size=100" class="size-btn{{if eq .PageSize 100}} active{{end}}">100</a>
        <a href="{{.CurrentPath}}?size=200" class="size-btn{{if eq .PageSize 200}} active{{end}}">200</a>
        <a href="{{.CurrentPath}}?size=all" class="size-btn{{if eq .PageSize 0}} active{{end}}">All</a>
    </div>
</div>
{{if .Books}}
<div class="book-grid">
{{range .Books}}
    <div class="book-card">
        <div class="book-card-content">
            {{if .HasCover}}<img src="{{$.OPDSPrefix}}/book/{{.ID}}/cover" class="book-cover" alt="Cover" onerror="this.style.display='none'">{{end}}
            <div class="book-info">
                <div class="book-title">{{.Title}}</div>
                <div class="book-meta">
                    {{if .Authors}}<span><i class="fas fa-user"></i> {{range $i, $a := .Authors}}{{if lt $i 2}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/authors/{{$a.ID}}" class="meta-link">{{$a.Name}}</a>{{end}}{{end}}{{if gt (len .Authors) 2}} <span class="more-info">+{{sub (len .Authors) 2}}<div class="more-info-tooltip{{if gt (len .Authors) 70}} below{{end}}">{{range $i, $a := .Authors}}{{if ge $i 2}}<a href="{{$.WebPrefix}}/authors/{{$a.ID}}">{{$a.Name}}</a>{{end}}{{end}}</div></span>{{end}}</span>{{end}}
                    {{if .Series}}<span><i class="fas fa-layer-group"></i> {{range $i, $s := .Series}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/series/{{$s.ID}}" class="meta-link">{{$s.Name}}</a>{{end}}</span>{{end}}
                    {{if .Lang}}<span><i class="fas fa-globe"></i> <a href="{{$.WebPrefix}}/languages/{{.Lang}}" class="meta-link">{{.LangName}}</a></span>{{end}}
                    <span><i class="fas fa-file"></i> {{.Format}}</span>
                    <span><i class="fas fa-weight-hanging"></i> {{.Size}}</span>
                </div>
                {{if .Genres}}<div class="book-meta"><span><i class="fas fa-tag"></i> {{range $i, $g := .Genres}}{{if lt $i 3}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/genres/{{$g.ID}}" class="meta-link">{{$g.Name}}</a>{{end}}{{end}}{{if gt (len .Genres) 3}} <span class="more-info">+{{sub (len .Genres) 3}}<div class="more-info-tooltip{{if gt (len .Genres) 71}} below{{end}}">{{range $i, $g := .Genres}}{{if ge $i 3}}<a href="{{$.WebPrefix}}/genres/{{$g.ID}}">{{$g.Name}}</a>{{end}}{{end}}</div></span>{{end}}</span></div>{{end}}
                {{if .Annotation}}<div class="book-annotation">{{.Annotation}}</div>{{end}}
            </div>
        </div>
        <div class="book-actions">
            <a href="{{$.OPDSPrefix}}/book/{{.ID}}/download" class="btn btn-primary"><i class="fas fa-download"></i> {{.Format}}</a>
            {{if and $.HasEPUB .CanEPUB}}<a href="{{$.OPDSPrefix}}/book/{{.ID}}/epub" class="btn btn-success"><i class="fas fa-file-arrow-down"></i> EPUB</a>{{end}}
            {{if and $.HasMOBI .CanMOBI}}<a href="{{$.OPDSPrefix}}/book/{{.ID}}/mobi" class="btn btn-warning"><i class="fas fa-file-arrow-down"></i> MOBI</a>{{end}}
            {{if or (gt .DuplicateCount 0) (gt .DuplicateOf 0)}}<a href="{{$.WebPrefix}}/duplicates/{{.ID}}" class="btn btn-secondary"><i class="fas fa-copy"></i> Duplicates{{if gt .DuplicateCount 0}} ({{.DuplicateCount}}){{end}}</a>{{end}}
            <a href="#" onclick="return bookshelfAction(this, '{{$.WebPrefix}}/bookshelf/remove/{{.ID}}')" class="btn btn-danger"><i class="fas fa-trash"></i> Remove</a>
        </div>
    </div>
{{end}}
</div>
{{if or (gt .Page 0) .HasMore}}
<div class="pagination">
    {{if gt .Page 0}}<a href="{{.CurrentPath}}?page={{.PrevPage}}{{if .PageSize}}&size={{.PageSize}}{{end}}"><i class="fas fa-arrow-left"></i> Previous</a>{{end}}
    {{if .HasMore}}<a href="{{.CurrentPath}}?page={{.NextPage}}{{if .PageSize}}&size={{.PageSize}}{{end}}">Next <i class="fas fa-arrow-right"></i></a>{{end}}
</div>
{{end}}
{{else}}
<p style="text-align: center; color: var(--gray); padding: 40px;">Your bookshelf is empty. Add books by clicking the bookmark button on any book.</p>
{{end}}
{{end}}`,

	"authors_index": `{{define "content"}}
<h2><i class="fas fa-user-pen"></i> Authors{{if .Prefix}}: {{.Prefix}}{{end}}</h2>
{{if .Prefixes}}
<div class="prefix-cloud">
{{range .Prefixes}}
    <a href="{{$.WebPrefix}}/authors?prefix={{.}}">{{.}}</a>
{{end}}
</div>
{{else}}
<p style="text-align: center; color: var(--gray);">No authors found.</p>
{{end}}
{{end}}`,

	"authors": `{{define "content"}}
<h2><i class="fas fa-user-pen"></i> {{.Title}}</h2>
{{if .Authors}}
<ul class="item-list">
{{range .Authors}}
    <li><a href="{{$.WebPrefix}}/authors/{{.ID}}"><i class="fas fa-user"></i> {{.Name}}</a></li>
{{end}}
</ul>
{{if or (gt .Page 0) .HasMore}}
<div class="pagination">
    {{if gt .Page 0}}<a href="{{.CurrentPath}}?prefix={{.Prefix}}&page={{.PrevPage}}"><i class="fas fa-arrow-left"></i> Previous</a>{{end}}
    {{if .HasMore}}<a href="{{.CurrentPath}}?prefix={{.Prefix}}&page={{.NextPage}}">Next <i class="fas fa-arrow-right"></i></a>{{end}}
</div>
{{end}}
{{else}}
<p style="text-align: center; color: var(--gray);">No authors found.</p>
{{end}}
{{end}}`,

	"genres_index": `{{define "content"}}
<h2><i class="fas fa-masks-theater"></i> Genres</h2>
{{if .Sections}}
<div class="sections-grid">
{{range .Sections}}
    <a href="{{$.WebPrefix}}/genres?section={{.}}" class="section-card"><i class="fas fa-folder"></i> {{.}}</a>
{{end}}
</div>
{{else}}
<p style="text-align: center; color: var(--gray);">No genres found.</p>
{{end}}
{{end}}`,

	"genres": `{{define "content"}}
<h2><i class="fas fa-masks-theater"></i> {{.Title}}</h2>
{{if .Genres}}
<ul class="item-list">
{{range .Genres}}
    <li><a href="{{$.WebPrefix}}/genres/{{.ID}}"><i class="fas fa-bookmark"></i> {{.Name}}</a></li>
{{end}}
</ul>
{{else}}
<p style="text-align: center; color: var(--gray);">No genres found.</p>
{{end}}
{{end}}`,

	"series_index": `{{define "content"}}
<h2><i class="fas fa-layer-group"></i> Series{{if .Prefix}}: {{.Prefix}}{{end}}</h2>
{{if .Prefixes}}
<div class="prefix-cloud">
{{range .Prefixes}}
    <a href="{{$.WebPrefix}}/series?prefix={{.}}">{{.}}</a>
{{end}}
</div>
{{else}}
<p style="text-align: center; color: var(--gray);">No series found.</p>
{{end}}
{{end}}`,

	"series": `{{define "content"}}
<h2><i class="fas fa-layer-group"></i> {{.Title}}</h2>
{{if .Series}}
<ul class="item-list">
{{range .Series}}
    <li><a href="{{$.WebPrefix}}/series/{{.ID}}"><i class="fas fa-book-bookmark"></i> {{.Name}}</a></li>
{{end}}
</ul>
{{if or (gt .Page 0) .HasMore}}
<div class="pagination">
    {{if gt .Page 0}}<a href="{{.CurrentPath}}?prefix={{.Prefix}}&page={{.PrevPage}}"><i class="fas fa-arrow-left"></i> Previous</a>{{end}}
    {{if .HasMore}}<a href="{{.CurrentPath}}?prefix={{.Prefix}}&page={{.NextPage}}">Next <i class="fas fa-arrow-right"></i></a>{{end}}
</div>
{{end}}
{{else}}
<p style="text-align: center; color: var(--gray);">No series found.</p>
{{end}}
{{end}}`,

	"catalogs": `{{define "content"}}
<h2><i class="fas fa-folder-tree"></i> {{.Title}}</h2>
{{if .Items}}
<ul class="item-list">
{{range .Items}}
    {{if .IsFolder}}
    <li><a href="{{$.WebPrefix}}/catalogs/{{.ID}}"><i class="fas fa-folder"></i> {{.Name}}</a></li>
    {{else}}
    <li style="display: flex; justify-content: space-between; align-items: center; padding: 15px;">
        <span><i class="fas fa-file" style="color: var(--primary); margin-right: 12px;"></i> {{.Name}}</span>
        <span style="display: flex; gap: 10px; align-items: center;">
            <span style="color: var(--gray); font-size: 0.85rem;">{{.Book.Format}} · {{.Book.Size}}</span>
            <a href="{{$.OPDSPrefix}}/book/{{.Book.ID}}/download" class="btn btn-primary" style="padding: 8px 14px;"><i class="fas fa-download"></i></a>
        </span>
    </li>
    {{end}}
{{end}}
</ul>
{{if or (gt .Page 0) .HasMore}}
<div class="pagination">
    {{if gt .Page 0}}<a href="{{.CurrentPath}}?page={{.PrevPage}}"><i class="fas fa-arrow-left"></i> Previous</a>{{end}}
    {{if .HasMore}}<a href="{{.CurrentPath}}?page={{.NextPage}}">Next <i class="fas fa-arrow-right"></i></a>{{end}}
</div>
{{end}}
{{else}}
<p style="text-align: center; color: var(--gray);">No items found.</p>
{{end}}
{{end}}`,

	"languages": `{{define "content"}}
<h2><i class="fas fa-globe"></i> Languages</h2>
{{if .Languages}}
<div class="sections-grid">
{{range .Languages}}
    <a href="{{$.WebPrefix}}/languages/{{.Code}}" class="section-card">
        <i class="fas fa-language"></i>
        <span style="flex: 1;">{{.Name}}</span>
        <span style="color: var(--gray); font-size: 0.85rem;">{{.Count}}</span>
    </a>
{{end}}
</div>
{{else}}
<p style="text-align: center; color: var(--gray);">No languages found.</p>
{{end}}
{{end}}`,

	"error": `{{define "content"}}
<h2><i class="fas fa-exclamation-triangle" style="color: var(--danger);"></i> Error</h2>
<p style="color: var(--danger); padding: 20px; background: #fef2f2; border-radius: 10px;">{{.Error}}</p>
<a href="{{.WebPrefix}}/" class="btn btn-primary" style="margin-top: 20px;"><i class="fas fa-home"></i> Back to Home</a>
{{end}}`,
}
