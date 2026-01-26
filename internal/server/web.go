package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/bodgit/sevenzip"
	"github.com/dhowden/tag"
	"github.com/go-chi/chi/v5"
	"github.com/sopds/sopds-go/internal/converter"
	"github.com/sopds/sopds-go/internal/database"
	"github.com/sopds/sopds-go/internal/i18n"
	"github.com/sopds/sopds-go/internal/infrastructure/persistence"
)

// Cached ebook-convert availability check
var (
	ebookConvertAvailable bool
	ebookConvertChecked   bool
	ebookConvertCheckMu   sync.Once
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

// --- Internationalization (i18n) ---
// Translations are now in internal/i18n/locales/*.yaml
// To add a new language:
// 1. Copy locales/en.yaml to locales/XX.yaml
// 2. Translate all values
// 3. Add the code to supportedLanguages in i18n/i18n.go

// getLang extracts language preference from request (cookie only for UI)
func getLang(r *http.Request) string {
	if cookie, err := r.Cookie("lang"); err == nil && i18n.IsValidLang(cookie.Value) {
		return cookie.Value
	}
	return i18n.DefaultLang
}

// Template data structures
type PageData struct {
	Title       string
	SiteTitle   string
	WebPrefix   string
	OPDSPrefix  string
	Query       string // Title search query (q=)
	AuthorQuery string // Author search query (author=)
	Prefix      string // 1, 2, or 3 char prefix for drilling down
	Page        int
	PageSize    int
	HasMore     bool
	PrevPage    int
	NextPage    int
	CurrentPath string
	HasEPUB     bool
	HasMOBI     bool
	// Search scope - if set, search only within this context
	ScopeAuthorID  int64
	ScopeGenreID   int64
	ScopeSeriesID  int64
	ScopeCatalogID int64
	ScopeLang      string // Scope to specific language (exact match)
	ScopeName      string // Human-readable scope name for display
	IncludeDesc    bool   // Include description in search
	// i18n
	Lang      string          // Current language code
	Languages []i18n.Language // Available languages for switcher
	// Auth info
	Auth AuthInfo // User authentication state
}

// Available page sizes
var pageSizes = []int{10, 50, 100, 200, 0} // 0 means "all"
const defaultPageSize = 50

// newPageData creates PageData with common fields including i18n
func (s *Server) newPageData(r *http.Request, title string) PageData {
	// UI language is set via cookie only (not URL param, which is for book filtering)
	lang := getLang(r)
	return PageData{
		Title:      title,
		SiteTitle:  s.config.Site.Title,
		WebPrefix:  s.config.Server.WebPrefix,
		OPDSPrefix: s.config.Server.OPDSPrefix,
		HasEPUB:    true, // Internal converter always available
		HasMOBI:    checkEbookConvert(s.config.Converters.FB2ToMOBI),
		Lang:       lang,
		Languages:  i18n.GetSupportedLanguages(),
		Auth:       GetAuthInfo(r),
	}
}

// addI18n adds language fields to PageData (for handlers that don't use newPageData)
func (s *Server) addI18n(pd *PageData, r *http.Request) {
	pd.Lang = getLang(r)
	pd.Languages = i18n.GetSupportedLanguages()
	pd.Auth = GetAuthInfo(r)
}

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

type LangOption struct {
	Code string
	Name string
}

type BooksData struct {
	PageData
	Books      []BookView
	TotalCount int
	// Filter options (from entire scope, not just current page)
	FilterLangs []LangOption // Available languages for filtering
	FirstNames  []string
	LastNames   []string
	Genres      []LinkedItem
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
	OnBookshelf    bool  // Whether the book is on user's bookshelf
	// Audiobook fields
	IsAudiobook     bool
	Duration        string // formatted "3h 45m"
	DurationSeconds int
	TrackCount      int
	Narrators       []LinkedItem
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

	info, _ := s.svc.GetDBInfo(ctx, false)
	var newBooks int64
	if newInfo, err := s.svc.GetNewInfo(ctx, 7); err == nil && newInfo != nil {
		newBooks = newInfo.NewBooks
	}

	data := MainMenuData{
		PageData: s.newPageData(r, "Library"),
		Stats:    info,
		NewBooks: newBooks,
	}

	s.renderTemplate(w, "main", data)
}

func (s *Server) handleWebSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	titleQuery := r.URL.Query().Get("q")
	authorQuery := r.URL.Query().Get("author")
	page := getPage(r)
	pageSize := getPageSize(r)

	// Parse filter parameters (pattern matching)
	filterLang := r.URL.Query().Get("lang")
	filterFirstName := r.URL.Query().Get("fname")
	filterLastName := r.URL.Query().Get("lname")
	filterGenreStr := r.URL.Query().Get("genre")
	var filterGenre int64
	if filterGenreStr != "" {
		filterGenre, _ = strconv.ParseInt(filterGenreStr, 10, 64)
	}

	// Pattern filters for ILIKE matching
	langPattern := r.URL.Query().Get("lang_pattern")
	genrePattern := r.URL.Query().Get("genre_pattern")
	seriesPattern := r.URL.Query().Get("series_pattern")

	// Parse scope parameters (for scoped search within current context)
	authorIDStr := r.URL.Query().Get("author_id")
	genreIDStr := r.URL.Query().Get("genre_id")
	seriesIDStr := r.URL.Query().Get("series_id")
	catalogIDStr := r.URL.Query().Get("catalog_id")
	var authorID, genreID, seriesID, catalogID int64
	if authorIDStr != "" {
		authorID, _ = strconv.ParseInt(authorIDStr, 10, 64)
	}
	if genreIDStr != "" {
		genreID, _ = strconv.ParseInt(genreIDStr, 10, 64)
	}
	if seriesIDStr != "" {
		seriesID, _ = strconv.ParseInt(seriesIDStr, 10, 64)
	}
	if catalogIDStr != "" {
		catalogID, _ = strconv.ParseInt(catalogIDStr, 10, 64)
	}

	// Parse search options
	includeDesc := r.URL.Query().Get("desc") == "1"

	// If no search criteria at all, show empty search page
	if titleQuery == "" && authorQuery == "" && langPattern == "" && genrePattern == "" && seriesPattern == "" {
		data := PageData{
			Title:      i18n.T(getLang(r), "search.books"),
			SiteTitle:  s.config.Site.Title,
			WebPrefix:  s.config.Server.WebPrefix,
			OPDSPrefix: s.config.Server.OPDSPrefix,
		}
		s.addI18n(&data, r)
		s.renderTemplate(w, "search", data)
		return
	}

	// Build search options
	opts := persistence.SearchOptions{
		TitleQuery:        titleQuery,
		AuthorQuery:       authorQuery,
		IncludeAnnotation: includeDesc,
		AuthorID:          authorID,
		GenreID:           genreID,
		SeriesID:          seriesID,
		CatalogID:         catalogID,
		Lang:              filterLang,
		LangPattern:       langPattern,
		GenrePattern:      genrePattern,
		SeriesPattern:     seriesPattern,
		FirstNameFilter:   filterFirstName,
		LastNameFilter:    filterLastName,
	}
	// Use genre filter if set (overrides scope)
	if filterGenre > 0 && genreID == 0 {
		opts.GenreID = filterGenre
	}

	// For "all", use large limit; otherwise use pageSize
	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)
	books, err := s.svc.SearchBooks(ctx, opts, pagination)
	if err != nil {
		s.renderError(w, "Search failed", err)
		return
	}

	// Convert books to view with bookshelf status
	user := s.getWebUser(r)
	bookViews := s.booksToViewForUser(ctx, books, user)

	// Get filter options for the SCOPE (search query without filters)
	scopeOpts := persistence.SearchOptions{
		TitleQuery:        titleQuery,
		AuthorQuery:       authorQuery,
		IncludeAnnotation: includeDesc,
		AuthorID:          authorID,
		GenreID:           genreID, // scope genre, not filter
		SeriesID:          seriesID,
		CatalogID:         catalogID,
		LangPattern:       langPattern,
		GenrePattern:      genrePattern,
		SeriesPattern:     seriesPattern,
	}
	filterOpts, _ := s.svc.GetFilterOptions(ctx, scopeOpts)

	// Look up genre name if filter is active
	var filterGenreName string
	if filterGenre > 0 {
		if genre, err := s.svc.GetGenre(ctx, filterGenre); err == nil {
			filterGenreName = genre.Subsection
			if filterGenreName == "" {
				filterGenreName = genre.Genre
			}
		}
	}

	hasMore := pageSize > 0 && pagination.TotalCount > int64(pagination.Offset()+len(books))

	// Build search description for title
	var searchParts []string
	if titleQuery != "" {
		searchParts = append(searchParts, titleQuery)
	}
	if authorQuery != "" {
		searchParts = append(searchParts, "author:"+authorQuery)
	}
	if langPattern != "" {
		searchParts = append(searchParts, "lang:"+langPattern)
	}
	if genrePattern != "" {
		searchParts = append(searchParts, "genre:"+genrePattern)
	}
	if seriesPattern != "" {
		searchParts = append(searchParts, "series:"+seriesPattern)
	}
	searchDesc := strings.Join(searchParts, " + ")
	if searchDesc == "" {
		searchDesc = "all books"
	}

	pd := s.newPageData(r, fmt.Sprintf("%s: %s", i18n.T(getLang(r), "main.search"), searchDesc))
	pd.Query = titleQuery
	pd.AuthorQuery = authorQuery
	pd.Page = page
	pd.PageSize = pageSize
	pd.HasMore = hasMore
	pd.PrevPage = page - 1
	pd.NextPage = page + 1
	pd.CurrentPath = s.config.Server.WebPrefix + "/search"

	// Build filter options with proper types
	var langOptions []LangOption
	var firstNames, lastNames []string
	var genres []LinkedItem
	if filterOpts != nil {
		langOptions = langsToOptions(filterOpts.Languages)
		firstNames = filterOpts.FirstNames
		lastNames = filterOpts.LastNames
		genres = genresToLinkedItems(filterOpts.GenreIDs, filterOpts.GenreNames)
	}

	data := BooksData{
		PageData:        pd,
		Books:           bookViews,
		TotalCount:      int(pagination.TotalCount),
		FilterLangs:     langOptions,
		FirstNames:      firstNames,
		LastNames:       lastNames,
		Genres:          genres,
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
	lang := getLang(r)

	// First level: show 1-char prefixes
	if prefix == "" {
		prefixes := s.getAuthorPrefixes(ctx, "", 1)
		pd := s.newPageData(r, i18n.T(lang, "authors.title"))
		data := AuthorsData{
			PageData: pd,
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
		pd := s.newPageData(r, fmt.Sprintf("%s: %s", i18n.T(lang, "authors.title"), prefix))
		pd.Prefix = prefix
		data := AuthorsData{
			PageData: pd,
			Prefixes: prefixes,
			IsIndex:  true,
		}
		s.renderTemplate(w, "authors_index", data)
		return
	}

	// Show actual authors list
	pagination := database.NewPagination(page, 100)
	authors, err := s.svc.GetAuthorsByPrefix(ctx, prefix, pagination)
	if err != nil {
		s.renderError(w, "Failed to get authors", err)
		return
	}

	var authorViews []AuthorView
	for _, a := range authors {
		authorViews = append(authorViews, AuthorView{ID: a.ID, Name: a.FullName()})
	}

	pd := s.newPageData(r, fmt.Sprintf("%s: %s", i18n.T(lang, "authors.title"), prefix))
	pd.Prefix = prefix
	pd.Page = page
	pd.HasMore = len(authors) >= 100
	pd.PrevPage = page - 1
	pd.NextPage = page + 1
	pd.CurrentPath = s.config.Server.WebPrefix + "/authors"
	data := AuthorsData{
		PageData: pd,
		Authors:  authorViews,
		IsIndex:  false,
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

	// Parse filter parameters
	filterLang := r.URL.Query().Get("lang")
	filterGenreStr := r.URL.Query().Get("genre")
	var filterGenre int64
	if filterGenreStr != "" {
		filterGenre, _ = strconv.ParseInt(filterGenreStr, 10, 64)
	}

	// Build search options with author scope
	opts := persistence.SearchOptions{
		AuthorID: authorID,
		Lang:     filterLang,
		GenreID:  filterGenre,
	}

	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.svc.SearchBooks(ctx, opts, pagination)
	if err != nil {
		s.renderError(w, "Failed to get books", err)
		return
	}

	authorName := "Author"
	if len(books) > 0 {
		if authors, err := s.svc.GetBookAuthors(ctx, books[0].ID); err == nil {
			for _, a := range authors {
				if a.ID == authorID {
					authorName = a.FullName()
					break
				}
			}
		}
	}

	hasMore := pageSize > 0 && pagination.TotalCount > int64(pagination.Offset()+len(books))
	user := s.getWebUser(r)
	bookViews := s.booksToViewForUser(ctx, books, user)

	// Get filter options for the SCOPE (author only, without filters)
	scopeOpts := persistence.SearchOptions{AuthorID: authorID}
	filterOpts, _ := s.svc.GetFilterOptions(ctx, scopeOpts)

	// Get filter genre name
	filterGenreName := ""
	if filterGenre > 0 {
		if g, err := s.svc.GetGenre(ctx, filterGenre); err == nil {
			filterGenreName = g.Subsection
			if filterGenreName == "" {
				filterGenreName = g.Genre
			}
		}
	}

	pd := PageData{
		Title:         authorName,
		SiteTitle:     s.config.Site.Title,
		WebPrefix:     s.config.Server.WebPrefix,
		OPDSPrefix:    s.config.Server.OPDSPrefix,
		Page:          page,
		PageSize:      pageSize,
		HasMore:       hasMore,
		PrevPage:      page - 1,
		NextPage:      page + 1,
		CurrentPath:   fmt.Sprintf("%s/authors/%d", s.config.Server.WebPrefix, authorID),
		HasEPUB:       true,
		HasMOBI:       checkEbookConvert(s.config.Converters.FB2ToMOBI),
		ScopeAuthorID: authorID,
		ScopeName:     authorName,
	}
	s.addI18n(&pd, r)

	// Build filter options with proper types
	var langOptions []LangOption
	var genres []LinkedItem
	if filterOpts != nil {
		langOptions = langsToOptions(filterOpts.Languages)
		genres = genresToLinkedItems(filterOpts.GenreIDs, filterOpts.GenreNames)
	}

	data := BooksData{
		PageData:        pd,
		Books:           bookViews,
		TotalCount:      int(pagination.TotalCount),
		FilterLangs:     langOptions,
		Genres:          genres,
		FilterLang:      filterLang,
		FilterGenre:     filterGenre,
		FilterGenreName: filterGenreName,
	}

	s.renderTemplate(w, "books", data)
}

func (s *Server) handleWebGenres(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	section := r.URL.Query().Get("section")
	lang := getLang(r)

	if section == "" {
		sections, err := s.svc.GetGenreSections(ctx)
		if err != nil {
			s.renderError(w, "Failed to get genres", err)
			return
		}

		pd := PageData{
			Title:      i18n.T(lang, "genres.title"),
			SiteTitle:  s.config.Site.Title,
			WebPrefix:  s.config.Server.WebPrefix,
			OPDSPrefix: s.config.Server.OPDSPrefix,
		}
		s.addI18n(&pd, r)
		data := GenresData{
			PageData: pd,
			Sections: sections,
		}
		s.renderTemplate(w, "genres_index", data)
		return
	}

	genres, err := s.svc.GetGenresInSection(ctx, section)
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

	pd := PageData{
		Title:      section,
		SiteTitle:  s.config.Site.Title,
		WebPrefix:  s.config.Server.WebPrefix,
		OPDSPrefix: s.config.Server.OPDSPrefix,
	}
	s.addI18n(&pd, r)
	data := GenresData{
		PageData: pd,
		Genres:   genreViews,
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

	// Parse filter parameters
	filterLang := r.URL.Query().Get("lang")
	filterFirstName := r.URL.Query().Get("fname")
	filterLastName := r.URL.Query().Get("lname")

	// Get genre name
	genreName := "Genre"
	if genre, err := s.svc.GetGenre(ctx, genreID); err == nil {
		genreName = genre.Subsection
		if genreName == "" {
			genreName = genre.Genre
		}
	}

	// Build search options with genre scope + filters
	opts := persistence.SearchOptions{
		GenreID:         genreID,
		Lang:            filterLang,
		FirstNameFilter: filterFirstName,
		LastNameFilter:  filterLastName,
	}

	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.svc.SearchBooks(ctx, opts, pagination)
	if err != nil {
		s.renderError(w, "Failed to get books", err)
		return
	}

	// Get filter options for the SCOPE (not filtered results) to show all available options
	scopeOpts := persistence.SearchOptions{GenreID: genreID}
	filterOpts, _ := s.svc.GetFilterOptions(ctx, scopeOpts)

	hasMore := pageSize > 0 && pagination.TotalCount > int64(pagination.Offset()+len(books))
	user := s.getWebUser(r)
	bookViews := s.booksToViewForUser(ctx, books, user)

	// Convert language codes to LangOption with names
	var langOptions []LangOption
	if filterOpts != nil {
		for _, code := range filterOpts.Languages {
			langOptions = append(langOptions, LangOption{Code: code, Name: getLanguageName(code)})
		}
	}

	pd := PageData{
		Title:        genreName,
		SiteTitle:    s.config.Site.Title,
		WebPrefix:    s.config.Server.WebPrefix,
		OPDSPrefix:   s.config.Server.OPDSPrefix,
		Page:         page,
		PageSize:     pageSize,
		HasMore:      hasMore,
		PrevPage:     page - 1,
		NextPage:     page + 1,
		CurrentPath:  fmt.Sprintf("%s/genres/%d", s.config.Server.WebPrefix, genreID),
		HasEPUB:      true,
		HasMOBI:      checkEbookConvert(s.config.Converters.FB2ToMOBI),
		ScopeGenreID: genreID,
		ScopeName:    genreName,
	}
	s.addI18n(&pd, r)
	data := BooksData{
		PageData:        pd,
		Books:           bookViews,
		TotalCount:      int(pagination.TotalCount),
		FilterLangs:     langOptions,
		FirstNames:      filterOpts.FirstNames,
		LastNames:       filterOpts.LastNames,
		FilterLang:      filterLang,
		FilterFirstName: filterFirstName,
		FilterLastName:  filterLastName,
	}

	s.renderTemplate(w, "books", data)
}

func (s *Server) handleWebSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	prefix := r.URL.Query().Get("prefix")
	page := getPage(r)
	lang := getLang(r)

	if prefix == "" {
		prefixes := s.getSeriesPrefixes(ctx, "", 1)
		pd := PageData{
			Title:      i18n.T(lang, "series.title"),
			SiteTitle:  s.config.Site.Title,
			WebPrefix:  s.config.Server.WebPrefix,
			OPDSPrefix: s.config.Server.OPDSPrefix,
		}
		s.addI18n(&pd, r)
		data := SeriesData{
			PageData: pd,
			Prefixes: prefixes,
			IsIndex:  true,
		}
		s.renderTemplate(w, "series_index", data)
		return
	}

	count := s.countSeriesByPrefix(ctx, prefix)

	if count > 100 && len(prefix) < 3 {
		prefixes := s.getSeriesPrefixes(ctx, prefix, len(prefix)+1)
		pd := PageData{
			Title:      fmt.Sprintf("%s: %s", i18n.T(lang, "series.title"), prefix),
			SiteTitle:  s.config.Site.Title,
			WebPrefix:  s.config.Server.WebPrefix,
			OPDSPrefix: s.config.Server.OPDSPrefix,
			Prefix:     prefix,
		}
		s.addI18n(&pd, r)
		data := SeriesData{
			PageData: pd,
			Prefixes: prefixes,
			IsIndex:  true,
		}
		s.renderTemplate(w, "series_index", data)
		return
	}

	pagination := database.NewPagination(page, 100)
	seriesList, err := s.svc.GetSeriesByPrefix(ctx, prefix, pagination)
	if err != nil {
		s.renderError(w, "Failed to get series", err)
		return
	}

	var seriesViews []SeriesView
	for _, ser := range seriesList {
		seriesViews = append(seriesViews, SeriesView{ID: ser.ID, Name: ser.Name})
	}

	pd := PageData{
		Title:       fmt.Sprintf("%s: %s", i18n.T(lang, "series.title"), prefix),
		SiteTitle:   s.config.Site.Title,
		WebPrefix:   s.config.Server.WebPrefix,
		OPDSPrefix:  s.config.Server.OPDSPrefix,
		Prefix:      prefix,
		Page:        page,
		HasMore:     len(seriesList) >= 100,
		PrevPage:    page - 1,
		NextPage:    page + 1,
		CurrentPath: s.config.Server.WebPrefix + "/series",
	}
	s.addI18n(&pd, r)
	data := SeriesData{
		PageData: pd,
		Series:   seriesViews,
		IsIndex:  false,
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

	// Parse filter parameters
	filterLang := r.URL.Query().Get("lang")
	filterFirstName := r.URL.Query().Get("fname")
	filterLastName := r.URL.Query().Get("lname")
	filterGenreStr := r.URL.Query().Get("genre")
	var filterGenre int64
	if filterGenreStr != "" {
		filterGenre, _ = strconv.ParseInt(filterGenreStr, 10, 64)
	}

	// Get series name
	seriesName := "Series"
	if ser, err := s.svc.GetSeries(ctx, seriesID); err == nil {
		seriesName = ser.Name
	}

	// Build search options with series scope
	opts := persistence.SearchOptions{
		SeriesID:        seriesID,
		Lang:            filterLang,
		FirstNameFilter: filterFirstName,
		LastNameFilter:  filterLastName,
		GenreID:         filterGenre,
	}

	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.svc.SearchBooks(ctx, opts, pagination)
	if err != nil {
		s.renderError(w, "Failed to get books", err)
		return
	}

	hasMore := pageSize > 0 && pagination.TotalCount > int64(pagination.Offset()+len(books))
	user := s.getWebUser(r)
	bookViews := s.booksToViewForUser(ctx, books, user)

	// Get filter options for the SCOPE (series only, without filters)
	scopeOpts := persistence.SearchOptions{SeriesID: seriesID}
	filterOpts, _ := s.svc.GetFilterOptions(ctx, scopeOpts)

	// Get filter genre name
	filterGenreName := ""
	if filterGenre > 0 {
		if g, err := s.svc.GetGenre(ctx, filterGenre); err == nil {
			filterGenreName = g.Subsection
			if filterGenreName == "" {
				filterGenreName = g.Genre
			}
		}
	}

	pd := PageData{
		Title:         seriesName,
		SiteTitle:     s.config.Site.Title,
		WebPrefix:     s.config.Server.WebPrefix,
		OPDSPrefix:    s.config.Server.OPDSPrefix,
		Page:          page,
		PageSize:      pageSize,
		HasMore:       hasMore,
		PrevPage:      page - 1,
		NextPage:      page + 1,
		CurrentPath:   fmt.Sprintf("%s/series/%d", s.config.Server.WebPrefix, seriesID),
		HasEPUB:       true,
		HasMOBI:       checkEbookConvert(s.config.Converters.FB2ToMOBI),
		ScopeSeriesID: seriesID,
		ScopeName:     seriesName,
	}
	s.addI18n(&pd, r)

	// Build filter options with proper types
	var langOptions []LangOption
	var firstNames, lastNames []string
	var genres []LinkedItem
	if filterOpts != nil {
		langOptions = langsToOptions(filterOpts.Languages)
		firstNames = filterOpts.FirstNames
		lastNames = filterOpts.LastNames
		genres = genresToLinkedItems(filterOpts.GenreIDs, filterOpts.GenreNames)
	}

	data := BooksData{
		PageData:        pd,
		Books:           bookViews,
		TotalCount:      int(pagination.TotalCount),
		FilterLangs:     langOptions,
		FirstNames:      firstNames,
		LastNames:       lastNames,
		Genres:          genres,
		FilterLang:      filterLang,
		FilterFirstName: filterFirstName,
		FilterLastName:  filterLastName,
		FilterGenre:     filterGenre,
		FilterGenreName: filterGenreName,
	}

	s.renderTemplate(w, "books", data)
}

func (s *Server) handleWebNew(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := getLang(r)
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

	// Build search options with new period
	opts := persistence.SearchOptions{
		NewPeriod:       7, // Last 7 days
		Lang:            filterLang,
		FirstNameFilter: filterFirstName,
		LastNameFilter:  filterLastName,
		GenreID:         filterGenre,
	}

	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.svc.SearchBooks(ctx, opts, pagination)
	if err != nil {
		s.renderError(w, "Failed to get new books", err)
		return
	}

	hasMore := pageSize > 0 && pagination.TotalCount > int64(pagination.Offset()+len(books))
	user := s.getWebUser(r)
	bookViews := s.booksToViewForUser(ctx, books, user)

	// Get filter options for the SCOPE (new period only, without filters)
	scopeOpts := persistence.SearchOptions{NewPeriod: 7}
	filterOpts, _ := s.svc.GetFilterOptions(ctx, scopeOpts)

	// Get filter genre name
	filterGenreName := ""
	if filterGenre > 0 {
		if g, err := s.svc.GetGenre(ctx, filterGenre); err == nil {
			filterGenreName = g.Subsection
			if filterGenreName == "" {
				filterGenreName = g.Genre
			}
		}
	}

	pd := PageData{
		Title:       i18n.T(lang, "main.newbooks"),
		SiteTitle:   s.config.Site.Title,
		WebPrefix:   s.config.Server.WebPrefix,
		OPDSPrefix:  s.config.Server.OPDSPrefix,
		Page:        page,
		PageSize:    pageSize,
		HasMore:     hasMore,
		PrevPage:    page - 1,
		NextPage:    page + 1,
		CurrentPath: s.config.Server.WebPrefix + "/new",
		HasEPUB:     true,
		HasMOBI:     checkEbookConvert(s.config.Converters.FB2ToMOBI),
	}
	s.addI18n(&pd, r)

	// Build filter options with proper types
	var langOptions []LangOption
	var firstNames, lastNames []string
	var genres []LinkedItem
	if filterOpts != nil {
		langOptions = langsToOptions(filterOpts.Languages)
		firstNames = filterOpts.FirstNames
		lastNames = filterOpts.LastNames
		genres = genresToLinkedItems(filterOpts.GenreIDs, filterOpts.GenreNames)
	}

	data := BooksData{
		PageData:        pd,
		Books:           bookViews,
		TotalCount:      int(pagination.TotalCount),
		FilterLangs:     langOptions,
		FirstNames:      firstNames,
		LastNames:       lastNames,
		Genres:          genres,
		FilterLang:      filterLang,
		FilterFirstName: filterFirstName,
		FilterLastName:  filterLastName,
		FilterGenre:     filterGenre,
		FilterGenreName: filterGenreName,
	}

	s.renderTemplate(w, "books", data)
}

func (s *Server) handleWebAudio(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := getLang(r)
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

	// Build search options for audiobooks only
	opts := persistence.SearchOptions{
		AudioOnly:       true,
		Lang:            filterLang,
		FirstNameFilter: filterFirstName,
		LastNameFilter:  filterLastName,
		GenreID:         filterGenre,
	}

	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.svc.SearchBooks(ctx, opts, pagination)
	if err != nil {
		s.renderError(w, "Failed to get audiobooks", err)
		return
	}

	hasMore := pageSize > 0 && pagination.TotalCount > int64(pagination.Offset()+len(books))
	user := s.getWebUser(r)
	bookViews := s.booksToViewForUser(ctx, books, user)

	// Get filter options for the SCOPE (audiobooks only, without filters)
	scopeOpts := persistence.SearchOptions{AudioOnly: true}
	filterOpts, _ := s.svc.GetFilterOptions(ctx, scopeOpts)

	// Get filter genre name
	filterGenreName := ""
	if filterGenre > 0 {
		if g, err := s.svc.GetGenre(ctx, filterGenre); err == nil {
			filterGenreName = g.Subsection
			if filterGenreName == "" {
				filterGenreName = g.Genre
			}
		}
	}

	pd := PageData{
		Title:       i18n.T(lang, "main.audiobooks"),
		SiteTitle:   s.config.Site.Title,
		WebPrefix:   s.config.Server.WebPrefix,
		OPDSPrefix:  s.config.Server.OPDSPrefix,
		Page:        page,
		PageSize:    pageSize,
		HasMore:     hasMore,
		PrevPage:    page - 1,
		NextPage:    page + 1,
		CurrentPath: s.config.Server.WebPrefix + "/audio",
		HasEPUB:     true,
		HasMOBI:     checkEbookConvert(s.config.Converters.FB2ToMOBI),
	}
	s.addI18n(&pd, r)

	// Build filter options with proper types
	var langOptions []LangOption
	var firstNames, lastNames []string
	var genres []LinkedItem
	if filterOpts != nil {
		langOptions = langsToOptions(filterOpts.Languages)
		firstNames = filterOpts.FirstNames
		lastNames = filterOpts.LastNames
		genres = genresToLinkedItems(filterOpts.GenreIDs, filterOpts.GenreNames)
	}

	data := BooksData{
		PageData:        pd,
		Books:           bookViews,
		TotalCount:      int(pagination.TotalCount),
		FilterLangs:     langOptions,
		FirstNames:      firstNames,
		LastNames:       lastNames,
		Genres:          genres,
		FilterLang:      filterLang,
		FilterFirstName: filterFirstName,
		FilterLastName:  filterLastName,
		FilterGenre:     filterGenre,
		FilterGenreName: filterGenreName,
	}

	s.renderTemplate(w, "books", data)
}

// AudiobookStructure for parsing chapters JSON
type AudiobookStructure struct {
	Type   string          `json:"type"`
	Parts  []AudiobookPart `json:"parts,omitempty"`
	Tracks []AudioTrack    `json:"tracks,omitempty"`
}

type AudiobookPart struct {
	Name     string       `json:"name"`
	Duration int          `json:"duration"`
	Tracks   []AudioTrack `json:"tracks"`
}

type AudioTrack struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Duration int    `json:"duration"`
	Size     int64  `json:"size"`
}

// AudioDetailData holds data for the audiobook detail page
type AudioDetailData struct {
	PageData
	Book      BookView
	Structure *AudiobookStructure
	Authors   []database.Author
}

func (s *Server) handleWebAudioDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.renderError(w, "Invalid audiobook ID", err)
		return
	}

	book, err := s.svc.GetBook(ctx, id)
	if err != nil || book == nil {
		s.renderError(w, "Audiobook not found", err)
		return
	}

	if !book.IsAudiobook {
		http.Redirect(w, r, s.config.Server.WebPrefix+"/", http.StatusFound)
		return
	}

	// Parse chapters JSON
	var structure *AudiobookStructure
	if book.Chapters != "" {
		structure = &AudiobookStructure{}
		if err := json.Unmarshal([]byte(book.Chapters), structure); err != nil {
			log.Printf("Failed to parse chapters JSON for book %d: %v", id, err)
			structure = nil
		}
	}

	// Get authors and narrators
	authors, _ := s.svc.GetBookAuthors(ctx, id)
	var authorLinks []LinkedItem
	for _, a := range authors {
		authorLinks = append(authorLinks, LinkedItem{ID: a.ID, Name: a.FullName()})
	}

	var narratorLinks []LinkedItem
	if narrators, err := s.svc.GetBookNarrators(ctx, id); err == nil {
		for _, n := range narrators {
			narratorLinks = append(narratorLinks, LinkedItem{ID: n.ID, Name: n.FullName()})
		}
	}

	// Check bookshelf status
	user := s.getWebUser(r)
	bookshelfIDs, _ := s.svc.GetBookShelfIDs(ctx, user)
	onBookshelf := bookshelfIDs != nil && bookshelfIDs[book.ID]

	// Build book view
	bookView := BookView{
		ID:              book.ID,
		Title:           book.Title,
		Authors:         authorLinks,
		Lang:            book.Lang,
		LangName:        getLanguageName(book.Lang),
		Format:          strings.ToUpper(book.Format),
		Size:            formatSize(book.Filesize),
		Annotation:      book.Annotation,
		HasCover:        true, // Try to load cover from audio file, template has onerror fallback
		OnBookshelf:     onBookshelf,
		IsAudiobook:     book.IsAudiobook,
		Duration:        formatDuration(book.DurationSeconds),
		DurationSeconds: book.DurationSeconds,
		TrackCount:      book.TrackCount,
		Narrators:       narratorLinks,
	}

	pd := s.newPageData(r, book.Title)

	data := AudioDetailData{
		PageData:  pd,
		Book:      bookView,
		Structure: structure,
		Authors:   authors,
	}

	s.renderTemplate(w, "audiodetail", data)
}

// handleAudioTrackDownload serves individual audio files from archive
func (s *Server) handleAudioTrackDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid audiobook ID", http.StatusBadRequest)
		return
	}

	// Get the file path from query parameter
	trackPath := r.URL.Query().Get("file")
	if trackPath == "" {
		http.Error(w, "Missing file parameter", http.StatusBadRequest)
		return
	}

	book, err := s.svc.GetBook(ctx, id)
	if err != nil {
		http.Error(w, "Audiobook not found", http.StatusNotFound)
		return
	}

	if !book.IsAudiobook {
		http.Error(w, "Not an audiobook", http.StatusBadRequest)
		return
	}

	// Construct archive path
	// Handle bug where path == filename (should be "." for files in library root)
	bookPath := book.Path
	if bookPath == book.Filename {
		bookPath = "."
	}
	archivePath := filepath.Join(s.config.Library.Root, bookPath, book.Filename)
	format := strings.ToLower(book.Format)

	// Determine MIME type from track extension
	trackExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(trackPath), "."))
	mimeType := "application/octet-stream"
	switch trackExt {
	case "mp3":
		mimeType = "audio/mpeg"
	case "m4b", "m4a", "aac":
		mimeType = "audio/mp4"
	case "flac":
		mimeType = "audio/flac"
	case "ogg", "opus":
		mimeType = "audio/ogg"
	case "wav":
		mimeType = "audio/wav"
	}

	trackFilename := filepath.Base(trackPath)

	if format == "zip" {
		s.serveTrackFromZip(w, archivePath, trackPath, trackFilename, mimeType)
	} else if format == "7z" {
		s.serveTrackFrom7z(w, archivePath, trackPath, trackFilename, mimeType)
	} else if format == "folder" {
		// For folder-based audiobooks, construct full path from track path
		// Track path can be: full path, relative to library root, or relative to book folder
		s.serveTrackFromFolder(w, r, book, trackPath, trackFilename, mimeType)
	} else {
		http.Error(w, "Unsupported audiobook format", http.StatusBadRequest)
	}
}

func (s *Server) serveTrackFromZip(w http.ResponseWriter, archivePath, trackPath, filename, mimeType string) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		http.Error(w, "Failed to open archive", http.StatusInternalServerError)
		return
	}
	defer zr.Close()

	// Try multiple matching strategies
	trackBasename := filepath.Base(trackPath)
	for _, f := range zr.File {
		// Exact match
		if f.Name == trackPath {
			s.serveFileFromZip(w, f, filename, mimeType)
			return
		}
		// Suffix match (trackPath is relative path within archive)
		if strings.HasSuffix(f.Name, "/"+trackPath) {
			s.serveFileFromZip(w, f, filename, mimeType)
			return
		}
		// Basename match (trackPath is just filename, search anywhere in archive)
		if trackPath == trackBasename && strings.HasSuffix(f.Name, "/"+trackBasename) {
			s.serveFileFromZip(w, f, filename, mimeType)
			return
		}
	}

	http.Error(w, "File not found in archive", http.StatusNotFound)
}

func (s *Server) serveFileFromZip(w http.ResponseWriter, f *zip.File, filename, mimeType string) {
	rc, err := f.Open()
	if err != nil {
		http.Error(w, "Failed to open file in archive", http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", f.UncompressedSize64))
	io.Copy(w, rc)
}

func (s *Server) serveTrackFrom7z(w http.ResponseWriter, archivePath, trackPath, filename, mimeType string) {
	szr, err := sevenzip.OpenReader(archivePath)
	if err != nil {
		// Archive path might be wrong (path=filename bug), try with just directory
		archiveDir := filepath.Dir(archivePath)
		archiveName := filepath.Base(archivePath)
		if archiveDir != archivePath {
			altPath := filepath.Join(archiveDir, archiveName)
			if altPath != archivePath {
				szr, err = sevenzip.OpenReader(filepath.Join(filepath.Dir(archiveDir), archiveName))
			}
		}
		if err != nil {
			http.Error(w, "Failed to open archive", http.StatusInternalServerError)
			return
		}
	}
	defer szr.Close()

	// Try multiple matching strategies
	trackBasename := filepath.Base(trackPath)
	for _, f := range szr.File {
		// Exact match
		if f.Name == trackPath {
			s.serveFileFrom7z(w, f, filename, mimeType)
			return
		}
		// Suffix match (trackPath is relative path within archive)
		if strings.HasSuffix(f.Name, "/"+trackPath) {
			s.serveFileFrom7z(w, f, filename, mimeType)
			return
		}
		// Basename match (trackPath is just filename, search anywhere in archive)
		if trackPath == trackBasename && strings.HasSuffix(f.Name, "/"+trackBasename) {
			s.serveFileFrom7z(w, f, filename, mimeType)
			return
		}
	}

	http.Error(w, "File not found in archive", http.StatusNotFound)
}

func (s *Server) serveFileFrom7z(w http.ResponseWriter, f *sevenzip.File, filename, mimeType string) {
	rc, err := f.Open()
	if err != nil {
		http.Error(w, "Failed to open file in archive", http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", f.UncompressedSize))
	io.Copy(w, rc)
}

// serveTrackFromFolder serves audio files from folder-based audiobooks (regular files on disk)
func (s *Server) serveTrackFromFolder(w http.ResponseWriter, r *http.Request, book *database.Book, trackPath, filename, mimeType string) {
	// Construct full path from track path
	// Track path can be in different formats:
	// 1. Full filesystem path (starts with /)
	// 2. Path relative to library root (FolderName/file.mp3)
	// 3. Just the filename

	var fullPath string
	if strings.HasPrefix(trackPath, "/") {
		// Already a full path
		fullPath = trackPath
	} else if strings.Contains(trackPath, string(filepath.Separator)) || strings.Contains(trackPath, "/") {
		// Relative path with directory component (likely includes folder name)
		// Construct: library_root / book.Path / trackPath
		fullPath = filepath.Join(s.config.Library.Root, book.Path, trackPath)
	} else {
		// Just a filename - construct full path from book location
		// For folder audiobooks: library_root / book.Path / book.Filename / filename
		fullPath = filepath.Join(s.config.Library.Root, book.Path, book.Filename, trackPath)
	}

	// Clean the path
	cleanPath := filepath.Clean(fullPath)

	// Security check: ensure path is within library root
	if !strings.HasPrefix(cleanPath, s.config.Library.Root) {
		log.Printf("Audio track path security violation: %s (not in %s)", cleanPath, s.config.Library.Root)
		http.Error(w, "Invalid file path", http.StatusBadRequest)
		return
	}

	// Check if file exists
	stat, err := os.Stat(cleanPath)
	if err != nil {
		log.Printf("Audio track not found: %s (from trackPath=%s, book.Path=%s, book.Filename=%s)",
			cleanPath, trackPath, book.Path, book.Filename)
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Open the file
	f, err := os.Open(cleanPath)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// Set headers
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Accept-Ranges", "bytes")

	// Use http.ServeContent which handles range requests properly
	http.ServeContent(w, r, filename, stat.ModTime(), f)
}

// handleAudioTrackCover serves cover art from a specific audio file inside an archive
func (s *Server) handleAudioTrackCover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.servePlaceholderAudioCover(w)
		return
	}

	trackPath := r.URL.Query().Get("file")
	if trackPath == "" {
		s.servePlaceholderAudioCover(w)
		return
	}

	book, err := s.svc.GetBook(ctx, id)
	if err != nil || book == nil {
		s.servePlaceholderAudioCover(w)
		return
	}

	if !book.IsAudiobook {
		s.servePlaceholderAudioCover(w)
		return
	}

	archivePath := filepath.Join(s.config.Library.Root, book.Path, book.Filename)
	format := strings.ToLower(book.Format)

	var coverData []byte
	var coverType string

	if format == "zip" {
		coverData, coverType = s.extractTrackCoverFromZip(archivePath, trackPath)
	} else if format == "7z" {
		coverData, coverType = s.extractTrackCoverFrom7z(archivePath, trackPath)
	} else if format == "folder" {
		coverData, coverType = s.extractTrackCoverFromFolder(book, trackPath)
	}

	if coverData == nil {
		s.servePlaceholderAudioCover(w)
		return
	}

	w.Header().Set("Content-Type", coverType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(coverData)
}

func (s *Server) extractTrackCoverFromZip(archivePath, trackPath string) ([]byte, string) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, ""
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == trackPath || strings.HasSuffix(f.Name, "/"+trackPath) {
			rc, err := f.Open()
			if err != nil {
				return nil, ""
			}
			defer rc.Close()

			// Read into memory for tag parsing
			data, err := io.ReadAll(rc)
			if err != nil {
				return nil, ""
			}

			return s.extractCoverFromAudioData(data)
		}
	}
	return nil, ""
}

func (s *Server) extractTrackCoverFrom7z(archivePath, trackPath string) ([]byte, string) {
	szr, err := sevenzip.OpenReader(archivePath)
	if err != nil {
		return nil, ""
	}
	defer szr.Close()

	for _, f := range szr.File {
		if f.Name == trackPath || strings.HasSuffix(f.Name, "/"+trackPath) {
			rc, err := f.Open()
			if err != nil {
				return nil, ""
			}
			defer rc.Close()

			// Read into memory for tag parsing
			data, err := io.ReadAll(rc)
			if err != nil {
				return nil, ""
			}

			return s.extractCoverFromAudioData(data)
		}
	}
	return nil, ""
}

func (s *Server) extractCoverFromAudioData(data []byte) ([]byte, string) {
	r := bytes.NewReader(data)
	m, err := tag.ReadFrom(r)
	if err != nil {
		return nil, ""
	}

	if pic := m.Picture(); pic != nil && len(pic.Data) > 0 {
		mimeType := pic.MIMEType
		if mimeType == "" {
			// Detect from data
			if len(pic.Data) > 2 && pic.Data[0] == 0xFF && pic.Data[1] == 0xD8 {
				mimeType = "image/jpeg"
			} else if len(pic.Data) > 8 && pic.Data[0] == 0x89 && pic.Data[1] == 0x50 && pic.Data[2] == 0x4E && pic.Data[3] == 0x47 {
				mimeType = "image/png"
			} else {
				mimeType = "image/jpeg"
			}
		}
		return pic.Data, mimeType
	}
	return nil, ""
}

func (s *Server) extractTrackCoverFromFolder(book *database.Book, trackPath string) ([]byte, string) {
	// Construct full path (same logic as serveTrackFromFolder)
	var fullPath string
	if strings.HasPrefix(trackPath, "/") {
		fullPath = trackPath
	} else if strings.Contains(trackPath, string(filepath.Separator)) || strings.Contains(trackPath, "/") {
		fullPath = filepath.Join(s.config.Library.Root, book.Path, trackPath)
	} else {
		fullPath = filepath.Join(s.config.Library.Root, book.Path, book.Filename, trackPath)
	}

	cleanPath := filepath.Clean(fullPath)
	if !strings.HasPrefix(cleanPath, s.config.Library.Root) {
		return nil, ""
	}

	f, err := os.Open(cleanPath)
	if err != nil {
		return nil, ""
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil, ""
	}

	if pic := m.Picture(); pic != nil && len(pic.Data) > 0 {
		mimeType := pic.MIMEType
		if mimeType == "" {
			if len(pic.Data) > 2 && pic.Data[0] == 0xFF && pic.Data[1] == 0xD8 {
				mimeType = "image/jpeg"
			} else if len(pic.Data) > 8 && pic.Data[0] == 0x89 && pic.Data[1] == 0x50 && pic.Data[2] == 0x4E && pic.Data[3] == 0x47 {
				mimeType = "image/png"
			} else {
				mimeType = "image/jpeg"
			}
		}
		return pic.Data, mimeType
	}
	return nil, ""
}

func (s *Server) handleWebBookshelf(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := getLang(r)

	user := s.getWebUser(r)

	page := getPage(r)
	pageSize := getPageSize(r)
	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.svc.GetBookShelf(ctx, user, pagination)
	if err != nil {
		s.renderError(w, "Failed to get bookshelf", err)
		return
	}

	count, _ := s.svc.CountBookShelf(ctx, user)
	hasMore := pageSize > 0 && count > int64(pagination.Offset()+len(books))

	pd := s.newPageData(r, i18n.T(lang, "bookshelf.title"))
	pd.CurrentPath = s.config.Server.WebPrefix + "/bookshelf"
	pd.Page = page
	pd.PageSize = pageSize
	pd.HasMore = hasMore
	pd.PrevPage = page - 1
	pd.NextPage = page + 1
	data := BooksData{
		PageData:   pd,
		Books:      s.booksToViewForUser(ctx, books, user),
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

// langsToOptions converts language codes to LangOption with human-readable names
func langsToOptions(codes []string) []LangOption {
	var opts []LangOption
	for _, code := range codes {
		opts = append(opts, LangOption{Code: code, Name: getLanguageName(code)})
	}
	return opts
}

// genresToLinkedItems converts genre IDs and names to LinkedItem slice
func genresToLinkedItems(ids []int64, names []string) []LinkedItem {
	var items []LinkedItem
	for i := 0; i < len(ids) && i < len(names); i++ {
		items = append(items, LinkedItem{ID: ids[i], Name: names[i]})
	}
	return items
}

func (s *Server) handleWebLanguages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := getLang(r)

	langs, err := s.svc.GetLanguages(ctx)
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

	pd := PageData{
		Title:      i18n.T(lang, "languages.title"),
		SiteTitle:  s.config.Site.Title,
		WebPrefix:  s.config.Server.WebPrefix,
		OPDSPrefix: s.config.Server.OPDSPrefix,
	}
	s.addI18n(&pd, r)
	data := LanguagesData{
		PageData:  pd,
		Languages: langViews,
	}

	s.renderTemplate(w, "languages", data)
}

func (s *Server) handleWebLanguage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uiLang := getLang(r)
	bookLang := chi.URLParam(r, "lang")

	page := getPage(r)
	pageSize := getPageSize(r)

	// Parse filter parameters
	filterFirstName := r.URL.Query().Get("fname")
	filterLastName := r.URL.Query().Get("lname")
	filterGenreStr := r.URL.Query().Get("genre")
	var filterGenre int64
	if filterGenreStr != "" {
		filterGenre, _ = strconv.ParseInt(filterGenreStr, 10, 64)
	}

	// Build search options with language scope
	opts := persistence.SearchOptions{
		Lang:            bookLang,
		FirstNameFilter: filterFirstName,
		LastNameFilter:  filterLastName,
		GenreID:         filterGenre,
	}

	limit := pageSize
	if limit == 0 {
		limit = 10000
	}
	pagination := database.NewPagination(page, limit)

	books, err := s.svc.SearchBooks(ctx, opts, pagination)
	if err != nil {
		s.renderError(w, "Failed to get books", err)
		return
	}

	hasMore := pageSize > 0 && pagination.TotalCount > int64(pagination.Offset()+len(books))
	user := s.getWebUser(r)
	bookshelfIDs, _ := s.svc.GetBookShelfIDs(ctx, user)
	bookViews, filterOpts := s.booksToViewWithFilters(ctx, books, bookshelfIDs)

	// Get filter genre name
	filterGenreName := ""
	if filterGenre > 0 {
		if g, err := s.svc.GetGenre(ctx, filterGenre); err == nil {
			filterGenreName = g.Subsection
			if filterGenreName == "" {
				filterGenreName = g.Genre
			}
		}
	}

	langName := getLanguageName(bookLang)
	pd := PageData{
		Title:       fmt.Sprintf("%s: %s", i18n.T(uiLang, "languages.title"), langName),
		SiteTitle:   s.config.Site.Title,
		WebPrefix:   s.config.Server.WebPrefix,
		OPDSPrefix:  s.config.Server.OPDSPrefix,
		Page:        page,
		PageSize:    pageSize,
		HasMore:     hasMore,
		PrevPage:    page - 1,
		NextPage:    page + 1,
		CurrentPath: fmt.Sprintf("%s/languages/%s", s.config.Server.WebPrefix, bookLang),
		HasEPUB:     true,
		HasMOBI:     checkEbookConvert(s.config.Converters.FB2ToMOBI),
		ScopeLang:   bookLang,
		ScopeName:   langName,
	}
	s.addI18n(&pd, r)
	data := BooksData{
		PageData:        pd,
		Books:           bookViews,
		TotalCount:      int(pagination.TotalCount),
		FilterLang:      bookLang, // Show current language as active filter
		FirstNames:      filterOpts.FirstNames,
		LastNames:       filterOpts.LastNames,
		Genres:          filterOpts.Genres,
		FilterFirstName: filterFirstName,
		FilterLastName:  filterLastName,
		FilterGenre:     filterGenre,
		FilterGenreName: filterGenreName,
	}

	s.renderTemplate(w, "books", data)
}

func (s *Server) handleWebCatalogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := getLang(r)
	page := getPage(r)
	pagination := database.NewPagination(page, 100)

	items, err := s.svc.GetItemsInCatalog(ctx, 0, pagination, false)
	if err != nil {
		s.renderError(w, "Failed to get catalogs", err)
		return
	}

	pd := PageData{
		Title:       i18n.T(lang, "catalogs.title"),
		SiteTitle:   s.config.Site.Title,
		WebPrefix:   s.config.Server.WebPrefix,
		OPDSPrefix:  s.config.Server.OPDSPrefix,
		Page:        page,
		HasMore:     len(items) >= 100,
		PrevPage:    page - 1,
		NextPage:    page + 1,
		CurrentPath: s.config.Server.WebPrefix + "/catalogs",
	}
	s.addI18n(&pd, r)
	data := CatalogsData{
		PageData: pd,
		Items:    s.catalogItemsToView(items),
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

	items, err := s.svc.GetItemsInCatalog(ctx, catID, pagination, false)
	if err != nil {
		s.renderError(w, "Failed to get catalog", err)
		return
	}

	cat, _ := s.svc.GetCatalog(ctx, catID)
	title := "Catalog"
	if cat != nil {
		title = cat.Name
	}

	pd := PageData{
		Title:          title,
		SiteTitle:      s.config.Site.Title,
		WebPrefix:      s.config.Server.WebPrefix,
		OPDSPrefix:     s.config.Server.OPDSPrefix,
		Page:           page,
		HasMore:        len(items) >= 100,
		PrevPage:       page - 1,
		NextPage:       page + 1,
		CurrentPath:    fmt.Sprintf("%s/catalogs/%d", s.config.Server.WebPrefix, catID),
		ScopeCatalogID: catID,
		ScopeName:      title,
	}
	s.addI18n(&pd, r)
	data := CatalogsData{
		PageData: pd,
		Items:    s.catalogItemsToView(items),
	}

	s.renderTemplate(w, "catalogs", data)
}

func (s *Server) handleWebDuplicates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := getLang(r)
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

	books, err := s.svc.GetBookDuplicates(ctx, bookID, pagination)
	if err != nil {
		s.renderError(w, "Failed to get duplicates", err)
		return
	}

	// Get the book title for the page header
	title := i18n.T(lang, "books.duplicates")
	if len(books) > 0 {
		title = fmt.Sprintf("%s: %s", i18n.T(lang, "books.duplicates"), books[0].Title)
	}

	hasMore := pageSize > 0 && pagination.TotalCount > int64(pagination.Offset()+len(books))

	pd := PageData{
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
	}
	s.addI18n(&pd, r)
	user := s.getWebUser(r)
	data := BooksData{
		PageData:   pd,
		Books:      s.booksToViewForUser(ctx, books, user),
		TotalCount: int(pagination.TotalCount),
	}

	s.renderTemplate(w, "books", data)
}

// HelpData contains data for the help page
type HelpData struct {
	PageData
}

func (s *Server) handleWebHelp(w http.ResponseWriter, r *http.Request) {
	pd := s.newPageData(r, "")
	pd.Title = i18n.T(pd.Lang, "help.title")
	data := HelpData{
		PageData: pd,
	}

	s.renderTemplate(w, "help", data)
}

// ReaderData contains data for the book reader page
type ReaderData struct {
	PageData
	BookID    int64
	BookTitle string
	Authors   string
	Content   string
	TOC       []converter.TOCEntry
	Cover     string
	BackURL   string
}

func (s *Server) handleWebReader(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid book ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	book, err := s.svc.GetBook(ctx, id)
	if err != nil || book == nil {
		http.Error(w, "Book not found", http.StatusNotFound)
		return
	}

	// Read book content
	var bookData []byte
	format := strings.ToLower(book.Format)

	if strings.Contains(book.Path, ".zip") {
		bookData, err = s.readFromZip(book)
	} else if strings.Contains(book.Path, ".7z") {
		bookData, err = s.readFrom7z(book)
	} else {
		filePath := filepath.Join(s.config.Library.Root, book.Path)
		bookData, err = os.ReadFile(filePath)
	}

	if err != nil {
		log.Printf("Failed to read book %d: %v", id, err)
		http.Error(w, "Failed to read book", http.StatusInternalServerError)
		return
	}

	// Convert to FB2 if needed
	if format != "fb2" {
		if s.converter == nil || s.converter.EbookConvertPath == "" {
			http.Error(w, i18n.T(getLang(r), "reader.not_supported"), http.StatusBadRequest)
			return
		}
		bookData, err = s.converter.ConvertToFB2(bookData, format, s.config.Converters.TempDir)
		if err != nil {
			log.Printf("Failed to convert book %d to FB2: %v", id, err)
			http.Error(w, i18n.T(getLang(r), "reader.conversion_failed"), http.StatusInternalServerError)
			return
		}
	}

	// Convert FB2 to reader HTML
	content, err := converter.FB2ToReaderHTML(bookData)
	if err != nil {
		log.Printf("Failed to parse FB2 for book %d: %v", id, err)
		http.Error(w, "Failed to parse book", http.StatusInternalServerError)
		return
	}

	pd := s.newPageData(r, content.Title)
	data := ReaderData{
		PageData:  pd,
		BookID:    id,
		BookTitle: content.Title,
		Authors:   content.Authors,
		Content:   content.HTML,
		TOC:       content.TOC,
		Cover:     content.Cover,
		BackURL:   fmt.Sprintf("%s/", s.config.Server.WebPrefix),
	}

	s.renderReaderTemplate(w, data)
}

// readFrom7z reads a book file from a 7z archive
func (s *Server) readFrom7z(book *database.Book) ([]byte, error) {
	parts := strings.Split(book.Path, string(filepath.Separator))

	// Find where the .7z extension is in the path
	idx := -1
	for i, part := range parts {
		if strings.HasSuffix(strings.ToLower(part), ".7z") {
			idx = i
			break
		}
	}

	if idx < 0 {
		if strings.HasSuffix(strings.ToLower(book.Path), ".7z") {
			idx = len(parts) - 1
		} else {
			return nil, fmt.Errorf("book not in 7z")
		}
	}

	szFilePath := filepath.Join(s.config.Library.Root, filepath.Join(parts[:idx+1]...))
	var internalPath string
	if idx+1 < len(parts) {
		internalPath = filepath.Join(append(parts[idx+1:], book.Filename)...)
	} else {
		internalPath = book.Filename
	}

	sz, err := sevenzip.OpenReader(szFilePath)
	if err != nil {
		return nil, err
	}
	defer sz.Close()

	for _, f := range sz.File {
		if f.Name == internalPath || f.Name == book.Filename {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}

	return nil, fmt.Errorf("file not found in 7z")
}

func (s *Server) renderReaderTemplate(w http.ResponseWriter, data ReaderData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	tmpl, err := template.New("reader").Funcs(template.FuncMap{
		"t": func(key string) string {
			return i18n.T(data.Lang, key)
		},
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
	}).Parse(readerTemplate)
	if err != nil {
		log.Printf("Reader template parse error: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Reader template execute error: %v", err)
	}
}

// Helper functions for prefix-based navigation

func (s *Server) getAuthorPrefixes(ctx context.Context, prefix string, length int) []string {
	prefixes, err := s.svc.GetAuthorPrefixesFiltered(ctx, prefix, length)
	if err != nil {
		return nil
	}
	return prefixes
}

func (s *Server) countAuthorsByPrefix(ctx context.Context, prefix string) int {
	count, err := s.svc.CountAuthorsByPrefixQuery(ctx, prefix)
	if err != nil {
		return 0
	}
	return int(count)
}

func (s *Server) getSeriesPrefixes(ctx context.Context, prefix string, length int) []string {
	prefixes, err := s.svc.GetSeriesPrefixesFiltered(ctx, prefix, length)
	if err != nil {
		return nil
	}
	return prefixes
}

func (s *Server) countSeriesByPrefix(ctx context.Context, prefix string) int {
	count, err := s.svc.CountSeriesByPrefixQuery(ctx, prefix)
	if err != nil {
		return 0
	}
	return int(count)
}

// SearchFilterOptions holds unique filter values collected from search results
type SearchFilterOptions struct {
	Languages  []string
	FirstNames []string
	LastNames  []string
	Genres     []LinkedItem
}

func (s *Server) booksToView(ctx context.Context, books []database.Book) []BookView {
	views, _ := s.booksToViewWithFilters(ctx, books, nil)
	return views
}

func (s *Server) booksToViewForUser(ctx context.Context, books []database.Book, username string) []BookView {
	bookshelfIDs, _ := s.svc.GetBookShelfIDs(ctx, username)
	views, _ := s.booksToViewWithFilters(ctx, books, bookshelfIDs)
	return views
}

func (s *Server) booksToViewWithFilters(ctx context.Context, books []database.Book, bookshelfIDs map[int64]bool) ([]BookView, SearchFilterOptions) {
	var views []BookView

	// Maps to collect unique values
	langSet := make(map[string]bool)
	firstNameSet := make(map[string]bool)
	lastNameSet := make(map[string]bool)
	genreMap := make(map[int64]string)

	for _, b := range books {
		authors, _ := s.svc.GetBookAuthors(ctx, b.ID)
		genres, _ := s.svc.GetBookGenres(ctx, b.ID)
		series, _ := s.svc.GetBookSeries(ctx, b.ID)

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
		var duplicateOf int64
		if b.DuplicateOf != nil {
			duplicateOf = *b.DuplicateOf
		} else {
			// This is an original - count its duplicates (subtract 1 to exclude self)
			cnt, _ := s.svc.GetDuplicateCount(ctx, b.ID)
			if cnt > 1 {
				dupCount = cnt - 1 // Show only additional duplicates
			}
		}

		// Audiobook fields
		var narratorLinks []LinkedItem
		var durationStr string
		if b.IsAudiobook {
			if narrators, err := s.svc.GetBookNarrators(ctx, b.ID); err == nil {
				for _, n := range narrators {
					narratorLinks = append(narratorLinks, LinkedItem{
						ID:   n.ID,
						Name: n.FullName(),
					})
				}
			}
			durationStr = formatDuration(b.DurationSeconds)
		}

		views = append(views, BookView{
			ID:              b.ID,
			Title:           b.Title,
			Authors:         authorLinks,
			Genres:          genreLinks,
			Series:          seriesLinks,
			Lang:            b.Lang,
			LangName:        getLanguageName(b.Lang),
			Format:          strings.ToUpper(b.Format),
			Size:            formatSize(b.Filesize),
			Annotation:      truncate(b.Annotation, 300),
			HasCover:        isFB2,
			CanEPUB:         isFB2,
			CanMOBI:         isFB2,
			DuplicateOf:     duplicateOf,
			DuplicateCount:  dupCount,
			OnBookshelf:     bookshelfIDs != nil && bookshelfIDs[b.ID],
			IsAudiobook:     b.IsAudiobook,
			Duration:        durationStr,
			DurationSeconds: b.DurationSeconds,
			TrackCount:      b.TrackCount,
			Narrators:       narratorLinks,
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

func formatDuration(seconds int) string {
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
	// Extract language from data for translation function
	lang := i18n.DefaultLang
	if pd, ok := data.(interface{ GetLang() string }); ok {
		lang = pd.GetLang()
	} else {
		// Try to get Lang field via reflection-free type assertions
		switch d := data.(type) {
		case PageData:
			lang = d.Lang
		case *PageData:
			lang = d.Lang
		case MainMenuData:
			lang = d.Lang
		case BooksData:
			lang = d.Lang
		case AuthorsData:
			lang = d.Lang
		case GenresData:
			lang = d.Lang
		case SeriesData:
			lang = d.Lang
		case LanguagesData:
			lang = d.Lang
		case CatalogsData:
			lang = d.Lang
		case HelpData:
			lang = d.Lang
		}
	}
	if lang == "" {
		lang = i18n.DefaultLang
	}

	// Auth info is not directly available here since we don't have the request
	// We'll need to pass it through the data or use a different approach

	funcMap := template.FuncMap{
		"t": func(key string) string {
			return i18n.T(lang, key)
		},
		"sortPrefixes": func(prefixes []string) []string {
			sorted := make([]string, len(prefixes))
			copy(sorted, prefixes)
			sort.Strings(sorted)
			return sorted
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"formatDuration": func(seconds int) string {
			if seconds <= 0 {
				return ""
			}
			hours := seconds / 3600
			minutes := (seconds % 3600) / 60
			secs := seconds % 60
			if hours > 0 {
				return fmt.Sprintf("%dh %dm", hours, minutes)
			}
			if minutes > 0 {
				return fmt.Sprintf("%dm %ds", minutes, secs)
			}
			return fmt.Sprintf("%ds", secs)
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
    <link rel="icon" type="image/svg+xml" href="/favicon.svg">
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
        .search-form input.search-author {
            flex: 0 0 150px;
            min-width: 100px;
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
        .search-option {
            display: flex;
            align-items: center;
            gap: 4px;
            font-size: 0.85rem;
            color: rgba(255,255,255,0.8);
            cursor: pointer;
            white-space: nowrap;
        }
        .search-option input[type="checkbox"] {
            width: 16px;
            height: 16px;
            accent-color: var(--primary);
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
        .btn-info {
            background: linear-gradient(135deg, #06b6d4, #0891b2);
            color: white;
        }
        .btn-info:hover {
            box-shadow: 0 4px 12px rgba(6, 182, 212, 0.4);
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
        .lang-switch { display: flex; gap: 5px; margin-left: 10px; padding-left: 10px; border-left: 1px solid var(--border); }
        .lang-switch a { padding: 4px 8px; border-radius: 4px; text-decoration: none; color: var(--gray); font-size: 0.85rem; text-transform: uppercase; }
        .lang-switch a:hover { background: var(--border); }
        .lang-switch a.active { background: var(--primary); color: white; }
        /* User dropdown */
        .user-dropdown { position: relative; margin-left: 10px; }
        .user-btn { background: var(--light); border: 1px solid var(--border); padding: 8px 14px; border-radius: 20px; cursor: pointer; display: flex; align-items: center; gap: 6px; font-size: 0.9rem; color: var(--dark); transition: all 0.3s; }
        .user-btn:hover { background: var(--primary); color: white; }
        .user-btn i { font-size: 1.1rem; }
        .user-menu { display: none; position: absolute; right: 0; top: 100%; padding-top: 5px; background: transparent; min-width: 150px; z-index: 100; }
        .user-menu-inner { background: white; border-radius: 8px; box-shadow: var(--shadow); overflow: hidden; }
        .user-dropdown:hover .user-menu { display: block; }
        .user-menu-inner a { display: flex; align-items: center; gap: 8px; padding: 12px 16px; color: var(--dark); text-decoration: none; font-size: 0.9rem; transition: all 0.2s; }
        .user-menu-inner a:hover { background: var(--primary); color: white; }
        .user-menu-inner a:hover i { color: white; }
        .user-menu-inner a i { width: 16px; color: var(--gray); }
        /* Guest warning banner */
        .guest-warning { background: #fef3c7; border: 1px solid #fcd34d; color: #92400e; padding: 12px 20px; border-radius: 8px; margin-bottom: 20px; font-size: 0.9rem; display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
        .guest-warning i { color: #f59e0b; }
        .guest-warning a { color: var(--primary); text-decoration: none; font-weight: 500; }
        .guest-warning a:hover { text-decoration: underline; }
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
                    <a href="{{.WebPrefix}}/"><i class="fas fa-home"></i> {{t "nav.home"}}</a>
                    <a href="{{.WebPrefix}}/catalogs"><i class="fas fa-folder"></i> {{t "nav.catalogs"}}</a>
                    <a href="{{.WebPrefix}}/authors"><i class="fas fa-user-pen"></i> {{t "nav.authors"}}</a>
                    <a href="{{.WebPrefix}}/genres"><i class="fas fa-tags"></i> {{t "nav.genres"}}</a>
                    <a href="{{.WebPrefix}}/series"><i class="fas fa-layer-group"></i> {{t "nav.series"}}</a>
                    <a href="{{.WebPrefix}}/languages"><i class="fas fa-globe"></i> {{t "nav.languages"}}</a>
                    <a href="{{.WebPrefix}}/new"><i class="fas fa-sparkles"></i> {{t "nav.new"}}</a>
                    <a href="{{.WebPrefix}}/audio"><i class="fas fa-headphones"></i> {{t "nav.audio"}}</a>
                    <a href="{{.WebPrefix}}/bookshelf"><i class="fas fa-bookmark"></i> {{t "nav.bookshelf"}}</a>
                    <a href="{{.OPDSPrefix}}/"><i class="fas fa-rss"></i> OPDS</a>
                    <a href="{{.WebPrefix}}/help"><i class="fas fa-circle-question"></i> {{t "nav.help"}}</a>
                    <span class="lang-switch">{{range .Languages}}<a href="javascript:void(0)" onclick="switchLang('{{.Code}}')"{{if eq $.Lang .Code}} class="active"{{end}}>{{.Code}}</a>{{end}}</span>
                    <div class="user-dropdown">
                        <button class="user-btn"><i class="fas fa-user-circle"></i>{{if .Auth.IsAuthenticated}} {{.Auth.Username}}{{else if .Auth.IsAnonymous}} {{t "auth.guest"}}{{end}}</button>
                        <div class="user-menu"><div class="user-menu-inner">
                            {{if .Auth.IsAuthenticated}}
                            <a href="{{.WebPrefix}}/logout"><i class="fas fa-sign-out-alt"></i> {{t "auth.logout"}}</a>
                            {{else}}
                            <a href="{{.WebPrefix}}/login"><i class="fas fa-sign-in-alt"></i> {{t "auth.login"}}</a>
                            <a href="{{.WebPrefix}}/register"><i class="fas fa-user-plus"></i> {{t "auth.register"}}</a>
                            {{end}}
                        </div></div>
                    </div>
                </nav>
            </div>
            <form class="search-form" action="{{.WebPrefix}}/search" method="get">
                <input type="text" name="q" placeholder="{{t "search.title"}}{{if .ScopeName}} {{t "search.in"}} {{.ScopeName}}{{end}}..." value="{{.Query}}">
                <input type="text" name="author" placeholder="{{t "search.author"}}" class="search-author">
                {{if .ScopeAuthorID}}<input type="hidden" name="author_id" value="{{.ScopeAuthorID}}">{{end}}
                {{if .ScopeGenreID}}<input type="hidden" name="genre_id" value="{{.ScopeGenreID}}">{{end}}
                {{if .ScopeSeriesID}}<input type="hidden" name="series_id" value="{{.ScopeSeriesID}}">{{end}}
                {{if .ScopeCatalogID}}<input type="hidden" name="catalog_id" value="{{.ScopeCatalogID}}">{{end}}
                {{if .ScopeLang}}<input type="hidden" name="lang" value="{{.ScopeLang}}">{{end}}
                <label class="search-option" title="Also search in book description"><input type="checkbox" name="desc" value="1"{{if .IncludeDesc}} checked{{end}}> +desc</label>
                <button type="submit"><i class="fas fa-search"></i></button>
            </form>
        </header>
        {{if .Auth.IsAnonymous}}
        <div class="guest-warning">
            <i class="fas fa-exclamation-triangle"></i> {{t "auth.guest_warning"}}
            <a href="{{.WebPrefix}}/login">{{t "auth.login"}}</a> | <a href="{{.WebPrefix}}/register">{{t "auth.register"}}</a>
        </div>
        {{end}}
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
    function switchLang(code) {
        document.cookie = 'lang=' + code + ';path=/;max-age=31536000';
        location.reload();
    }
    </script>
</body>
</html>
{{define "content"}}{{end}}`

var templates = map[string]string{
	"main": `{{define "content"}}
<h2><i class="fas fa-chart-pie"></i> {{t "main.stats"}}</h2>
{{if .Stats}}
<div class="stats-grid">
    <div class="stat-card">
        <div class="number">{{.Stats.BooksCount}}</div>
        <div class="label"><i class="fas fa-book"></i> {{t "main.books"}}</div>
    </div>
    <div class="stat-card">
        <div class="number">{{.Stats.AuthorsCount}}</div>
        <div class="label"><i class="fas fa-users"></i> {{t "main.authors"}}</div>
    </div>
    <div class="stat-card">
        <div class="number">{{.Stats.GenresCount}}</div>
        <div class="label"><i class="fas fa-tags"></i> {{t "main.genres"}}</div>
    </div>
    <div class="stat-card">
        <div class="number">{{.Stats.SeriesCount}}</div>
        <div class="label"><i class="fas fa-list"></i> {{t "main.series"}}</div>
    </div>
    {{if .NewBooks}}
    <div class="stat-card">
        <div class="number">{{.NewBooks}}</div>
        <div class="label"><i class="fas fa-star"></i> {{t "main.new7d"}}</div>
    </div>
    {{end}}
</div>
{{end}}
<h2><i class="fas fa-compass"></i> {{t "main.browse"}}</h2>
<div class="menu-grid">
    <a href="{{.WebPrefix}}/catalogs" class="menu-card">
        <i class="fas fa-folder-tree"></i>
        <div class="title">{{t "nav.catalogs"}}</div>
    </a>
    <a href="{{.WebPrefix}}/authors" class="menu-card">
        <i class="fas fa-user-pen"></i>
        <div class="title">{{t "nav.authors"}}</div>
    </a>
    <a href="{{.WebPrefix}}/genres" class="menu-card">
        <i class="fas fa-masks-theater"></i>
        <div class="title">{{t "nav.genres"}}</div>
    </a>
    <a href="{{.WebPrefix}}/series" class="menu-card">
        <i class="fas fa-layer-group"></i>
        <div class="title">{{t "nav.series"}}</div>
    </a>
    <a href="{{.WebPrefix}}/languages" class="menu-card">
        <i class="fas fa-globe"></i>
        <div class="title">{{t "nav.languages"}}</div>
    </a>
    <a href="{{.WebPrefix}}/new" class="menu-card">
        <i class="fas fa-wand-magic-sparkles"></i>
        <div class="title">{{t "main.newbooks"}}</div>
    </a>
    <a href="{{.WebPrefix}}/audio" class="menu-card">
        <i class="fas fa-headphones"></i>
        <div class="title">{{t "main.audiobooks"}}</div>
    </a>
    <a href="{{.WebPrefix}}/search" class="menu-card">
        <i class="fas fa-magnifying-glass"></i>
        <div class="title">{{t "main.search"}}</div>
    </a>
    <a href="{{.WebPrefix}}/bookshelf" class="menu-card">
        <i class="fas fa-bookmark"></i>
        <div class="title">{{t "nav.bookshelf"}}</div>
    </a>
</div>
{{end}}`,

	"search": `{{define "content"}}
<h2><i class="fas fa-search"></i> {{t "search.books"}}</h2>
<p style="color: var(--gray); font-size: 1.1rem;">{{t "search.hint"}}</p>
{{end}}`,

	"books": `{{define "content"}}
<div style="display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 15px; margin-bottom: 20px;">
    <h2 style="margin: 0;"><i class="fas fa-book"></i> {{.Title}}{{if .TotalCount}} <span style="color: var(--gray); font-weight: normal; font-size: 0.7em;">({{.TotalCount}} {{t "main.books"}})</span>{{end}}</h2>
    {{if .CurrentPath}}
    <div class="page-size-selector">
        <span style="color: var(--gray); margin-right: 10px;">{{t "books.show"}}</span>
        <a href="javascript:setPageSize(10)" class="size-btn{{if eq .PageSize 10}} active{{end}}">10</a>
        <a href="javascript:setPageSize(50)" class="size-btn{{if eq .PageSize 50}} active{{end}}">50</a>
        <a href="javascript:setPageSize(100)" class="size-btn{{if eq .PageSize 100}} active{{end}}">100</a>
        <a href="javascript:setPageSize(200)" class="size-btn{{if eq .PageSize 200}} active{{end}}">200</a>
        <a href="javascript:setPageSize('all')" class="size-btn{{if eq .PageSize 0}} active{{end}}">{{t "books.all"}}</a>
    </div>
    {{end}}
</div>
{{if or .FilterLangs .FirstNames .LastNames .Genres}}
<div style="display: flex; gap: 15px; margin-bottom: 20px; flex-wrap: wrap; align-items: center;">
    <span style="color: var(--gray);"><i class="fas fa-filter"></i> {{t "books.filters"}}</span>
    {{if and .FilterLangs (not .FilterLang)}}
    <select onchange="applyFilter('lang', this.value)" style="padding: 8px 12px; border-radius: 8px; border: 1px solid var(--border); background: var(--card-bg); color: var(--text);">
        <option value="">{{t "books.alllang"}}</option>
        {{range .FilterLangs}}<option value="{{.Code}}">{{.Name}}</option>{{end}}
    </select>
    {{end}}
    {{if .FilterLang}}
    <span style="padding: 8px 12px; border-radius: 8px; background: var(--accent); color: white;">Lang: {{.FilterLang}}{{if not .ScopeLang}} <a href="javascript:applyFilter('lang','')" style="color: white; margin-left: 5px;">×</a>{{end}}</span>
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
    <a href="javascript:clearFilters()" style="color: var(--accent); text-decoration: none;"><i class="fas fa-times"></i> Clear all</a>
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
function setPageSize(size) {
    var url = new URL(window.location.href);
    if (size === 'all') {
        url.searchParams.set('size', 'all');
    } else {
        url.searchParams.set('size', size);
    }
    url.searchParams.delete('page');
    window.location.href = url.toString();
}
function clearFilters() {
    var url = new URL(window.location.href);
    url.searchParams.delete('lang');
    url.searchParams.delete('fname');
    url.searchParams.delete('lname');
    url.searchParams.delete('genre');
    url.searchParams.delete('page');
    window.location.href = url.toString();
}
function goToPage(page) {
    var url = new URL(window.location.href);
    if (page > 0) {
        url.searchParams.set('page', page);
    } else {
        url.searchParams.delete('page');
    }
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
                <div class="book-title">{{if .IsAudiobook}}<i class="fas fa-headphones" style="color: var(--primary);"></i> {{end}}{{.Title}}</div>
                <div class="book-meta">
                    {{if .Authors}}<span><i class="fas fa-user"></i> {{range $i, $a := .Authors}}{{if lt $i 2}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/authors/{{$a.ID}}" class="meta-link">{{$a.Name}}</a>{{end}}{{end}}{{if gt (len .Authors) 2}} <span class="more-info">+{{sub (len .Authors) 2}}<div class="more-info-tooltip{{if gt (len .Authors) 70}} below{{end}}">{{range $i, $a := .Authors}}{{if ge $i 2}}<a href="{{$.WebPrefix}}/authors/{{$a.ID}}">{{$a.Name}}</a>{{end}}{{end}}</div></span>{{end}}</span>{{end}}
                    {{if .Narrators}}<span><i class="fas fa-microphone"></i> {{range $i, $n := .Narrators}}{{if lt $i 2}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/authors/{{$n.ID}}" class="meta-link">{{$n.Name}}</a>{{end}}{{end}}{{if gt (len .Narrators) 2}} <span class="more-info">+{{sub (len .Narrators) 2}}</span>{{end}}</span>{{end}}
                    {{if .Series}}<span><i class="fas fa-layer-group"></i> {{range $i, $s := .Series}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/series/{{$s.ID}}" class="meta-link">{{$s.Name}}</a>{{end}}</span>{{end}}
                    {{if .Lang}}<span><i class="fas fa-globe"></i> <a href="{{$.WebPrefix}}/languages/{{.Lang}}" class="meta-link">{{.LangName}}</a></span>{{end}}
                    <span><i class="fas fa-file"></i> {{.Format}}</span>
                    {{if .Duration}}<span><i class="fas fa-clock"></i> {{.Duration}}</span>{{else}}<span><i class="fas fa-weight-hanging"></i> {{.Size}}</span>{{end}}
                    {{if gt .TrackCount 1}}<a href="{{$.WebPrefix}}/audio/{{.ID}}" class="meta-link"><i class="fas fa-list-ol"></i> {{.TrackCount}} {{t "audio.tracks"}}</a>{{end}}
                </div>
                {{if .Genres}}<div class="book-meta"><span><i class="fas fa-tag"></i> {{range $i, $g := .Genres}}{{if lt $i 3}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/genres/{{$g.ID}}" class="meta-link">{{$g.Name}}</a>{{end}}{{end}}{{if gt (len .Genres) 3}} <span class="more-info">+{{sub (len .Genres) 3}}<div class="more-info-tooltip{{if gt (len .Genres) 71}} below{{end}}">{{range $i, $g := .Genres}}{{if ge $i 3}}<a href="{{$.WebPrefix}}/genres/{{$g.ID}}">{{$g.Name}}</a>{{end}}{{end}}</div></span>{{end}}</span></div>{{end}}
                {{if .Annotation}}<div class="book-annotation">{{.Annotation}}</div>{{end}}
            </div>
        </div>
        <div class="book-actions">
            {{if .IsAudiobook}}<a href="{{$.WebPrefix}}/audio/{{.ID}}" class="btn btn-primary"><i class="fas fa-headphones"></i> {{if gt .TrackCount 1}}{{.TrackCount}} {{t "audio.tracks"}}{{else}}{{t "audio.book"}}{{end}}</a>{{else}}<a href="{{$.OPDSPrefix}}/book/{{.ID}}/download" class="btn btn-primary"><i class="fas fa-download"></i> {{.Format}}</a>{{end}}
            {{if or (eq .Format "FB2") (eq .Format "EPUB") (eq .Format "MOBI")}}<a href="{{$.WebPrefix}}/read/{{.ID}}" class="btn btn-info"><i class="fas fa-book-open"></i> {{t "reader.read"}}</a>{{end}}
            {{if and $.HasEPUB .CanEPUB}}<a href="{{$.OPDSPrefix}}/book/{{.ID}}/epub" class="btn btn-success"><i class="fas fa-file-arrow-down"></i> EPUB</a>{{end}}
            {{if and $.HasMOBI .CanMOBI}}<a href="{{$.OPDSPrefix}}/book/{{.ID}}/mobi" class="btn btn-warning"><i class="fas fa-file-arrow-down"></i> MOBI</a>{{end}}
            {{if .OnBookshelf}}<span class="btn btn-secondary disabled"><i class="fas fa-check"></i> Added</span>{{else}}<a href="#" onclick="return bookshelfAction(this, '{{$.WebPrefix}}/bookshelf/add/{{.ID}}')" class="btn btn-secondary"><i class="fas fa-bookmark"></i> {{t "books.addshelf"}}</a>{{end}}
            {{if or (gt .DuplicateCount 0) (gt .DuplicateOf 0)}}<a href="{{$.WebPrefix}}/duplicates/{{.ID}}" class="btn btn-secondary"><i class="fas fa-copy"></i> {{t "books.duplicates"}}{{if gt .DuplicateCount 0}} ({{.DuplicateCount}}){{end}}</a>{{end}}
        </div>
    </div>
{{end}}
</div>
{{if or (gt .Page 0) .HasMore}}
<div class="pagination">
    {{if gt .Page 0}}<a href="javascript:goToPage({{.PrevPage}})"><i class="fas fa-arrow-left"></i> {{t "books.prev"}}</a>{{end}}
    {{if .HasMore}}<a href="javascript:goToPage({{.NextPage}})">{{t "books.next"}} <i class="fas fa-arrow-right"></i></a>{{end}}
</div>
{{end}}
{{else}}
<p style="text-align: center; color: var(--gray); padding: 40px;">{{t "books.nobooks"}}</p>
{{end}}
{{end}}`,

	"audiodetail": `{{define "content"}}
<div class="audio-detail">
    <div class="audio-header" id="audioHeader">
        <img id="audioCover" src="{{if .Book.HasCover}}{{.OPDSPrefix}}/book/{{.Book.ID}}/cover{{else}}data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 150 150'%3E%3Crect fill='%23333' width='150' height='150'/%3E%3Ctext x='75' y='80' text-anchor='middle' fill='%23666' font-size='40'%3E%F0%9F%8E%A7%3C/text%3E%3C/svg%3E{{end}}" class="audio-cover" alt="Cover" onerror="this.src='data:image/svg+xml,%3Csvg xmlns=\'http://www.w3.org/2000/svg\' viewBox=\'0 0 150 150\'%3E%3Crect fill=\'%23333\' width=\'150\' height=\'150\'/%3E%3Ctext x=\'75\' y=\'80\' text-anchor=\'middle\' fill=\'%23666\' font-size=\'40\'%3E%F0%9F%8E%A7%3C/text%3E%3C/svg%3E'">
        <div class="audio-info">
            <h1><i class="fas fa-headphones"></i> {{.Book.Title}}</h1>
            <div id="nowPlaying" class="now-playing hidden">
                <i class="fas fa-volume-up pulse"></i> <span id="nowPlayingTrack">--</span>
            </div>
            {{if .Authors}}
            <div class="audio-authors">
                <i class="fas fa-user"></i>
                {{range $i, $a := .Authors}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/authors/{{$a.ID}}">{{$a.FirstName}} {{$a.LastName}}</a>{{end}}
            </div>
            {{end}}
            <div class="audio-meta">
                {{if .Structure}}
                    {{if eq .Structure.Type "collection"}}
                    <span class="badge badge-collection"><i class="fas fa-folder-tree"></i> {{t "audio.collection"}}</span>
                    {{else}}
                    <span class="badge badge-book"><i class="fas fa-book"></i> {{t "audio.book"}}</span>
                    {{end}}
                {{end}}
                <span><i class="fas fa-clock"></i> {{.Book.Duration}}</span>
                <span><i class="fas fa-list-ol"></i> {{.Book.TrackCount}} {{t "audio.tracks"}}</span>
                <span><i class="fas fa-weight-hanging"></i> {{.Book.Size}}</span>
            </div>
        </div>
        <div class="audio-actions">
            <a href="{{.OPDSPrefix}}/book/{{.Book.ID}}/download" class="btn btn-primary btn-lg">
                <i class="fas fa-file-archive"></i> {{t "audio.download"}}
            </a>
            <button onclick="downloadSelected()" class="btn btn-success btn-lg" id="downloadSelectedBtn" disabled>
                <i class="fas fa-download"></i> <span id="downloadSelectedText">{{t "audio.downloadsel"}}</span>
            </button>
            {{if .Book.OnBookshelf}}<span class="btn btn-secondary disabled"><i class="fas fa-check"></i> Added</span>{{else}}<a href="#" onclick="return bookshelfAction(this, '{{.WebPrefix}}/bookshelf/add/{{.Book.ID}}')" class="btn btn-secondary"><i class="fas fa-bookmark"></i> {{t "books.addshelf"}}</a>{{end}}
        </div>
    </div>

    {{if .Structure}}
    <div class="audio-structure">
        <div class="select-controls">
            <label class="select-all-label">
                <input type="checkbox" id="selectAll" onchange="toggleSelectAll()">
                <span>{{t "audio.selectall"}}</span>
            </label>
        </div>
        {{if eq .Structure.Type "collection"}}
        <h2><i class="fas fa-folder-tree"></i> {{t "audio.parts"}} ({{len .Structure.Parts}})</h2>
        <div class="audio-parts">
            {{range $i, $part := .Structure.Parts}}
            <details class="audio-part" {{if eq $i 0}}open{{end}}>
                <summary>
                    <label class="part-checkbox" onclick="event.stopPropagation()">
                        <input type="checkbox" class="part-select" data-part="{{$i}}" onchange="togglePartSelect({{$i}})">
                    </label>
                    <i class="fas fa-folder"></i>
                    <span class="part-name">{{$part.Name}}</span>
                    <span class="part-meta">
                        <span class="track-count">{{len $part.Tracks}} {{t "audio.tracks"}}</span>
                        <span class="part-duration">{{formatDuration $part.Duration}}</span>
                    </span>
                </summary>
                <ul class="track-list" data-part="{{$i}}">
                    {{range $j, $track := $part.Tracks}}
                    {{$trackFile := or $track.Path $track.Name}}
                    <li data-url="{{$.WebPrefix}}/audio/{{$.Book.ID}}/track?file={{$trackFile}}" data-name="{{$track.Name}}" data-duration="{{$track.Duration}}" data-path="{{$trackFile}}" onclick="player.onTrackClick(this, event)">
                        <label class="track-checkbox" onclick="event.stopPropagation()">
                            <input type="checkbox" class="track-select" data-part="{{$i}}" data-path="{{$trackFile}}" onchange="updateSelection()">
                        </label>
                        <i class="fas fa-music track-icon"></i>
                        <span class="track-name">{{$track.Name}}</span>
                        <span class="track-duration">{{formatDuration $track.Duration}}</span>
                        <button class="track-play" onclick="event.stopPropagation(); player.playTrack(this.closest('li'))" title="Play">
                            <i class="fas fa-play"></i>
                        </button>
                        <a href="{{$.WebPrefix}}/audio/{{$.Book.ID}}/track?file={{$trackFile}}" class="track-download" title="Download" onclick="event.stopPropagation()">
                            <i class="fas fa-download"></i>
                        </a>
                    </li>
                    {{end}}
                </ul>
            </details>
            {{end}}
        </div>
        {{else}}
        <h2><i class="fas fa-list-ol"></i> {{t "audio.tracks"}} ({{len .Structure.Tracks}})</h2>
        <ul class="track-list flat">
            {{range $i, $track := .Structure.Tracks}}
            {{$trackFile := or $track.Path $track.Name}}
            <li data-url="{{$.WebPrefix}}/audio/{{$.Book.ID}}/track?file={{$trackFile}}" data-name="{{$track.Name}}" data-duration="{{$track.Duration}}" data-path="{{$trackFile}}" onclick="player.onTrackClick(this, event)">
                <label class="track-checkbox" onclick="event.stopPropagation()">
                    <input type="checkbox" class="track-select" data-path="{{$trackFile}}" onchange="updateSelection()">
                </label>
                <i class="fas fa-music track-icon"></i>
                <span class="track-name">{{$track.Name}}</span>
                <span class="track-duration">{{formatDuration $track.Duration}}</span>
                <button class="track-play" onclick="event.stopPropagation(); player.playTrack(this.closest('li'))" title="Play">
                    <i class="fas fa-play"></i>
                </button>
                <a href="{{$.WebPrefix}}/audio/{{$.Book.ID}}/track?file={{$trackFile}}" class="track-download" title="Download" onclick="event.stopPropagation()">
                    <i class="fas fa-download"></i>
                </a>
            </li>
            {{end}}
        </ul>
        {{end}}
    </div>
    {{end}}
</div>

<!-- Audio Player Bar -->
<div id="playerBar" class="player-bar hidden">
    <div class="player-progress-container" id="progressContainer">
        <div class="player-progress-bar" id="progressBar"></div>
        <div class="player-progress-buffered" id="bufferedBar"></div>
    </div>
    <div class="player-content">
        <div class="player-track-info">
            <span class="player-track-name" id="playerTrackName">--</span>
            <span class="player-track-time">
                <span id="playerCurrentTime">0:00</span> / <span id="playerDuration">0:00</span>
            </span>
        </div>
        <div class="player-controls">
            <button class="player-btn" id="btnPrev" title="Previous track">
                <i class="fas fa-step-backward"></i>
            </button>
            <button class="player-btn" id="btnRewind" title="Rewind 15s">
                <i class="fas fa-undo"></i>
                <span class="btn-label">15</span>
            </button>
            <button class="player-btn player-btn-main" id="btnPlayPause" title="Play/Pause">
                <i class="fas fa-play"></i>
            </button>
            <button class="player-btn" id="btnForward" title="Forward 15s">
                <i class="fas fa-redo"></i>
                <span class="btn-label">15</span>
            </button>
            <button class="player-btn" id="btnNext" title="Next track">
                <i class="fas fa-step-forward"></i>
            </button>
        </div>
        <div class="player-extras">
            <div class="speed-control">
                <button class="player-btn speed-btn" id="btnSpeed">1x</button>
            </div>
            <button class="player-btn" id="btnVolume" title="Volume">
                <i class="fas fa-volume-up"></i>
            </button>
        </div>
    </div>
</div>

<audio id="audioPlayer"></audio>

<script>
const bookId = {{.Book.ID}};
const bookFormat = "{{.Book.Format}}";
const webPrefix = "{{.WebPrefix}}";
const i18n = {
    downloadsel: "{{t "audio.downloadsel"}}"
};

// Cookie helpers
function setCookie(name, value, days) {
    const expires = new Date(Date.now() + days * 864e5).toUTCString();
    document.cookie = name + '=' + encodeURIComponent(value) + '; expires=' + expires + '; path=/';
}
function getCookie(name) {
    return document.cookie.split('; ').reduce((r, v) => {
        const parts = v.split('=');
        return parts[0] === name ? decodeURIComponent(parts[1]) : r;
    }, '');
}

// Audio Player Class
class AudioPlayer {
    constructor() {
        this.audio = document.getElementById('audioPlayer');
        this.playerBar = document.getElementById('playerBar');
        this.progressContainer = document.getElementById('progressContainer');
        this.progressBar = document.getElementById('progressBar');
        this.bufferedBar = document.getElementById('bufferedBar');
        this.trackNameEl = document.getElementById('playerTrackName');
        this.currentTimeEl = document.getElementById('playerCurrentTime');
        this.durationEl = document.getElementById('playerDuration');
        this.playPauseBtn = document.getElementById('btnPlayPause');
        this.speedBtn = document.getElementById('btnSpeed');

        this.tracks = [];
        this.currentTrackIndex = -1;
        this.currentTrackLi = null;
        this.speeds = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2];
        this.speedIndex = 2; // Default 1x
        this.isSeeking = false;
        this.trackPositions = {}; // Per-track position storage
        this.coverImg = document.getElementById('audioCover');
        this.nowPlayingEl = document.getElementById('nowPlaying');
        this.nowPlayingTrackEl = document.getElementById('nowPlayingTrack');

        this.init();
    }

    init() {
        // Collect all tracks
        document.querySelectorAll('.track-list li[data-url]').forEach((li, idx) => {
            this.tracks.push({
                el: li,
                url: li.dataset.url,
                name: li.dataset.name,
                duration: parseInt(li.dataset.duration) || 0,
                path: li.dataset.path || ''
            });
        });

        // Event listeners
        this.audio.addEventListener('timeupdate', () => this.onTimeUpdate());
        this.audio.addEventListener('loadedmetadata', () => this.onLoadedMetadata());
        this.audio.addEventListener('ended', () => this.onEnded());
        this.audio.addEventListener('play', () => this.onPlay());
        this.audio.addEventListener('pause', () => this.onPause());
        this.audio.addEventListener('progress', () => this.onProgress());
        this.audio.addEventListener('waiting', () => this.onWaiting());
        this.audio.addEventListener('canplay', () => this.onCanPlay());

        // Control buttons
        document.getElementById('btnPlayPause').addEventListener('click', () => this.togglePlay());
        document.getElementById('btnPrev').addEventListener('click', () => this.prevTrack());
        document.getElementById('btnNext').addEventListener('click', () => this.nextTrack());
        document.getElementById('btnRewind').addEventListener('click', () => this.seek(-15));
        document.getElementById('btnForward').addEventListener('click', () => this.seek(15));
        document.getElementById('btnSpeed').addEventListener('click', () => this.cycleSpeed());
        document.getElementById('btnVolume').addEventListener('click', () => this.toggleMute());

        // Progress bar seeking
        this.progressContainer.addEventListener('click', (e) => this.seekTo(e));
        this.progressContainer.addEventListener('mousedown', (e) => this.startSeek(e));
        document.addEventListener('mousemove', (e) => this.doSeek(e));
        document.addEventListener('mouseup', () => this.endSeek());

        // Touch support for mobile
        this.progressContainer.addEventListener('touchstart', (e) => this.startSeek(e.touches[0]));
        document.addEventListener('touchmove', (e) => { if (this.isSeeking) this.doSeek(e.touches[0]); });
        document.addEventListener('touchend', () => this.endSeek());

        // Keyboard shortcuts
        document.addEventListener('keydown', (e) => this.onKeyDown(e));

        // Load saved state
        this.loadState();

        // Load saved speed
        const savedSpeed = getCookie('audioSpeed');
        if (savedSpeed) {
            const idx = this.speeds.indexOf(parseFloat(savedSpeed));
            if (idx !== -1) {
                this.speedIndex = idx;
                this.audio.playbackRate = this.speeds[this.speedIndex];
                this.speedBtn.textContent = this.speeds[this.speedIndex] + 'x';
            }
        }
    }

    formatTime(seconds) {
        if (isNaN(seconds) || seconds < 0) return '0:00';
        const h = Math.floor(seconds / 3600);
        const m = Math.floor((seconds % 3600) / 60);
        const s = Math.floor(seconds % 60);
        if (h > 0) {
            return h + ':' + m.toString().padStart(2, '0') + ':' + s.toString().padStart(2, '0');
        }
        return m + ':' + s.toString().padStart(2, '0');
    }

    playTrack(li) {
        const idx = this.tracks.findIndex(t => t.el === li);
        if (idx === -1) return;

        // If same track, toggle play/pause
        if (idx === this.currentTrackIndex && this.audio.src) {
            this.togglePlay();
            return;
        }

        // Save current track position before switching
        this.saveCurrentTrackPosition();

        this.currentTrackIndex = idx;
        this.loadTrack(idx);
        this.audio.play().catch(err => console.error('Playback failed:', err));
    }

    onTrackClick(li, event) {
        // Don't trigger if clicking on controls (checkbox, buttons, links)
        if (event.target.closest('.track-checkbox, .track-play, .track-download')) {
            return;
        }
        this.playTrack(li);
    }

    saveCurrentTrackPosition() {
        if (this.currentTrackIndex >= 0 && this.audio.currentTime > 0) {
            const trackPath = this.tracks[this.currentTrackIndex].el.dataset.path;
            if (trackPath) {
                this.trackPositions[trackPath] = this.audio.currentTime;
            }
        }
    }

    loadTrack(idx) {
        if (idx < 0 || idx >= this.tracks.length) return;

        const track = this.tracks[idx];
        const trackPath = track.el.dataset.path;

        // Update highlight
        if (this.currentTrackLi) {
            this.currentTrackLi.classList.remove('playing');
            const icon = this.currentTrackLi.querySelector('.track-icon');
            if (icon) {
                icon.classList.remove('fa-volume-up');
                icon.classList.add('fa-music');
            }
        }
        this.currentTrackLi = track.el;
        this.currentTrackLi.classList.add('playing');
        const icon = this.currentTrackLi.querySelector('.track-icon');
        if (icon) {
            icon.classList.remove('fa-music');
            icon.classList.add('fa-volume-up');
        }

        // Scroll into view if needed
        this.currentTrackLi.scrollIntoView({ behavior: 'smooth', block: 'nearest' });

        // Open parent details if collapsed
        const details = this.currentTrackLi.closest('details');
        if (details && !details.open) {
            details.open = true;
        }

        // Load audio
        this.audio.src = track.url;
        this.audio.load();

        // Update UI
        this.trackNameEl.textContent = track.name;
        this.playerBar.classList.remove('hidden');

        // Update "Now Playing" in header
        if (this.nowPlayingEl && this.nowPlayingTrackEl) {
            this.nowPlayingTrackEl.textContent = track.name;
            this.nowPlayingEl.classList.remove('hidden');
        }

        // Update track-specific cover for folder audiobooks (fast - files on disk)
        if (bookFormat.toLowerCase() === 'folder' && this.coverImg && track.path) {
            const coverUrl = webPrefix + '/audio/' + bookId + '/cover?file=' + encodeURIComponent(track.path);
            // Use a new Image to preload and avoid flicker
            const img = new Image();
            img.onload = () => {
                this.coverImg.src = coverUrl;
            };
            img.onerror = () => {
                // Keep current cover if track doesn't have one
            };
            img.src = coverUrl;
        }

        // Restore saved position for this specific track
        if (trackPath && this.trackPositions[trackPath] > 0) {
            this.audio.currentTime = this.trackPositions[trackPath];
        } else {
            // Check saved state for backwards compatibility
            const savedState = this.getSavedState();
            if (savedState && savedState.trackIndex === idx && savedState.position > 0) {
                this.audio.currentTime = savedState.position;
            }
        }
    }

    togglePlay() {
        if (this.audio.paused) {
            this.audio.play();
        } else {
            this.audio.pause();
        }
    }

    prevTrack() {
        if (this.audio.currentTime > 3) {
            this.audio.currentTime = 0;
        } else if (this.currentTrackIndex > 0) {
            this.currentTrackIndex--;
            this.loadTrack(this.currentTrackIndex);
            this.audio.play();
        }
    }

    nextTrack() {
        if (this.currentTrackIndex < this.tracks.length - 1) {
            this.currentTrackIndex++;
            this.loadTrack(this.currentTrackIndex);
            this.audio.play();
        }
    }

    seek(seconds) {
        this.audio.currentTime = Math.max(0, Math.min(this.audio.duration, this.audio.currentTime + seconds));
    }

    seekTo(e) {
        const rect = this.progressContainer.getBoundingClientRect();
        const percent = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
        this.audio.currentTime = percent * this.audio.duration;
    }

    startSeek(e) {
        this.isSeeking = true;
        this.progressContainer.classList.add('seeking');
    }

    doSeek(e) {
        if (!this.isSeeking) return;
        const rect = this.progressContainer.getBoundingClientRect();
        const percent = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
        this.progressBar.style.width = (percent * 100) + '%';
    }

    endSeek() {
        if (!this.isSeeking) return;
        this.isSeeking = false;
        this.progressContainer.classList.remove('seeking');
        const percent = parseFloat(this.progressBar.style.width) / 100;
        if (!isNaN(percent) && this.audio.duration) {
            this.audio.currentTime = percent * this.audio.duration;
        }
    }

    cycleSpeed() {
        this.speedIndex = (this.speedIndex + 1) % this.speeds.length;
        this.audio.playbackRate = this.speeds[this.speedIndex];
        this.speedBtn.textContent = this.speeds[this.speedIndex] + 'x';
        setCookie('audioSpeed', this.speeds[this.speedIndex], 365);
    }

    toggleMute() {
        this.audio.muted = !this.audio.muted;
        const icon = document.querySelector('#btnVolume i');
        if (this.audio.muted) {
            icon.classList.remove('fa-volume-up');
            icon.classList.add('fa-volume-mute');
        } else {
            icon.classList.remove('fa-volume-mute');
            icon.classList.add('fa-volume-up');
        }
    }

    onTimeUpdate() {
        if (this.isSeeking) return;
        const percent = (this.audio.currentTime / this.audio.duration) * 100;
        this.progressBar.style.width = percent + '%';
        this.currentTimeEl.textContent = this.formatTime(this.audio.currentTime);

        // Save state periodically (every 5 seconds)
        if (Math.floor(this.audio.currentTime) % 5 === 0) {
            this.saveState();
        }
    }

    onLoadedMetadata() {
        this.durationEl.textContent = this.formatTime(this.audio.duration);
    }

    onEnded() {
        this.saveState();
        // Auto-play next track
        if (this.currentTrackIndex < this.tracks.length - 1) {
            this.nextTrack();
        } else {
            // End of playlist
            const icon = this.playPauseBtn.querySelector('i');
            icon.classList.remove('fa-pause');
            icon.classList.add('fa-play');
        }
    }

    onPlay() {
        const icon = this.playPauseBtn.querySelector('i');
        icon.classList.remove('fa-play');
        icon.classList.add('fa-pause');

        // Update track icon
        if (this.currentTrackLi) {
            const trackIcon = this.currentTrackLi.querySelector('.track-icon');
            if (trackIcon) {
                trackIcon.classList.remove('fa-music');
                trackIcon.classList.add('fa-volume-up');
            }
        }
    }

    onPause() {
        const icon = this.playPauseBtn.querySelector('i');
        icon.classList.remove('fa-pause');
        icon.classList.add('fa-play');
        this.saveState();
    }

    onProgress() {
        if (this.audio.buffered.length > 0) {
            const bufferedEnd = this.audio.buffered.end(this.audio.buffered.length - 1);
            const percent = (bufferedEnd / this.audio.duration) * 100;
            this.bufferedBar.style.width = percent + '%';
        }
    }

    onWaiting() {
        this.playPauseBtn.classList.add('loading');
    }

    onCanPlay() {
        this.playPauseBtn.classList.remove('loading');
    }

    onKeyDown(e) {
        // Don't trigger if typing in input
        if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;

        switch(e.code) {
            case 'Space':
                e.preventDefault();
                this.togglePlay();
                break;
            case 'ArrowLeft':
                e.preventDefault();
                this.seek(e.shiftKey ? -30 : -5);
                break;
            case 'ArrowRight':
                e.preventDefault();
                this.seek(e.shiftKey ? 30 : 5);
                break;
            case 'ArrowUp':
                e.preventDefault();
                this.audio.volume = Math.min(1, this.audio.volume + 0.1);
                break;
            case 'ArrowDown':
                e.preventDefault();
                this.audio.volume = Math.max(0, this.audio.volume - 0.1);
                break;
            case 'KeyM':
                this.toggleMute();
                break;
            case 'KeyN':
                this.nextTrack();
                break;
            case 'KeyP':
                this.prevTrack();
                break;
        }
    }

    saveState() {
        // Save current track position to trackPositions map
        this.saveCurrentTrackPosition();

        const state = {
            bookId: bookId,
            trackIndex: this.currentTrackIndex,
            position: this.audio.currentTime,
            trackPositions: this.trackPositions,
            timestamp: Date.now()
        };
        setCookie('audioState_' + bookId, JSON.stringify(state), 30);
    }

    getSavedState() {
        const saved = getCookie('audioState_' + bookId);
        if (saved) {
            try {
                return JSON.parse(saved);
            } catch (e) {
                return null;
            }
        }
        return null;
    }

    loadState() {
        const state = this.getSavedState();
        if (state && state.bookId === bookId) {
            // Restore per-track positions
            if (state.trackPositions) {
                this.trackPositions = state.trackPositions;
            }

            if (state.trackIndex >= 0 && state.trackIndex < this.tracks.length) {
                // Show player bar and highlight track
                this.currentTrackIndex = state.trackIndex;
                this.loadTrack(state.trackIndex);
                // Don't auto-play, just set position
                this.audio.currentTime = state.position || 0;
            }
        }
    }
}

// Initialize player
const player = new AudioPlayer();

// Header is now position:sticky via CSS, no JS needed

// Selection functions (unchanged)
function updateSelection() {
    const checkboxes = document.querySelectorAll('.track-select:checked');
    const btn = document.getElementById('downloadSelectedBtn');
    const text = document.getElementById('downloadSelectedText');
    btn.disabled = checkboxes.length === 0;
    text.textContent = checkboxes.length > 0 ? i18n.downloadsel + ' (' + checkboxes.length + ')' : i18n.downloadsel;

    const allCheckboxes = document.querySelectorAll('.track-select');
    const selectAll = document.getElementById('selectAll');
    if (checkboxes.length === 0) {
        selectAll.checked = false;
        selectAll.indeterminate = false;
    } else if (checkboxes.length === allCheckboxes.length) {
        selectAll.checked = true;
        selectAll.indeterminate = false;
    } else {
        selectAll.checked = false;
        selectAll.indeterminate = true;
    }

    document.querySelectorAll('.part-select').forEach(function(partCb) {
        const partIdx = partCb.dataset.part;
        const partTracks = document.querySelectorAll('.track-select[data-part="' + partIdx + '"]');
        const partChecked = document.querySelectorAll('.track-select[data-part="' + partIdx + '"]:checked');
        if (partChecked.length === 0) {
            partCb.checked = false;
            partCb.indeterminate = false;
        } else if (partChecked.length === partTracks.length) {
            partCb.checked = true;
            partCb.indeterminate = false;
        } else {
            partCb.checked = false;
            partCb.indeterminate = true;
        }
    });
}

function toggleSelectAll() {
    const selectAll = document.getElementById('selectAll');
    document.querySelectorAll('.track-select').forEach(function(cb) {
        cb.checked = selectAll.checked;
    });
    updateSelection();
}

function togglePartSelect(partIdx) {
    const partCb = document.querySelector('.part-select[data-part="' + partIdx + '"]');
    document.querySelectorAll('.track-select[data-part="' + partIdx + '"]').forEach(function(cb) {
        cb.checked = partCb.checked;
    });
    updateSelection();
}

function downloadSelected() {
    const checkboxes = document.querySelectorAll('.track-select:checked');
    checkboxes.forEach(function(cb) {
        const path = cb.dataset.path;
        const url = webPrefix + '/audio/' + bookId + '/track?file=' + encodeURIComponent(path);
        const a = document.createElement('a');
        a.href = url;
        a.download = '';
        a.style.display = 'none';
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
    });
}
</script>

<style>
.audio-detail {
    max-width: 1000px;
    margin: 0 auto;
}
.audio-header {
    display: flex;
    justify-content: flex-start;
    align-items: flex-start;
    gap: 20px;
    margin-bottom: 20px;
    padding: 20px;
    background: var(--card-bg);
    border-radius: 12px;
    flex-wrap: wrap;
    position: sticky;
    top: 10px;
    z-index: 100;
    box-shadow: 0 4px 20px rgba(0,0,0,0.2);
}
.audio-cover {
    width: 150px;
    height: 150px;
    object-fit: cover;
    border-radius: 8px;
    box-shadow: 0 4px 12px rgba(0,0,0,0.3);
    flex-shrink: 0;
}
.audio-info {
    flex: 1;
    min-width: 200px;
}
.audio-info h1 {
    margin: 0 0 10px 0;
    font-size: 1.8rem;
}
.audio-authors {
    font-size: 1.2rem;
    margin-bottom: 10px;
}
.audio-authors a {
    color: var(--primary);
    text-decoration: none;
}
.audio-authors a:hover {
    text-decoration: underline;
}
.now-playing {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 1rem;
    color: var(--success);
    background: rgba(40, 167, 69, 0.15);
    padding: 6px 12px;
    border-radius: 6px;
    margin-bottom: 10px;
}
.now-playing.hidden {
    display: none;
}
.now-playing .pulse {
    animation: pulse 1.5s ease-in-out infinite;
}
@keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
}
.audio-meta {
    display: flex;
    gap: 15px;
    flex-wrap: wrap;
    color: var(--gray);
}
.audio-meta span {
    display: flex;
    align-items: center;
    gap: 5px;
}
.badge {
    padding: 4px 10px;
    border-radius: 20px;
    font-size: 0.85rem;
    font-weight: 500;
}
.badge-collection {
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
    color: white;
}
.badge-book {
    background: linear-gradient(135deg, #11998e 0%, #38ef7d 100%);
    color: white;
}
.audio-actions {
    display: flex;
    flex-direction: column;
    gap: 10px;
}
.btn-lg {
    padding: 12px 24px;
    font-size: 1.1rem;
}
.audio-structure h2 {
    margin: 0 0 15px 0;
    font-size: 1.4rem;
}
.audio-parts {
    display: flex;
    flex-direction: column;
    gap: 10px;
}
.audio-part {
    background: var(--card-bg);
    border-radius: 8px;
    overflow: hidden;
}
.audio-part summary {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 15px;
    cursor: pointer;
    user-select: none;
    font-weight: 500;
}
.audio-part summary:hover {
    background: rgba(255,255,255,0.05);
}
.audio-part[open] summary {
    border-bottom: 1px solid var(--border);
}
.part-name {
    flex: 1;
}
.part-meta {
    display: flex;
    gap: 15px;
    color: var(--gray);
    font-weight: normal;
    font-size: 0.9rem;
}
.track-list {
    list-style: none;
    margin: 0;
    padding: 0;
}
.track-list li {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 15px 8px 35px;
    border-bottom: 1px solid var(--border);
    transition: background 0.2s;
    cursor: pointer;
}
.track-list li:hover {
    background: rgba(255,255,255,0.05);
}
.track-list li:last-child {
    border-bottom: none;
}
.track-list li.playing {
    background: rgba(102, 126, 234, 0.15);
}
.track-list li.playing .track-icon {
    color: var(--primary);
    animation: pulse 1.5s ease-in-out infinite;
}
@keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
}
.track-list li i {
    color: var(--gray);
    font-size: 0.85rem;
}
.track-name {
    flex: 1;
}
.track-duration {
    color: var(--gray);
    font-size: 0.9rem;
}
.track-list.flat {
    background: var(--card-bg);
    border-radius: 8px;
}
.track-list.flat li {
    padding-left: 15px;
}
.select-controls {
    display: flex;
    align-items: center;
    gap: 15px;
    margin-bottom: 15px;
    padding: 10px 15px;
    background: var(--card-bg);
    border-radius: 8px;
}
.select-all-label {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-weight: 500;
}
.select-all-label input[type="checkbox"] {
    width: 18px;
    height: 18px;
    cursor: pointer;
}
.track-checkbox, .part-checkbox {
    width: 16px;
    height: 16px;
    cursor: pointer;
    flex-shrink: 0;
}
.track-play {
    background: none;
    border: none;
    color: var(--success);
    padding: 5px 8px;
    border-radius: 4px;
    cursor: pointer;
    transition: background 0.2s;
}
.track-play:hover {
    background: var(--success);
    color: white;
}
.track-download {
    color: var(--primary);
    padding: 5px;
    border-radius: 4px;
    transition: background 0.2s;
}
.track-download:hover {
    background: var(--primary);
    color: white;
}
.part-header label {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
}

/* Player Bar */
.player-bar {
    position: sticky;
    bottom: 0;
    background: linear-gradient(135deg, #1a1a2e 0%, #16213e 100%);
    border-radius: 12px;
    margin-top: 20px;
    box-shadow: 0 4px 20px rgba(0,0,0,0.3);
    overflow: hidden;
}
.player-bar.hidden {
    display: none;
}
.player-progress-container {
    height: 6px;
    background: rgba(255,255,255,0.1);
    cursor: pointer;
    position: relative;
}
.player-progress-container:hover {
    height: 10px;
}
.player-progress-container.seeking {
    height: 10px;
}
.player-progress-bar {
    position: absolute;
    top: 0;
    left: 0;
    height: 100%;
    background: linear-gradient(90deg, #667eea 0%, #764ba2 100%);
    width: 0%;
    z-index: 2;
    transition: width 0.1s linear;
}
.player-progress-buffered {
    position: absolute;
    top: 0;
    left: 0;
    height: 100%;
    background: rgba(255,255,255,0.2);
    width: 0%;
    z-index: 1;
}
.player-content {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 20px;
    gap: 20px;
}
.player-track-info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
}
.player-track-name {
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: #fff;
}
.player-track-time {
    font-size: 0.85rem;
    color: var(--gray);
}
.player-controls {
    display: flex;
    align-items: center;
    gap: 8px;
}
.player-btn {
    background: none;
    border: none;
    color: #fff;
    width: 40px;
    height: 40px;
    border-radius: 50%;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: all 0.2s;
    position: relative;
}
.player-btn:hover {
    background: rgba(255,255,255,0.1);
    transform: scale(1.1);
}
.player-btn-main {
    width: 50px;
    height: 50px;
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
    font-size: 1.2rem;
}
.player-btn-main:hover {
    background: linear-gradient(135deg, #7b8eee 0%, #8a5eb5 100%);
}
.player-btn-main.loading {
    animation: spin 1s linear infinite;
}
@keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
}
.player-btn .btn-label {
    position: absolute;
    font-size: 0.6rem;
    bottom: 5px;
    font-weight: bold;
}
.player-extras {
    display: flex;
    align-items: center;
    gap: 8px;
}
.speed-btn {
    width: auto !important;
    padding: 0 12px;
    font-weight: bold;
    font-size: 0.9rem;
    border-radius: 20px !important;
}

/* Mobile responsive */
@media (max-width: 600px) {
    .player-content {
        padding: 8px 12px;
        gap: 10px;
    }
    .player-track-info {
        max-width: 100px;
    }
    .player-btn {
        width: 36px;
        height: 36px;
    }
    .player-btn-main {
        width: 44px;
        height: 44px;
    }
    #btnRewind, #btnForward {
        display: none;
    }
    .player-extras {
        display: none;
    }
}
</style>
{{end}}`,

	"bookshelf": `{{define "content"}}
<div style="display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 15px; margin-bottom: 20px;">
    <h2 style="margin: 0;"><i class="fas fa-bookmark"></i> {{t "bookshelf.title"}} {{if .TotalCount}}(<span id="bookshelf-count">{{.TotalCount}}</span> {{t "main.books"}}){{end}}</h2>
    <div class="page-size-selector">
        <span style="color: var(--gray); margin-right: 10px;">{{t "books.show"}}</span>
        <a href="{{.CurrentPath}}?size=10" class="size-btn{{if eq .PageSize 10}} active{{end}}">10</a>
        <a href="{{.CurrentPath}}?size=50" class="size-btn{{if eq .PageSize 50}} active{{end}}">50</a>
        <a href="{{.CurrentPath}}?size=100" class="size-btn{{if eq .PageSize 100}} active{{end}}">100</a>
        <a href="{{.CurrentPath}}?size=200" class="size-btn{{if eq .PageSize 200}} active{{end}}">200</a>
        <a href="{{.CurrentPath}}?size=all" class="size-btn{{if eq .PageSize 0}} active{{end}}">{{t "books.all"}}</a>
    </div>
</div>
{{if .Books}}
<div class="book-grid">
{{range .Books}}
    <div class="book-card">
        <div class="book-card-content">
            {{if .HasCover}}<img src="{{$.OPDSPrefix}}/book/{{.ID}}/cover" class="book-cover" alt="Cover" onerror="this.style.display='none'">{{end}}
            <div class="book-info">
                <div class="book-title">{{if .IsAudiobook}}<i class="fas fa-headphones" style="color: var(--primary);"></i> {{end}}{{.Title}}</div>
                <div class="book-meta">
                    {{if .Authors}}<span><i class="fas fa-user"></i> {{range $i, $a := .Authors}}{{if lt $i 2}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/authors/{{$a.ID}}" class="meta-link">{{$a.Name}}</a>{{end}}{{end}}{{if gt (len .Authors) 2}} <span class="more-info">+{{sub (len .Authors) 2}}<div class="more-info-tooltip{{if gt (len .Authors) 70}} below{{end}}">{{range $i, $a := .Authors}}{{if ge $i 2}}<a href="{{$.WebPrefix}}/authors/{{$a.ID}}">{{$a.Name}}</a>{{end}}{{end}}</div></span>{{end}}</span>{{end}}
                    {{if .Narrators}}<span><i class="fas fa-microphone"></i> {{range $i, $n := .Narrators}}{{if lt $i 2}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/authors/{{$n.ID}}" class="meta-link">{{$n.Name}}</a>{{end}}{{end}}{{if gt (len .Narrators) 2}} <span class="more-info">+{{sub (len .Narrators) 2}}</span>{{end}}</span>{{end}}
                    {{if .Series}}<span><i class="fas fa-layer-group"></i> {{range $i, $s := .Series}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/series/{{$s.ID}}" class="meta-link">{{$s.Name}}</a>{{end}}</span>{{end}}
                    {{if .Lang}}<span><i class="fas fa-globe"></i> <a href="{{$.WebPrefix}}/languages/{{.Lang}}" class="meta-link">{{.LangName}}</a></span>{{end}}
                    <span><i class="fas fa-file"></i> {{.Format}}</span>
                    {{if .Duration}}<span><i class="fas fa-clock"></i> {{.Duration}}</span>{{else}}<span><i class="fas fa-weight-hanging"></i> {{.Size}}</span>{{end}}
                    {{if gt .TrackCount 1}}<a href="{{$.WebPrefix}}/audio/{{.ID}}" class="meta-link"><i class="fas fa-list-ol"></i> {{.TrackCount}} {{t "audio.tracks"}}</a>{{end}}
                </div>
                {{if .Genres}}<div class="book-meta"><span><i class="fas fa-tag"></i> {{range $i, $g := .Genres}}{{if lt $i 3}}{{if $i}}, {{end}}<a href="{{$.WebPrefix}}/genres/{{$g.ID}}" class="meta-link">{{$g.Name}}</a>{{end}}{{end}}{{if gt (len .Genres) 3}} <span class="more-info">+{{sub (len .Genres) 3}}<div class="more-info-tooltip{{if gt (len .Genres) 71}} below{{end}}">{{range $i, $g := .Genres}}{{if ge $i 3}}<a href="{{$.WebPrefix}}/genres/{{$g.ID}}">{{$g.Name}}</a>{{end}}{{end}}</div></span>{{end}}</span></div>{{end}}
                {{if .Annotation}}<div class="book-annotation">{{.Annotation}}</div>{{end}}
            </div>
        </div>
        <div class="book-actions">
            {{if .IsAudiobook}}<a href="{{$.WebPrefix}}/audio/{{.ID}}" class="btn btn-primary"><i class="fas fa-headphones"></i> {{if gt .TrackCount 1}}{{.TrackCount}} {{t "audio.tracks"}}{{else}}{{t "audio.book"}}{{end}}</a>{{else}}<a href="{{$.OPDSPrefix}}/book/{{.ID}}/download" class="btn btn-primary"><i class="fas fa-download"></i> {{.Format}}</a>{{end}}
            {{if or (eq .Format "FB2") (eq .Format "EPUB") (eq .Format "MOBI")}}<a href="{{$.WebPrefix}}/read/{{.ID}}" class="btn btn-info"><i class="fas fa-book-open"></i> {{t "reader.read"}}</a>{{end}}
            {{if and $.HasEPUB .CanEPUB}}<a href="{{$.OPDSPrefix}}/book/{{.ID}}/epub" class="btn btn-success"><i class="fas fa-file-arrow-down"></i> EPUB</a>{{end}}
            {{if and $.HasMOBI .CanMOBI}}<a href="{{$.OPDSPrefix}}/book/{{.ID}}/mobi" class="btn btn-warning"><i class="fas fa-file-arrow-down"></i> MOBI</a>{{end}}
            {{if or (gt .DuplicateCount 0) (gt .DuplicateOf 0)}}<a href="{{$.WebPrefix}}/duplicates/{{.ID}}" class="btn btn-secondary"><i class="fas fa-copy"></i> {{t "books.duplicates"}}{{if gt .DuplicateCount 0}} ({{.DuplicateCount}}){{end}}</a>{{end}}
            <a href="#" onclick="return bookshelfAction(this, '{{$.WebPrefix}}/bookshelf/remove/{{.ID}}')" class="btn btn-danger"><i class="fas fa-trash"></i> {{t "bookshelf.remove"}}</a>
        </div>
    </div>
{{end}}
</div>
{{if or (gt .Page 0) .HasMore}}
<div class="pagination">
    {{if gt .Page 0}}<a href="{{.CurrentPath}}?page={{.PrevPage}}{{if .PageSize}}&size={{.PageSize}}{{end}}"><i class="fas fa-arrow-left"></i> {{t "books.prev"}}</a>{{end}}
    {{if .HasMore}}<a href="{{.CurrentPath}}?page={{.NextPage}}{{if .PageSize}}&size={{.PageSize}}{{end}}">{{t "books.next"}} <i class="fas fa-arrow-right"></i></a>{{end}}
</div>
{{end}}
{{else}}
<p style="text-align: center; color: var(--gray); padding: 40px;">{{t "bookshelf.empty"}}</p>
{{end}}
{{end}}`,

	"authors_index": `{{define "content"}}
<h2><i class="fas fa-user-pen"></i> {{t "authors.title"}}{{if .Prefix}}: {{.Prefix}}{{end}}</h2>
{{if .Prefixes}}
<div class="prefix-cloud">
{{range .Prefixes}}
    <a href="{{$.WebPrefix}}/authors?prefix={{.}}">{{.}}</a>
{{end}}
</div>
{{else}}
<p style="text-align: center; color: var(--gray);">{{t "authors.none"}}</p>
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
    {{if gt .Page 0}}<a href="{{.CurrentPath}}?prefix={{.Prefix}}&page={{.PrevPage}}"><i class="fas fa-arrow-left"></i> {{t "books.prev"}}</a>{{end}}
    {{if .HasMore}}<a href="{{.CurrentPath}}?prefix={{.Prefix}}&page={{.NextPage}}">{{t "books.next"}} <i class="fas fa-arrow-right"></i></a>{{end}}
</div>
{{end}}
{{else}}
<p style="text-align: center; color: var(--gray);">{{t "authors.none"}}</p>
{{end}}
{{end}}`,

	"genres_index": `{{define "content"}}
<h2><i class="fas fa-masks-theater"></i> {{t "genres.title"}}</h2>
{{if .Sections}}
<div class="sections-grid">
{{range .Sections}}
    <a href="{{$.WebPrefix}}/genres?section={{.}}" class="section-card"><i class="fas fa-folder"></i> {{.}}</a>
{{end}}
</div>
{{else}}
<p style="text-align: center; color: var(--gray);">{{t "genres.none"}}</p>
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
<p style="text-align: center; color: var(--gray);">{{t "genres.none"}}</p>
{{end}}
{{end}}`,

	"series_index": `{{define "content"}}
<h2><i class="fas fa-layer-group"></i> {{t "series.title"}}{{if .Prefix}}: {{.Prefix}}{{end}}</h2>
{{if .Prefixes}}
<div class="prefix-cloud">
{{range .Prefixes}}
    <a href="{{$.WebPrefix}}/series?prefix={{.}}">{{.}}</a>
{{end}}
</div>
{{else}}
<p style="text-align: center; color: var(--gray);">{{t "series.none"}}</p>
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
    {{if gt .Page 0}}<a href="{{.CurrentPath}}?prefix={{.Prefix}}&page={{.PrevPage}}"><i class="fas fa-arrow-left"></i> {{t "books.prev"}}</a>{{end}}
    {{if .HasMore}}<a href="{{.CurrentPath}}?prefix={{.Prefix}}&page={{.NextPage}}">{{t "books.next"}} <i class="fas fa-arrow-right"></i></a>{{end}}
</div>
{{end}}
{{else}}
<p style="text-align: center; color: var(--gray);">{{t "series.none"}}</p>
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
    {{if gt .Page 0}}<a href="{{.CurrentPath}}?page={{.PrevPage}}"><i class="fas fa-arrow-left"></i> {{t "books.prev"}}</a>{{end}}
    {{if .HasMore}}<a href="{{.CurrentPath}}?page={{.NextPage}}">{{t "books.next"}} <i class="fas fa-arrow-right"></i></a>{{end}}
</div>
{{end}}
{{else}}
<p style="text-align: center; color: var(--gray);">{{t "catalogs.none"}}</p>
{{end}}
{{end}}`,

	"languages": `{{define "content"}}
<h2><i class="fas fa-globe"></i> {{t "languages.title"}}</h2>
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
<p style="text-align: center; color: var(--gray);">{{t "languages.none"}}</p>
{{end}}
{{end}}`,

	"error": `{{define "content"}}
<h2><i class="fas fa-exclamation-triangle" style="color: var(--danger);"></i> {{t "error.title"}}</h2>
<p style="color: var(--danger); padding: 20px; background: #fef2f2; border-radius: 10px;">{{.Error}}</p>
<a href="{{.WebPrefix}}/" class="btn btn-primary" style="margin-top: 20px;"><i class="fas fa-home"></i> {{t "error.back"}}</a>
{{end}}`,

	"help": `{{define "content"}}
<div class="help-page">
<h2><i class="fas fa-circle-question"></i> {{t "help.welcome"}}</h2>
<p class="intro">{{t "help.intro"}}</p>

<section>
    <h3><i class="fas fa-search"></i> {{t "help.search.title"}}</h3>
    <p>{{t "help.search.p1"}}</p>
    <ul>
        <li><strong>{{t "search.title"}}</strong> — {{t "help.search.field1"}}</li>
        <li><strong>{{t "search.author"}}</strong> — {{t "help.search.field2"}}</li>
    </ul>
    <p>{{t "help.search.p2"}}</p>
</section>

<section>
    <h3><i class="fas fa-crosshairs"></i> {{t "help.scope.title"}}</h3>
    <p>{{t "help.scope.p1"}}</p>
</section>

<section>
    <h3><i class="fas fa-filter"></i> {{t "help.filters.title"}}</h3>
    <p>{{t "help.filters.p1"}}</p>
    <ul class="code-list">
        <li><code>{{t "help.filters.lang"}}</code></li>
        <li><code>{{t "help.filters.genre"}}</code></li>
        <li><code>{{t "help.filters.series"}}</code></li>
    </ul>
</section>

<section>
    <h3><i class="fas fa-compass"></i> {{t "help.browse.title"}}</h3>
    <p>{{t "help.browse.p1"}}</p>
    <ul>
        <li><i class="fas fa-folder"></i> <strong>{{t "nav.catalogs"}}</strong> — {{t "help.browse.cat"}}</li>
        <li><i class="fas fa-user-pen"></i> <strong>{{t "nav.authors"}}</strong> — {{t "help.browse.auth"}}</li>
        <li><i class="fas fa-tags"></i> <strong>{{t "nav.genres"}}</strong> — {{t "help.browse.genre"}}</li>
        <li><i class="fas fa-layer-group"></i> <strong>{{t "nav.series"}}</strong> — {{t "help.browse.series"}}</li>
        <li><i class="fas fa-globe"></i> <strong>{{t "nav.languages"}}</strong> — {{t "help.browse.lang"}}</li>
    </ul>
</section>

<section>
    <h3><i class="fas fa-download"></i> {{t "help.download.title"}}</h3>
    <p>{{t "help.download.p1"}}</p>
</section>

<section>
    <h3><i class="fas fa-rss"></i> {{t "help.opds.title"}}</h3>
    <p>{{t "help.opds.p1"}}</p>
    <code class="url">{{.OPDSPrefix}}/</code>
</section>

<section>
    <h3><i class="fas fa-bookmark"></i> {{t "help.bookshelf.title"}}</h3>
    <p>{{t "help.bookshelf.p1"}}</p>
</section>
</div>

<style>
.help-page { max-width: 800px; margin: 0 auto; }
.help-page .intro { font-size: 1.1rem; color: var(--gray); margin-bottom: 30px; }
.help-page section { background: white; padding: 20px 25px; border-radius: 12px; margin-bottom: 20px; box-shadow: var(--shadow); }
.help-page h3 { color: var(--primary); margin-bottom: 15px; display: flex; align-items: center; gap: 10px; }
.help-page ul { margin: 15px 0; padding-left: 25px; }
.help-page li { margin: 8px 0; line-height: 1.6; }
.help-page .code-list { list-style: none; padding-left: 0; }
.help-page .code-list li { background: #f8fafc; padding: 8px 15px; border-radius: 6px; margin: 5px 0; }
.help-page code { background: #f1f5f9; padding: 2px 8px; border-radius: 4px; font-family: monospace; }
.help-page code.url { display: block; background: var(--dark); color: #22c55e; padding: 12px 15px; border-radius: 8px; margin-top: 10px; }
</style>
{{end}}`,
}

// Reader template - standalone page for reading books
const readerTemplate = `<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{.BookTitle}} - {{.SiteTitle}}</title>
    <link rel="icon" type="image/svg+xml" href="/favicon.svg">
    <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.5.1/css/all.min.css">
    <style>
        :root {
            --font-size: 18px;
            --line-height: 1.8;
            --bg: #fefefe;
            --text: #1a1a1a;
            --header-bg: #ffffff;
            --toc-bg: #f8f9fa;
            --toc-hover: #e9ecef;
            --border: #dee2e6;
            --primary: #6366f1;
            --shadow: 0 2px 8px rgba(0,0,0,0.1);
        }
        .dark-mode {
            --bg: #1a1a1a;
            --text: #e0e0e0;
            --header-bg: #2d2d2d;
            --toc-bg: #252525;
            --toc-hover: #333333;
            --border: #404040;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        html { scroll-behavior: smooth; }
        body {
            font-family: Georgia, 'Times New Roman', serif;
            font-size: var(--font-size);
            line-height: var(--line-height);
            background: var(--bg);
            color: var(--text);
            min-height: 100vh;
        }

        /* Header */
        .reader-header {
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            height: 50px;
            background: var(--header-bg);
            border-bottom: 1px solid var(--border);
            display: flex;
            align-items: center;
            padding: 0 15px;
            gap: 15px;
            z-index: 1000;
            box-shadow: var(--shadow);
        }
        .toc-toggle {
            background: none;
            border: none;
            font-size: 1.2rem;
            cursor: pointer;
            color: var(--text);
            padding: 8px;
            border-radius: 6px;
        }
        .toc-toggle:hover { background: var(--toc-hover); }
        .book-info {
            flex: 1;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }
        .book-title-header {
            font-size: 1rem;
            font-weight: 600;
        }
        .book-authors {
            font-size: 0.85rem;
            color: #666;
        }
        .dark-mode .book-authors { color: #999; }
        .reader-controls {
            display: flex;
            gap: 8px;
        }
        .reader-controls button, .reader-controls a {
            background: none;
            border: 1px solid var(--border);
            padding: 6px 12px;
            border-radius: 6px;
            cursor: pointer;
            font-size: 0.9rem;
            color: var(--text);
            text-decoration: none;
            display: flex;
            align-items: center;
            gap: 5px;
        }
        .reader-controls button:hover, .reader-controls a:hover {
            background: var(--toc-hover);
        }
        .close-btn { color: #dc3545 !important; }

        /* TOC Sidebar */
        .reader-toc {
            position: fixed;
            top: 50px;
            left: 0;
            width: 300px;
            height: calc(100vh - 50px);
            background: var(--toc-bg);
            border-right: 1px solid var(--border);
            overflow-y: auto;
            transform: translateX(-100%);
            transition: transform 0.3s ease;
            z-index: 999;
        }
        .reader-toc.open { transform: translateX(0); }
        .toc-header {
            padding: 15px;
            font-weight: 600;
            border-bottom: 1px solid var(--border);
            position: sticky;
            top: 0;
            background: var(--toc-bg);
        }
        .toc-list {
            padding: 10px 0;
        }
        .toc-item {
            display: block;
            padding: 10px 15px;
            color: var(--text);
            text-decoration: none;
            border-left: 3px solid transparent;
            transition: all 0.2s;
        }
        .toc-item:hover {
            background: var(--toc-hover);
            border-left-color: var(--primary);
        }
        .toc-item.level-2 { padding-left: 25px; font-size: 0.95em; }
        .toc-item.level-3 { padding-left: 35px; font-size: 0.9em; }
        .toc-item.level-4 { padding-left: 45px; font-size: 0.85em; }

        /* Overlay for mobile */
        .toc-overlay {
            display: none;
            position: fixed;
            top: 50px;
            left: 0;
            right: 0;
            bottom: 0;
            background: rgba(0,0,0,0.5);
            z-index: 998;
        }
        .toc-overlay.open { display: block; }

        /* Content */
        .reader-content {
            max-width: 750px;
            margin: 0 auto;
            padding: 70px 20px 50px;
        }

        /* Typography */
        .reader-content h1, .reader-content h2, .reader-content h3,
        .reader-content h4, .reader-content h5, .reader-content h6 {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            margin: 1.5em 0 0.5em;
            line-height: 1.3;
        }
        .reader-content h1 { font-size: 1.8em; text-align: center; margin-top: 2em; }
        .reader-content h2 { font-size: 1.5em; }
        .reader-content h3 { font-size: 1.3em; }
        .reader-content h4 { font-size: 1.1em; }

        .reader-content p {
            margin: 0.8em 0;
            text-indent: 1.5em;
            text-align: justify;
        }
        .reader-content p:first-child { text-indent: 0; }

        .reader-content .section {
            margin-bottom: 1.5em;
        }

        .reader-content .epigraph {
            margin: 2em 0 2em 20%;
            font-style: italic;
            font-size: 0.95em;
        }
        .reader-content .epigraph p { text-indent: 0; text-align: right; }
        .reader-content .text-author {
            text-align: right;
            font-style: normal;
            margin-top: 0.5em;
        }

        .reader-content .poem {
            margin: 1.5em 0;
            padding-left: 2em;
        }
        .reader-content .stanza {
            margin-bottom: 1em;
        }
        .reader-content .verse {
            text-indent: 0;
            text-align: left;
            margin: 0.2em 0;
        }

        .reader-content blockquote {
            margin: 1.5em 2em;
            padding-left: 1em;
            border-left: 3px solid var(--border);
            font-style: italic;
        }

        .reader-content .subtitle {
            text-align: center;
            font-style: italic;
            margin: 1em 0;
        }

        .reader-content .book-image {
            display: block;
            max-width: 100%;
            height: auto;
            margin: 1.5em auto;
            border-radius: 4px;
        }

        .reader-content em { font-style: italic; }
        .reader-content strong { font-weight: bold; }
        .reader-content a { color: var(--primary); }

        .empty-line { display: block; height: 1em; }

        /* Mobile */
        @media (max-width: 768px) {
            .reader-toc { width: 280px; }
            .reader-content { padding: 60px 15px 40px; }
            .book-info { display: none; }
            .reader-controls button span { display: none; }
        }
    </style>
</head>
<body>
    <header class="reader-header">
        <button class="toc-toggle" onclick="toggleTOC()" title="{{t "reader.toc"}}">
            <i class="fas fa-bars"></i>
        </button>
        <div class="book-info">
            <div class="book-title-header">{{.BookTitle}}</div>
            <div class="book-authors">{{.Authors}}</div>
        </div>
        <div class="reader-controls">
            <button onclick="decreaseFontSize()" title="{{t "reader.font_smaller"}}">
                <i class="fas fa-minus"></i> <span>A</span>
            </button>
            <button onclick="increaseFontSize()" title="{{t "reader.font_larger"}}">
                <i class="fas fa-plus"></i> <span>A</span>
            </button>
            <button onclick="toggleDarkMode()" title="{{t "reader.dark_mode"}}">
                <i class="fas fa-moon"></i>
            </button>
            <a href="{{.BackURL}}" class="close-btn" title="{{t "reader.close"}}">
                <i class="fas fa-times"></i>
            </a>
        </div>
    </header>

    <div class="toc-overlay" onclick="toggleTOC()"></div>

    <aside class="reader-toc">
        <div class="toc-header">{{t "reader.toc"}}</div>
        <nav class="toc-list">
            {{range .TOC}}
            <a href="#{{.Anchor}}" class="toc-item level-{{.Level}}" onclick="closeTOCMobile()">{{.Title}}</a>
            {{end}}
        </nav>
    </aside>

    <main class="reader-content">
        {{safeHTML .Content}}
    </main>

    <script>
    // Font size
    let fontSize = parseInt(localStorage.getItem('readerFontSize')) || 18;
    document.documentElement.style.setProperty('--font-size', fontSize + 'px');

    function increaseFontSize() {
        if (fontSize < 28) {
            fontSize += 2;
            document.documentElement.style.setProperty('--font-size', fontSize + 'px');
            localStorage.setItem('readerFontSize', fontSize);
        }
    }

    function decreaseFontSize() {
        if (fontSize > 12) {
            fontSize -= 2;
            document.documentElement.style.setProperty('--font-size', fontSize + 'px');
            localStorage.setItem('readerFontSize', fontSize);
        }
    }

    // Dark mode
    if (localStorage.getItem('readerDarkMode') === 'true') {
        document.body.classList.add('dark-mode');
    }

    function toggleDarkMode() {
        document.body.classList.toggle('dark-mode');
        localStorage.setItem('readerDarkMode', document.body.classList.contains('dark-mode'));
    }

    // TOC
    function toggleTOC() {
        document.querySelector('.reader-toc').classList.toggle('open');
        document.querySelector('.toc-overlay').classList.toggle('open');
    }

    function closeTOCMobile() {
        if (window.innerWidth <= 768) {
            document.querySelector('.reader-toc').classList.remove('open');
            document.querySelector('.toc-overlay').classList.remove('open');
        }
    }

    // Keyboard shortcuts
    document.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') {
            document.querySelector('.reader-toc').classList.remove('open');
            document.querySelector('.toc-overlay').classList.remove('open');
        }
    });
    </script>
</body>
</html>`
