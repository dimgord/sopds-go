package database

import (
	"time"
)

// CatType represents the type of catalog entry
type CatType int

const (
	CatNormal CatType = 0 // Regular file
	CatZip    CatType = 1 // File in ZIP archive
	CatGz     CatType = 2 // File in GZIP archive
)

// Avail represents the availability status of a book
type Avail int

const (
	AvailDeleted  Avail = 0 // Deleted (logical or physical)
	AvailPending  Avail = 1 // Pending verification in current scan
	AvailVerified Avail = 2 // Verified available
)

// DuplicateMode represents the duplicate detection strategy
type DuplicateMode int

const (
	DupNone   DuplicateMode = 0 // No duplicate detection
	DupNormal DuplicateMode = 1 // By title + authors
	DupStrong DuplicateMode = 2 // By title + format + filesize
	DupClear  DuplicateMode = 3 // Clear all duplicate markers
)

// Book represents a book in the catalog
type Book struct {
	ID           int64     `db:"book_id"`
	Filename     string    `db:"filename"`
	Path         string    `db:"path"`
	Format       string    `db:"format"`
	Filesize     int64     `db:"filesize"`
	Title        string    `db:"title"`
	Annotation   string    `db:"annotation"`
	Lang         string    `db:"lang"`
	DocDate      string    `db:"docdate"`
	RegisterDate time.Time `db:"registerdate"`
	CatID        int64     `db:"cat_id"`
	CatType      CatType   `db:"cat_type"`
	Avail        Avail     `db:"avail"`
	DuplicateOf  *int64    `db:"duplicate_of"`
	Cover        string    `db:"cover"`
	CoverType    string    `db:"cover_type"`
	Favorite     bool      `db:"favorite"`
	// Audiobook fields
	DurationSeconds int    `db:"duration_seconds"`
	Bitrate         int    `db:"bitrate"`
	IsAudiobook     bool   `db:"is_audiobook"`
	TrackCount      int    `db:"track_count"`
	Chapters        string `db:"chapters"` // JSON array
}

// Author represents an author
type Author struct {
	ID        int64  `db:"author_id"`
	FirstName string `db:"first_name"`
	LastName  string `db:"last_name"`
}

// FullName returns the author's full name
func (a *Author) FullName() string {
	if a.FirstName == "" {
		return a.LastName
	}
	if a.LastName == "" {
		return a.FirstName
	}
	return a.LastName + " " + a.FirstName
}

// Genre represents a book genre
type Genre struct {
	ID         int64  `db:"genre_id"`
	Genre      string `db:"genre"`
	Section    string `db:"section"`
	Subsection string `db:"subsection"`
}

// Series represents a book series
type Series struct {
	ID   int64  `db:"ser_id"`
	Name string `db:"ser"`
}

// BookSeries represents a book's membership in a series with order
type BookSeries struct {
	SeriesID int64  `db:"ser_id"`
	BookID   int64  `db:"book_id"`
	SerNo    int    `db:"ser_no"`
	Name     string `db:"ser"`
}

// Catalog represents a directory in the catalog tree
type Catalog struct {
	ID       int64   `db:"cat_id"`
	ParentID *int64  `db:"parent_id"`
	Name     string  `db:"cat_name"`
	Path     string  `db:"path"`
	CatType  CatType `db:"cat_type"`
}

// BookShelf represents a user's reading list entry
type BookShelf struct {
	User     string    `db:"user"`
	BookID   int64     `db:"book_id"`
	ReadTime time.Time `db:"readtime"`
}

// CatalogItem represents an item in catalog listing (either book or subcatalog)
type CatalogItem struct {
	ItemType   string    // "catalog" or "book"
	ID         int64
	Name       string
	Path       string
	Date       time.Time
	Title      string
	Annotation string
	DocDate    string
	Format     string
	Filesize   int64
	Cover      string
	CoverType  string
}

// LanguageInfo holds language statistics
type LanguageInfo struct {
	Code  string
	Count int64
}

// DBInfo holds database statistics
type DBInfo struct {
	BooksCount     int64
	AuthorsCount   int64
	CatalogsCount  int64
	GenresCount    int64
	SeriesCount    int64
	BookshelfCount int64
}

// NewInfo holds new content statistics
type NewInfo struct {
	NewBooks   int64
	NewAuthors int64
	NewGenres  int64
	NewSeries  int64
}

// Pagination holds pagination info
type Pagination struct {
	Page       int
	Limit      int
	TotalCount int64
	HasNext    bool
	HasPrev    bool
}

// NewPagination creates a new pagination with defaults
func NewPagination(page, limit int) *Pagination {
	if page < 0 {
		page = 0
	}
	if limit <= 0 {
		limit = 50
	}
	return &Pagination{
		Page:  page,
		Limit: limit,
	}
}

// Offset returns the SQL offset for this page
func (p *Pagination) Offset() int {
	return p.Page * p.Limit
}

// SearchFilters holds optional filters for book searches
type SearchFilters struct {
	Lang     *string
	AuthorID *int64
	GenreID  *int64
}
