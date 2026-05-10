package repository

import (
	"context"

	"github.com/dimgord/sopds-go/internal/domain/book"
)

// DuplicateMode defines the duplicate detection strategy
type DuplicateMode int

const (
	DuplicateModeNone   DuplicateMode = 0 // No duplicate detection
	DuplicateModeNormal DuplicateMode = 1 // By title + authors
	DuplicateModeStrong DuplicateMode = 2 // By title + format + filesize
	DuplicateModeClear  DuplicateMode = 3 // Clear all duplicate markers
)

// LanguageStats contains language statistics
type LanguageStats struct {
	Code  string
	Count int64
}

// FormatStats contains format statistics
type FormatStats struct {
	Format string
	Count  int64
}

// BookRepository defines the interface for book persistence
type BookRepository interface {
	// --- CRUD Operations ---

	// FindByID retrieves a book by its ID
	FindByID(ctx context.Context, id book.ID) (*book.Book, error)

	// FindByFilenameAndPath finds a book by filename and path (unique constraint)
	FindByFilenameAndPath(ctx context.Context, filename, path string) (*book.Book, error)

	// GetBookMapByCatalog returns a map of path -> book_id for all books in a catalog
	// Used for efficient existence checking during ZIP scans
	GetBookMapByCatalog(ctx context.Context, catalogID int64) (map[string]int64, error)

	// Save persists a book (insert or update)
	Save(ctx context.Context, b *book.Book) error

	// SaveBatch persists multiple books in a single transaction
	SaveBatch(ctx context.Context, books []*book.Book) ([]book.ID, error)

	// Delete removes a book by ID
	Delete(ctx context.Context, id book.ID) error

	// --- Query Operations ---

	// Find returns books matching the query (filters + sort + pagination)
	Find(ctx context.Context, query *BookQuery) ([]*book.Book, error)

	// Count returns the total count of books matching filters
	Count(ctx context.Context, filters *BookFilters) (int64, error)

	// --- Relationship Loading ---

	// LoadAuthors loads authors for a book
	LoadAuthors(ctx context.Context, bookID book.ID) ([]book.AuthorRef, error)

	// LoadGenres loads genres for a book
	LoadGenres(ctx context.Context, bookID book.ID) ([]book.GenreRef, error)

	// LoadSeries loads series for a book
	LoadSeries(ctx context.Context, bookID book.ID) ([]book.SeriesRef, error)

	// LoadRelationships loads all relationships for a book
	LoadRelationships(ctx context.Context, bookID book.ID) ([]book.AuthorRef, []book.GenreRef, []book.SeriesRef, error)

	// --- Relationship Management ---

	// AddAuthor links a book to an author
	AddAuthor(ctx context.Context, bookID book.ID, authorID int64) error

	// AddAuthorsBatch links multiple book-author pairs
	AddAuthorsBatch(ctx context.Context, pairs [][2]int64) error

	// AddAuthorWithRole links a book to an author with a specific role (author/narrator)
	AddAuthorWithRole(ctx context.Context, bookID book.ID, authorID int64, role string) error

	// LoadNarrators loads narrators (authors with role='narrator') for a book
	LoadNarrators(ctx context.Context, bookID book.ID) ([]book.AuthorRef, error)

	// LoadAuthorsByRole loads authors with a specific role for a book
	LoadAuthorsByRole(ctx context.Context, bookID book.ID, role string) ([]book.AuthorRef, error)

	// AddGenre links a book to a genre
	AddGenre(ctx context.Context, bookID book.ID, genreID int64) error

	// AddSeries links a book to a series with order number
	AddSeries(ctx context.Context, bookID book.ID, seriesID int64, number int) error

	// --- Availability Management ---

	// UpdateAvailability changes a book's availability status
	UpdateAvailability(ctx context.Context, id book.ID, avail book.Availability) error

	// UpdateAvailabilityBatch updates availability for multiple books in a single query
	UpdateAvailabilityBatch(ctx context.Context, ids []int64, avail book.Availability) error

	// MarkAllPending marks all verified books as pending (for rescan)
	MarkAllPending(ctx context.Context) error

	// MarkCatalogVerified marks all pending books in a catalog as verified
	MarkCatalogVerified(ctx context.Context, catalogID int64) error

	// MarkCatalogsVerified marks all pending books in multiple catalogs as verified
	MarkCatalogsVerified(ctx context.Context, catalogIDs []int64) error

	// DeleteUnavailable deletes books that are still pending after scan
	// If physical is true, removes from database; otherwise marks as deleted
	DeleteUnavailable(ctx context.Context, physical bool) (int64, error)

	// DeleteInCatalogs deletes all books in the specified catalogs
	DeleteInCatalogs(ctx context.Context, catalogIDs []int64) (int64, error)

	// --- Duplicate Management ---

	// MarkDuplicates runs duplicate detection and marks duplicates
	MarkDuplicates(ctx context.Context, mode DuplicateMode) error

	// MarkDuplicatesIncremental marks duplicates only for newly added books
	// This is much faster than full MarkDuplicates for incremental scans
	// progressFn is called with (processed, total) counts for progress reporting
	MarkDuplicatesIncremental(ctx context.Context, mode DuplicateMode, newBookIDs []int64, progressFn func(processed, total int)) error

	// FindDuplicates returns all books that are duplicates of the given book
	// (or the original if the given book is itself a duplicate)
	FindDuplicates(ctx context.Context, bookID book.ID, pagination *Pagination) ([]*book.Book, error)

	// CountDuplicates returns the number of duplicates for a book
	CountDuplicates(ctx context.Context, bookID book.ID) (int, error)

	// --- Statistics ---

	// GetLanguageStats returns languages with book counts
	GetLanguageStats(ctx context.Context) ([]LanguageStats, error)

	// GetFormatStats returns formats with book counts
	GetFormatStats(ctx context.Context) ([]FormatStats, error)

	// GetDistinctLanguages returns all distinct language codes
	GetDistinctLanguages(ctx context.Context) ([]string, error)

	// GetDistinctFormats returns all distinct format codes
	GetDistinctFormats(ctx context.Context) ([]string, error)
}
