package repository

import (
	"github.com/sopds/sopds-go/internal/domain/book"
)

// SortField defines what field to sort by
type SortField string

const (
	SortByTitle        SortField = "title"
	SortByAuthor       SortField = "author"
	SortByDate         SortField = "date"         // register date
	SortBySize         SortField = "size"         // filesize
	SortByFormat       SortField = "format"
	SortByLanguage     SortField = "language"
	SortByName         SortField = "name"         // for authors, series, genres
	SortByBookCount    SortField = "book_count"   // for authors, series, genres
	SortBySeriesNumber SortField = "series_number" // for books in series
)

// SortDirection defines ascending or descending order
type SortDirection string

const (
	SortAsc  SortDirection = "asc"
	SortDesc SortDirection = "desc"
)

// Sort defines sorting parameters
type Sort struct {
	Field     SortField
	Direction SortDirection
}

// NewSort creates a new Sort with defaults
func NewSort(field SortField, direction SortDirection) Sort {
	if field == "" {
		field = SortByTitle
	}
	if direction == "" {
		direction = SortAsc
	}
	return Sort{Field: field, Direction: direction}
}

// DefaultSort returns default sorting (by title ascending)
func DefaultSort() Sort {
	return Sort{Field: SortByTitle, Direction: SortAsc}
}

// ByDateDesc returns sorting by date descending (newest first)
func ByDateDesc() Sort {
	return Sort{Field: SortByDate, Direction: SortDesc}
}

// BySizeDesc returns sorting by size descending (largest first)
func BySizeDesc() Sort {
	return Sort{Field: SortBySize, Direction: SortDesc}
}

// ByName returns sorting by name ascending
func ByName() Sort {
	return Sort{Field: SortByName, Direction: SortAsc}
}

// IsDescending returns true if sort direction is descending
func (s Sort) IsDescending() bool {
	return s.Direction == SortDesc
}

// BookFilters contains filters for book queries
// All filters are combined with AND logic
type BookFilters struct {
	// Text search
	Keywords           []string // Search in title (and annotation if SearchInAnnotation)
	SearchInAnnotation bool     // Also search in annotation (default false = title only)
	AuthorNameQuery    string   // Search in author first+last name (ILIKE)

	// Entity filters
	AuthorIDs  []int64 // Filter by author IDs (OR within, AND with others)
	GenreIDs   []int64 // Filter by genre IDs (OR within, AND with others)
	SeriesIDs  []int64 // Filter by series IDs (OR within, AND with others)
	CatalogIDs []int64 // Filter by catalog IDs (OR within, AND with others)

	// Value filters
	Languages []book.Language // Filter by languages (OR within)
	Formats   []book.Format   // Filter by formats (OR within)

	// Pattern filters (ILIKE matching)
	LangPattern   string // Filter by language pattern (e.g., "uk")
	GenrePattern  string // Filter by genre name pattern (e.g., "comedy")
	SeriesPattern string // Filter by series name pattern (e.g., "Silo")

	// Author name exact filters (from dropdown)
	AuthorFirstName string // Filter by author first_name (exact match)
	AuthorLastName  string // Filter by author last_name (exact match)

	// Range filters
	MinSize *int64 // Minimum filesize in bytes
	MaxSize *int64 // Maximum filesize in bytes

	// Date filters
	RegisteredAfter  *int // Days ago (e.g., 7 = last week)
	RegisteredBefore *int // Days ago

	// Status filters
	ShowDuplicates bool // Include duplicate books
	ShowDeleted    bool // Include deleted books
	FavoritesOnly  bool // Only show favorites

	// Type filters
	AudioOnly bool // Only show audiobooks

	// Prefix filter (for alphabetical navigation)
	TitlePrefix string // Title starts with this prefix
}

// NewBookFilters creates empty filters with sensible defaults
func NewBookFilters() *BookFilters {
	return &BookFilters{
		ShowDuplicates: false,
		ShowDeleted:    false,
		FavoritesOnly:  false,
	}
}

// WithKeywords adds keyword search (title only by default)
func (f *BookFilters) WithKeywords(keywords ...string) *BookFilters {
	f.Keywords = append(f.Keywords, keywords...)
	return f
}

// IncludeAnnotation also searches in annotation text
func (f *BookFilters) IncludeAnnotation() *BookFilters {
	f.SearchInAnnotation = true
	return f
}

// WithAuthorName adds author name search (first+last name ILIKE)
func (f *BookFilters) WithAuthorName(query string) *BookFilters {
	f.AuthorNameQuery = query
	return f
}

// WithLangPattern adds language pattern filter
func (f *BookFilters) WithLangPattern(pattern string) *BookFilters {
	f.LangPattern = pattern
	return f
}

// WithGenrePattern adds genre name pattern filter
func (f *BookFilters) WithGenrePattern(pattern string) *BookFilters {
	f.GenrePattern = pattern
	return f
}

// WithSeriesPattern adds series name pattern filter
func (f *BookFilters) WithSeriesPattern(pattern string) *BookFilters {
	f.SeriesPattern = pattern
	return f
}

// WithAuthorFirstName adds author first name filter (exact match)
func (f *BookFilters) WithAuthorFirstName(firstName string) *BookFilters {
	f.AuthorFirstName = firstName
	return f
}

// WithAuthorLastName adds author last name filter (exact match)
func (f *BookFilters) WithAuthorLastName(lastName string) *BookFilters {
	f.AuthorLastName = lastName
	return f
}

// WithAuthors adds author filter
func (f *BookFilters) WithAuthors(authorIDs ...int64) *BookFilters {
	f.AuthorIDs = append(f.AuthorIDs, authorIDs...)
	return f
}

// WithGenres adds genre filter
func (f *BookFilters) WithGenres(genreIDs ...int64) *BookFilters {
	f.GenreIDs = append(f.GenreIDs, genreIDs...)
	return f
}

// WithSeries adds series filter
func (f *BookFilters) WithSeries(seriesIDs ...int64) *BookFilters {
	f.SeriesIDs = append(f.SeriesIDs, seriesIDs...)
	return f
}

// WithCatalogs adds catalog filter
func (f *BookFilters) WithCatalogs(catalogIDs ...int64) *BookFilters {
	f.CatalogIDs = append(f.CatalogIDs, catalogIDs...)
	return f
}

// WithLanguages adds language filter
func (f *BookFilters) WithLanguages(langs ...book.Language) *BookFilters {
	f.Languages = append(f.Languages, langs...)
	return f
}

// WithFormats adds format filter
func (f *BookFilters) WithFormats(formats ...book.Format) *BookFilters {
	f.Formats = append(f.Formats, formats...)
	return f
}

// WithSizeRange adds filesize range filter
func (f *BookFilters) WithSizeRange(min, max *int64) *BookFilters {
	f.MinSize = min
	f.MaxSize = max
	return f
}

// WithNewPeriod filters to books registered within N days
func (f *BookFilters) WithNewPeriod(days int) *BookFilters {
	f.RegisteredAfter = &days
	return f
}

// WithTitlePrefix adds title prefix filter (for alphabetical navigation)
func (f *BookFilters) WithTitlePrefix(prefix string) *BookFilters {
	f.TitlePrefix = prefix
	return f
}

// IncludeDuplicates includes duplicate books in results
func (f *BookFilters) IncludeDuplicates() *BookFilters {
	f.ShowDuplicates = true
	return f
}

// OnlyFavorites filters to only favorite books
func (f *BookFilters) OnlyFavorites() *BookFilters {
	f.FavoritesOnly = true
	return f
}

// WithAudioOnly filters to only audiobooks
func (f *BookFilters) WithAudioOnly() *BookFilters {
	f.AudioOnly = true
	return f
}

// HasFilters returns true if any filters are set
func (f *BookFilters) HasFilters() bool {
	return len(f.Keywords) > 0 ||
		len(f.AuthorIDs) > 0 ||
		len(f.GenreIDs) > 0 ||
		len(f.SeriesIDs) > 0 ||
		len(f.CatalogIDs) > 0 ||
		len(f.Languages) > 0 ||
		len(f.Formats) > 0 ||
		f.MinSize != nil ||
		f.MaxSize != nil ||
		f.RegisteredAfter != nil ||
		f.RegisteredBefore != nil ||
		f.FavoritesOnly ||
		f.TitlePrefix != ""
}

// AuthorFilters contains filters for author queries
type AuthorFilters struct {
	Keywords   []string // Search in first/last name
	NamePrefix string   // Last name starts with prefix
	HasBooks   bool     // Only authors with available books
}

// NewAuthorFilters creates empty author filters
func NewAuthorFilters() *AuthorFilters {
	return &AuthorFilters{
		HasBooks: true, // Default to authors with books
	}
}

// WithKeywords adds keyword search
func (f *AuthorFilters) WithKeywords(keywords ...string) *AuthorFilters {
	f.Keywords = append(f.Keywords, keywords...)
	return f
}

// WithNamePrefix adds name prefix filter
func (f *AuthorFilters) WithNamePrefix(prefix string) *AuthorFilters {
	f.NamePrefix = prefix
	return f
}

// GenreFilters contains filters for genre queries
type GenreFilters struct {
	Section  string // Filter by section
	HasBooks bool   // Only genres with available books
}

// NewGenreFilters creates empty genre filters
func NewGenreFilters() *GenreFilters {
	return &GenreFilters{
		HasBooks: true,
	}
}

// WithSection adds section filter
func (f *GenreFilters) WithSection(section string) *GenreFilters {
	f.Section = section
	return f
}

// SeriesFilters contains filters for series queries
type SeriesFilters struct {
	Keywords   []string // Search in series name
	NamePrefix string   // Name starts with prefix
	HasBooks   bool     // Only series with available books
}

// NewSeriesFilters creates empty series filters
func NewSeriesFilters() *SeriesFilters {
	return &SeriesFilters{
		HasBooks: true,
	}
}

// WithKeywords adds keyword search
func (f *SeriesFilters) WithKeywords(keywords ...string) *SeriesFilters {
	f.Keywords = append(f.Keywords, keywords...)
	return f
}

// WithNamePrefix adds name prefix filter
func (f *SeriesFilters) WithNamePrefix(prefix string) *SeriesFilters {
	f.NamePrefix = prefix
	return f
}

// CatalogFilters contains filters for catalog queries
type CatalogFilters struct {
	ParentID *int64 // Filter by parent catalog
	TypeZip  *bool  // Filter by catalog type (nil = all, true = only ZIP, false = only directories)
}

// NewCatalogFilters creates empty catalog filters
func NewCatalogFilters() *CatalogFilters {
	return &CatalogFilters{}
}

// WithParent adds parent filter
func (f *CatalogFilters) WithParent(parentID int64) *CatalogFilters {
	f.ParentID = &parentID
	return f
}

// OnlyZip filters to only ZIP catalogs
func (f *CatalogFilters) OnlyZip() *CatalogFilters {
	t := true
	f.TypeZip = &t
	return f
}

// OnlyDirectories filters to only directory catalogs
func (f *CatalogFilters) OnlyDirectories() *CatalogFilters {
	t := false
	f.TypeZip = &t
	return f
}
